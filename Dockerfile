# 多阶段构建：node 构建前端 → go 嵌入并编译后端 → 极简运行镜像。
#
# 设计要点：
#   1. 阶段 1（node-builder）：在 node:20-alpine 中跑 npm install + npm run build，
#      产出 web/dist；该阶段镜像有 ~300MB，最终运行镜像不会带它。
#   2. 阶段 2（go-builder）：把前端 dist 拷贝到 internal/web/dist 让 go:embed
#      把它打进二进制。modernc.org/sqlite 是纯 Go 实现，CGO 关闭即可。
#   3. 阶段 3（运行镜像）：alpine:3.20 + ca-certificates + tzdata，~10MB；
#      运维通过挂载 /app/data 卷持久化数据库 + 首次密码文件。
#
# v0.2 阶段说明：
#   配置真相源已迁移到 SQLite settings/bridges 表，不再使用 config.yaml。
#   首次启动时进程会创建数据库与默认 admin 账户（随机密码同时写入日志
#   与 /app/data/initial_password.txt），运维通过 Web 面板配置后即可。

# ---------- 阶段 1：构建前端 ----------
FROM node:20-alpine AS web-builder

WORKDIR /web

# 先单独 COPY 元数据再 install，最大化 Docker 层缓存命中：
# 前端依赖未变 → install 步骤用缓存；依赖变了再重装。
#
# 用 web/package*.json 通配符：本仓库当前不入仓 package-lock.json
# （npm install 自动生成），通配符让 COPY 在 lock 文件存在/不存在两种
# 情形下都能成功；Docker COPY 的"零匹配视为错误"行为只触发在没有
# package.json 的极端情况。
COPY web/package*.json ./
RUN npm install --include=dev

# 拷贝其余源码并构建。
COPY web/ ./
RUN npm run build

# ---------- 阶段 2：编译 Go ----------
FROM golang:1.25-alpine AS go-builder

# 基础工具：git 用于版本号注入；ca-certificates 用于 HTTPS 证书校验。
RUN apk add --no-cache git ca-certificates

WORKDIR /src

# 先单独拉模块缓存，最大化 Docker 层缓存命中率。
COPY go.mod go.sum ./
RUN go mod download

# 拷贝全部 Go 源码。
COPY . .

# 把阶段 1 产出的前端 dist 注入到 internal/web/dist——这是 go:embed 的目标。
# 先 rm 再 cp 保证全量覆盖（仓库内的 placeholder index.html 被替换为真实前端）。
COPY --from=web-builder /web/dist /tmp/web-dist
RUN rm -rf /src/internal/web/dist && \
    mkdir -p /src/internal/web/dist && \
    cp -R /tmp/web-dist/. /src/internal/web/dist/

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/xboard-xui-bridge \
    ./cmd/bridge

# ---------- 阶段 3：运行镜像 ----------
FROM alpine:3.20

# 仅安装 ca-certificates；不需要 sqlite-libs（modernc.org/sqlite 是纯 Go 实现）。
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# 创建非 root 用户运行进程：减少潜在攻击面。
RUN addgroup -S bridge && adduser -S -G bridge bridge

# 预创建 /app/data 并 chown 给 bridge 用户：
#
# 否则进程作为 bridge 启动时无法在 VOLUME 挂载点下写入（OCI 运行时把
# VOLUME 视为容器内匿名卷或 host bind mount，默认权限是 root:root）。
# SQLite 写文件、admin 账户引导写 initial_password.txt 都会因此 EACCES。
#
# 使用 bind mount 时，运维仍需自行确保宿主路径可被容器内 uid 写入——
# 一个常见做法：宿主预先 `chown 1000:1000 ./data`（取决于镜像内 bridge
# 的 uid，alpine adduser -S 默认是 100 系统用户，可能不是 1000）。
# 文档在 README "容器化" 章节会说明。
RUN mkdir -p /app/data && chown -R bridge:bridge /app/data

COPY --from=go-builder /out/xboard-xui-bridge /usr/local/bin/xboard-xui-bridge

# 显式声明持久卷：运维须挂载本地目录避免数据丢失。
# /app/data 内会自动生成 bridge.db、bridge.db-wal、bridge.db-shm、
# initial_password.txt（首次启动）以及未来日志文件等。
VOLUME ["/app/data"]

# 暴露 Web 面板默认端口。
EXPOSE 8787

# 默认让 Web 面板绑定全部网卡，让 -p 8787:8787 在容器场景下可达。
# 裸跑（非容器）时该环境变量不存在，配置仍按 store 中的 web.listen_addr
# 走（默认 127.0.0.1:8787，更安全）。
ENV BRIDGE_LISTEN_ADDR=:8787

USER bridge:bridge

# v0.2 起命令行接受：
#   xboard-xui-bridge                  默认 daemon
#   xboard-xui-bridge run --db ...     显式 daemon
#   xboard-xui-bridge reset-password   重置密码
#   xboard-xui-bridge version          打印版本
ENTRYPOINT ["/usr/local/bin/xboard-xui-bridge"]
CMD ["--db", "/app/data/bridge.db"]
