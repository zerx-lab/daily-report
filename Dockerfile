# ============================================================
# 日报自动化系统 - 多阶段构建 Dockerfile
# 适用于 Coolify 自动部署
# ============================================================

# -------------------- 阶段 1: 编译 --------------------
FROM golang:1.24-alpine AS builder

# CGO 必须启用（SQLite 依赖 mattn/go-sqlite3）
ENV CGO_ENABLED=1

# 安装 C 编译工具链（alpine 下编译 SQLite 需要）
RUN apk add --no-cache gcc musl-dev

WORKDIR /src

# 先复制依赖文件，利用 Docker 缓存层加速后续构建
COPY go.mod go.sum ./
RUN go mod download

# 复制全部源码
COPY . .

# 编译静态链接的二进制（模板和静态资源已通过 embed 嵌入）
RUN go build -trimpath -ldflags="-s -w" -o /out/daily-report ./cmd/server

# -------------------- 阶段 2: 运行 --------------------
FROM alpine:3.20

# 安装运行时依赖：时区数据 + CA 证书（HTTPS 请求需要）
RUN apk add --no-cache tzdata ca-certificates

# 设置时区
ENV TZ=Asia/Shanghai

WORKDIR /app

# 从编译阶段复制二进制
COPY --from=builder /out/daily-report .

# 复制配置模板（实际部署时通过挂载或环境变量覆盖）
COPY config/config.yaml.example config/config.yaml.example

# 创建数据目录（SQLite 数据库 + 日志）
RUN mkdir -p data

# 数据持久化卷（Coolify 中配置 Volume 挂载到此路径）
VOLUME ["/app/data", "/app/config"]

EXPOSE 8080

ENTRYPOINT ["./daily-report"]
CMD ["-config", "config/config.yaml"]
