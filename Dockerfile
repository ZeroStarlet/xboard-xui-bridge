# 多阶段构建：第一阶段编译，第二阶段产出极简运行镜像。
#
# 设计要点：
#   1. 编译阶段使用官方 golang:alpine 镜像；运行阶段使用 alpine:latest。
#      modernc.org/sqlite 是纯 Go 实现，运行镜像无需 glibc / libsqlite。
#   2. 二进制带 -trimpath -ldflags="-s -w"，剥离调试信息，体积更小。
#   3. 配置与状态目录通过 VOLUME 显式声明：运维必须挂载到持久卷，否则容器重启会丢基线。

FROM golang:1.22-alpine AS builder

# 基础工具：git 用于版本号注入；ca-certificates 用于 HTTPS 证书校验。
RUN apk add --no-cache git ca-certificates

WORKDIR /src

# 先单独拉模块缓存，最大化 Docker 层缓存命中率。
COPY go.mod go.sum ./
RUN go mod download

# 拷贝全部源码并编译。
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/xboard-xui-bridge \
    ./cmd/bridge

# ---------- 运行阶段 ----------
FROM alpine:3.20

# 仅安装 ca-certificates；不需要 sqlite-libs（modernc.org/sqlite 是纯 Go 实现）。
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# 创建非 root 用户运行进程：减少潜在攻击面。
RUN addgroup -S bridge && adduser -S -G bridge bridge

COPY --from=builder /out/xboard-xui-bridge /usr/local/bin/xboard-xui-bridge
COPY configs/config.example.yaml /app/config.example.yaml

# 显式声明持久卷：运维须挂载本地目录避免数据丢失。
VOLUME ["/app/data"]

USER bridge:bridge

# 默认查找 /app/config.yaml；运维通过挂载或 -e 替代。
ENTRYPOINT ["/usr/local/bin/xboard-xui-bridge", "--config", "/app/config.yaml"]
