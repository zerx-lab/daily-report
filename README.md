# 📋 日报自动化系统

基于思源笔记 API 的工作日报自动化管理系统。支持自动创建日报条目、Web 管理界面编辑、定时邮件发送，让日报管理更加高效。

## ✨ 功能特性

- **自动创建日报**：每个工作日早晨自动在思源笔记数据库中创建当日日报条目
- **Web 管理界面**：基于 Bootstrap 5 的响应式管理后台，支持日报的增删改查
- **邮件自动发送**：每个工作日晚间通过 SMTP 自动将日报发送给指定收件人
- **思源笔记同步**：双向同步，支持从思源笔记拉取日报内容到本地数据库
- **定时任务管理**：可视化管理定时任务，支持启用/禁用、修改 Cron 表达式、手动触发
- **发送记录追踪**：完整的邮件发送日志，可追溯每次发送的状态和详情
- **节假日识别**：支持跳过中国法定节假日，避免非工作日误创建/发送
- **系统设置**：Web 界面配置 SMTP、思源笔记连接、收件人等参数，无需重启服务
- **REST API**：提供完整的 JSON API，方便与其他系统集成

## 🛠 技术栈

| 类别 | 技术 |
|------|------|
| 语言 | Go 1.23+ |
| Web 框架 | [Gin](https://github.com/gin-gonic/gin) |
| 数据库 | SQLite（通过 [GORM](https://gorm.io/) ORM） |
| 定时调度 | [gocron/v2](https://github.com/go-co-op/gocron) |
| 邮件发送 | 标准库 `net/smtp` + `crypto/tls` |
| 配置管理 | YAML（[gopkg.in/yaml.v3](https://gopkg.in/yaml.v3)） |
| 前端 UI | Bootstrap 5 + 自定义 CSS |
| 笔记 API | [思源笔记](https://b3log.org/siyuan/) HTTP API |

## 🚀 快速开始

### 前置条件

- Go 1.23 或更高版本
- GCC（SQLite 需要 CGO 编译）
- 思源笔记实例（已配置数据库属性视图）

### 1. 克隆项目

```bash
git clone https://github.com/zerx-lab/daily-report.git
cd daily-report
```

### 2. 安装依赖

```bash
go mod tidy
```

### 3. 创建配置文件

```bash
cp config/config.yaml.example config/config.yaml
```

编辑 `config/config.yaml`，填入你的实际配置：

- **思源笔记**：填写 `base_url`、`token`、数据库相关 ID
- **SMTP**：填写邮件服务器地址、端口、账号密码
- **收件人**：配置日报邮件的接收者

### 4. 运行项目

**开发模式（推荐）：**

```bash
make dev
```

**编译后运行：**

```bash
make build
./build/daily-report
```

**指定配置文件：**

```bash
./build/daily-report -config /path/to/config.yaml
```

### 5. 访问管理界面

启动后在浏览器中打开：

```
http://localhost:8080
```

## 📁 项目结构

```
daily-report/
├── cmd/
│   └── server/
│       └── main.go              # 程序入口
├── config/
│   ├── config.yaml              # 运行配置（不提交到 Git）
│   └── config.yaml.example      # 配置文件模板
├── internal/
│   ├── config/
│   │   └── config.go            # 配置加载与解析
│   ├── controller/
│   │   ├── report.go            # 日报控制器（页面 + API）
│   │   └── setting.go           # 设置控制器
│   ├── model/
│   │   ├── db.go                # 数据库初始化与迁移
│   │   ├── report.go            # 日报模型
│   │   ├── email_log.go         # 邮件日志模型
│   │   └── setting.go           # 系统设置模型
│   ├── router/
│   │   └── router.go            # 路由注册与中间件
│   └── service/
│       ├── report.go            # 日报业务逻辑
│       ├── email.go             # 邮件渲染与发送
│       ├── siyuan.go            # 思源笔记 API 交互
│       └── scheduler.go         # 定时任务调度
├── templates/
│   ├── email/
│   │   └── daily_report.html    # 邮件 HTML 模板
│   └── pages/
│       ├── layout.html          # 页面布局模板
│       ├── dashboard.html       # 仪表盘页面
│       └── reports.html         # 日报列表页面
├── static/
│   └── css/
│       └── style.css            # 自定义样式
├── data/                        # 运行时数据目录（自动创建）
│   ├── daily_report.db          # SQLite 数据库
│   └── app.log                  # 应用日志
├── go.mod
├── go.sum
├── Makefile                     # 构建脚本
├── AGENTS.md                    # 项目开发规范
└── README.md                    # 本文件
```

## 📡 API 接口

系统提供两类接口：**页面路由**（返回 HTML）和 **REST API**（返回 JSON）。

### 页面路由

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/` | 仪表盘首页 |
| GET | `/reports` | 日报列表 |
| GET | `/reports/new` | 新建日报表单 |
| POST | `/reports/new` | 提交新建日报 |
| GET | `/reports/:id/edit` | 编辑日报 |
| POST | `/reports/:id/edit` | 提交更新 |
| POST | `/reports/:id/delete` | 删除日报 |
| POST | `/reports/:id/send` | 手动发送邮件 |
| POST | `/reports/create-today` | 快速创建今日日报 |
| POST | `/sync/from-siyuan` | 从思源笔记同步 |
| GET | `/logs` | 发送记录列表 |
| GET | `/schedule` | 定时任务管理 |
| GET | `/settings` | 系统设置 |

### REST API (JSON)

所有 API 路由以 `/api/v1` 为前缀。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/reports` | 日报列表 |
| GET | `/api/v1/reports/:id` | 日报详情 |
| POST | `/api/v1/reports` | 创建日报 |
| PUT | `/api/v1/reports/:id` | 更新日报 |
| DELETE | `/api/v1/reports/:id` | 删除日报 |
| POST | `/api/v1/reports/:id/send` | 发送日报邮件 |
| POST | `/api/v1/sync/pull` | 从思源笔记同步 |
| POST | `/api/v1/sync/ping` | 测试思源连接 |
| GET | `/api/v1/schedule/status` | 调度器状态 |
| POST | `/api/v1/schedule/trigger` | 手动触发任务 |
| GET | `/api/v1/logs` | 发送记录 |
| GET | `/api/v1/system/health` | 健康检查 |
| GET | `/api/v1/system/info` | 系统信息 |

## ⚙️ 配置说明

配置文件为 `config/config.yaml`，支持以下配置项：

### 服务器配置

```yaml
server:
  host: "0.0.0.0"       # 监听地址
  port: 8080             # 监听端口
  mode: "debug"          # Gin 运行模式：debug / release / test
```

### 思源笔记配置

```yaml
siyuan:
  base_url: "https://your-siyuan-instance.com"   # 思源笔记地址
  token: "your-api-token"                         # API Token
  notebook_id: "..."                              # 笔记本 ID
  document_id: "..."                              # 日报文档 ID
  av_id: "..."                                    # 数据库（属性视图）ID
  block_id: "..."                                 # 数据库块 ID
  view_id: "..."                                  # 视图 ID
  key_id: "..."                                   # 日期列 Key ID（主键）
  key_content_id: "..."                           # 工作内容列 Key ID
```

### SMTP 邮件配置

```yaml
smtp:
  host: "smtp.example.com"    # SMTP 服务器
  port: 465                    # 端口（465=SSL，587=STARTTLS）
  username: "user@example.com" # 账号
  password: "password"         # 密码或授权码
  use_tls: true                # 是否使用 TLS
  from_name: "日报系统"         # 发件人名称
  from_address: "user@example.com"  # 发件人地址
```

### 邮件收件人配置

```yaml
email:
  recipients:
    - name: "领导"
      address: "leader@example.com"
  cc: []
  subject_template: "{{.Date}} 工作日报 - {{.Author}}"
  author: "你的名字"
```

### 定时调度配置

```yaml
scheduler:
  enabled: true
  timezone: "Asia/Shanghai"
  create_cron: "0 30 8 * * 1-5"    # 工作日 08:30 自动创建
  send_cron: "0 0 18 * * 1-5"      # 工作日 18:00 自动发送
  skip_holidays: true                # 跳过法定节假日
  default_content: "待填写"          # 默认日报内容
```

> 💡 **提示**：大部分配置也可以通过 Web 管理界面的「系统设置」页面进行修改，修改后立即生效，无需重启服务。

## 📦 构建命令

```bash
make help        # 查看所有可用命令
make dev         # 开发模式运行（go run）
make build       # 编译项目
make run         # 编译并运行
make test        # 运行测试
make lint        # 代码检查
make clean       # 清理构建产物
make deps        # 整理依赖
```

## 📄 License

MIT