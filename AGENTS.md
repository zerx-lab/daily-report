# 日报自动化系统 - AGENTS

## 项目概述

Go + Gin + GORM(SQLite) 实现的日报管理 Web 应用。每个工作日自动在思源笔记数据库顶部新建日报记录，并通过 SMTP 定时发送邮件。

**模块名：** `github.com/zerx-lab/daily-report`  
**Go 版本：** 1.23  **CGO：** 必须启用（SQLite 依赖）

---

## 构建 / 运行 / 测试命令

```bash
# 整理依赖
make deps          # go mod tidy

# 编译（产物在 build/daily-report）
make build         # CGO_ENABLED=1 go build ./cmd/server

# 开发模式（不编译直接运行）
make dev           # CGO_ENABLED=1 go run ./cmd/server

# 编译后运行
make run

# 运行所有测试
make test          # go test ./... -v -cover

# 运行单个测试（替换 TestXxx 为实际测试函数名）
CGO_ENABLED=1 go test ./internal/service/... -run TestXxx -v

# 运行某个包的所有测试
CGO_ENABLED=1 go test ./internal/model/... -v -cover

# 代码静态检查
make lint          # go vet ./...

# 清理构建产物
make clean
```

**首次初始化：** 复制配置文件 `cp config/config.yaml.example config/config.yaml`，填写实际配置后再运行。

---

## 目录结构

```
cmd/server/main.go          入口：启动顺序 配置→DB→服务→调度→路由→HTTP
internal/
  config/config.go          YAML 配置加载（单例），支持 os.ExpandEnv 展开环境变量
  model/                    GORM 数据模型 + DB 初始化（WAL 模式，MaxOpenConns=1）
  service/                  业务逻辑层（report、email、siyuan、scheduler）
  controller/report.go      Gin 处理器（统一由 ReportController 处理所有路由）
  router/router.go          路由注册 + 中间件 + 模板函数
config/                     config.yaml（忽略）+ config.yaml.example（提交）
templates/                  HTML 模板（Go html/template）
static/                     CSS/JS 静态资源
data/                       SQLite 数据库（忽略，不提交）
```

---

## 代码风格规范

### 命名

- **类型 / 函数 / 常量**：导出用 PascalCase，未导出用 camelCase
- **构造函数**：`NewXxx(deps) *Xxx` 模式，依赖通过参数注入
- **接口方法**：动词开头，如 `GetByID`、`CreateToday`、`UpdateContent`
- **常量枚举**：使用 `iota`，类型定义为 `type XxxStatus int`，并实现 `String()` 方法
- **模型方法**：工具方法用值接收者；业务方法用指针接收者

### import 顺序（goimports 标准）

```go
import (
    // 1. 标准库
    "fmt"
    "time"

    // 2. 外部依赖（空行分隔）
    "github.com/gin-gonic/gin"
    "gorm.io/gorm"

    // 3. 内部包（空行分隔）
    "github.com/zerx-lab/daily-report/internal/model"
    "github.com/zerx-lab/daily-report/internal/service"
)
```

### 错误处理

- 始终用 `fmt.Errorf("上下文描述: %w", err)` 包装错误，提供中文上下文
- Service 层：返回 `(result, error)` 或 `(result, bool, error)`，不吞错误
- Controller 层：使用 `c.jsonError()` / `c.flashAndRedirect()` 统一响应
- 不使用 `panic`；`log.Fatalf` 仅用于启动阶段不可恢复的错误

### JSON API 响应格式

```go
// 成功
ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
// 失败
ctx.JSON(http.StatusXxx, gin.H{"code": -1, "message": err.Error()})
```

### 日志格式

使用标准 `log` 包，前缀用中括号中文标识模块：

```go
log.Printf("[日报] 创建成功: %s %s\n", date, weekday)
log.Printf("[控制器] 获取仪表盘数据失败: %v\n", err)
log.Println("[启动] 配置加载成功")
```

### 数据模型规范

- GORM 模型字段：同时标注 `gorm:"..."` 和 `json:"..."` tag
- 软删除字段：`DeletedAt gorm.DeletedAt \`gorm:"index" json:"-"\``
- 指针类型用于可空时间：`SentAt *time.Time`
- 实现 `TableName() string` 显式指定表名

```go
type Report struct {
    ID      uint         `gorm:"primaryKey" json:"id"`
    Date    string       `gorm:"type:varchar(10);uniqueIndex;not null" json:"date"`
    Status  ReportStatus `gorm:"type:integer;default:0;index" json:"status"`
    // ...
}
func (Report) TableName() string { return "reports" }
```

### 异步操作

耗时操作（思源同步、邮件发送）在 goroutine 中异步执行，错误仅记录日志：

```go
go func(r *model.Report) {
    if err := c.siyuanSvc.SyncLocalToSiyuan(r.ID); err != nil {
        log.Printf("[控制器] 同步思源笔记失败(异步): %v\n", err)
    }
}(report)
```

---

## 思源笔记关键 ID（只读，勿修改）

| 项目 | ID |
|------|-----|
| 笔记本（工作） | `20260320105739-1y06ufo` |
| 日报文档 | `20260324161646-k5n8meb` |
| 数据库 AV ID | `20260324161653-vrznito` |
| 数据库块 ID | `20260324161646-e2zu02m` |
| 主键列 Key ID（工作内容） | `20260324161653-rkbx4tv` |

**核心 API：**  
- 顶部插入行：`POST /api/av/addAttributeViewBlocks`，`previousID: ""` 表示插入到顶部  
- 修改单元格：`POST /api/av/setAttributeViewBlockAttr`  
- 读取数据：`POST /api/av/renderAttributeView`，响应路径 `data.view.rows[].cells[].value`

---

## 安全注意事项

- `config/config.yaml` 含敏感信息，已加入 `.gitignore`，**不得提交**
- API Token / SMTP 密码使用配置文件或环境变量，**不得硬编码**
- 配置文件支持 `$VAR` 环境变量展开（`os.ExpandEnv`）
