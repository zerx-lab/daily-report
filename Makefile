# 日报自动化系统 - Makefile

APP_NAME := daily-report
BUILD_DIR := build
MAIN_PATH := ./cmd/server

# Go 相关变量
GO := go
GOFLAGS := -v
LDFLAGS := -s -w

.PHONY: all build build-mcp build-all run clean deps dev dev-mcp test lint help init

# 默认目标
all: deps build

# 初始化项目（首次使用）
init:
	@echo ">>> 初始化 Go 模块..."
	$(GO) mod init daily-report
	@echo ">>> 安装依赖..."
	$(GO) get github.com/gin-gonic/gin@latest
	$(GO) get gorm.io/gorm@latest
	$(GO) get gorm.io/driver/sqlite@latest
	$(GO) get gopkg.in/yaml.v3@latest
	$(GO) get github.com/robfig/cron/v3@latest
	$(GO) get github.com/jordan-wright/email@latest
	$(GO) mod tidy
	@echo ">>> 初始化完成！"

# 安装/更新依赖
deps:
	@echo ">>> 整理依赖..."
	$(GO) mod tidy

# 编译项目
build: deps
	@echo ">>> 编译项目..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_PATH)
	@echo ">>> 编译完成: $(BUILD_DIR)/$(APP_NAME)"

# 编译 MCP Server（供 Claude Code CLI 使用）
build-mcp: deps
	@echo ">>> 编译 MCP Server..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)-mcp ./cmd/mcp
	@echo ">>> 编译完成: $(BUILD_DIR)/$(APP_NAME)-mcp"

# 开发模式运行（不编译直接运行）
dev:
	@echo ">>> 开发模式启动..."
	CGO_ENABLED=1 $(GO) run $(MAIN_PATH)

# 开发模式运行 MCP Server
dev-mcp:
	@echo ">>> MCP Server 开发模式启动..."
	CGO_ENABLED=1 $(GO) run ./cmd/mcp --db data/daily_report.db

# 运行编译后的程序
run: build
	@echo ">>> 启动服务..."
	./$(BUILD_DIR)/$(APP_NAME)

# 运行测试
test:
	@echo ">>> 运行测试..."
	$(GO) test ./... -v -cover

# 代码检查
lint:
	@echo ">>> 代码检查..."
	$(GO) vet ./...

# 清理构建产物
clean:
	@echo ">>> 清理构建产物..."
	@rm -rf $(BUILD_DIR)
	@rm -f daily-report.db
	@echo ">>> 清理完成"

# 生成默认配置文件（如果不存在）
config:
	@if [ ! -f config/config.yaml ]; then \
		echo ">>> 配置文件已存在于 config/config.yaml"; \
	else \
		echo ">>> 配置文件已存在，跳过生成"; \
	fi

# 帮助信息
help:
	@echo ""
	@echo "日报自动化系统 - 构建命令"
	@echo "========================================"
	@echo ""
	@echo "  make init      - 首次初始化项目（创建 go.mod 并安装依赖）"
	@echo "  make deps      - 整理依赖"
	@echo "  make build     - 编译 Web 服务"
	@echo "  make build-mcp - 编译 MCP Server（供 Claude Code CLI 使用）"
	@echo "  make build-all - 编译所有产物"
	@echo "  make dev       - 开发模式运行 Web 服务（go run）"
	@echo "  make dev-mcp   - 开发模式运行 MCP Server"
	@echo "  make run       - 编译并运行 Web 服务"
	@echo "  make test      - 运行测试"
	@echo "  make lint      - 代码检查"
	@echo "  make clean     - 清理构建产物"
	@echo "  make help      - 显示帮助信息"
	@echo ""
