# xboard-xui-bridge 构建脚本。
#
# 设计原则：
#   - 单一 Make 目标对应单一动作；不在一个目标里做两件事。
#   - 所有 Go 命令都通过环境变量 GO 引用，便于切换 toolchain。
#   - 版本号通过 ldflags 注入到 main.version；CI 构建时由 git tag 提供。
#
# v0.2 起的目标增量：
#   - web         本地构建 Vue 3 前端 + 拷贝到 internal/web/dist 供 go:embed
#   - build-all   先 web 再 build，端到端产出含前端的二进制
#
# 平台说明：
#   本 Makefile 假设 GNU Make + Unix 工具（cp / rm / mkdir）。Windows 用户
#   建议用 WSL 或 git-bash 运行；纯 cmd 环境请直接用 Docker 多阶段构建。

GO        ?= go
NPM       ?= npm
GOFLAGS   ?= -trimpath
LDFLAGS   ?= -s -w -X main.version=$(VERSION)
VERSION   ?= $(shell git -C $(CURDIR) describe --tags --always 2>/dev/null || echo dev)
OUT       ?= dist
BIN       ?= xboard-xui-bridge

WEB_DIR   := web
EMBED_DIR := internal/web/dist

.PHONY: all
all: build-all

## build: 编译当前平台二进制到 dist/<bin>（不含前端构建步骤；用 build-all 端到端）
.PHONY: build
build:
	@mkdir -p $(OUT)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(OUT)/$(BIN) ./cmd/bridge

## build-linux: 交叉编译 linux/amd64 二进制
.PHONY: build-linux
build-linux:
	@mkdir -p $(OUT)
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(OUT)/$(BIN)-linux-amd64 ./cmd/bridge

## build-arm64: 交叉编译 linux/arm64 二进制（适用于 ARM 架构服务器）
.PHONY: build-arm64
build-arm64:
	@mkdir -p $(OUT)
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(OUT)/$(BIN)-linux-arm64 ./cmd/bridge

## web: 构建 Vue 3 前端并把 dist 同步到 internal/web/dist 让 go:embed 打入二进制
##
## 先 npm install（含 dev 依赖）→ npm run build 输出到 web/dist →
## rm -rf internal/web/dist + cp 全量覆盖。注意：本目标不增量——每次都
## 重建并全量覆盖，保证 dist 永远反映最新前端代码。
.PHONY: web
web:
	cd $(WEB_DIR) && $(NPM) install --include=dev
	cd $(WEB_DIR) && $(NPM) run build
	rm -rf $(EMBED_DIR)
	mkdir -p $(EMBED_DIR)
	cp -R $(WEB_DIR)/dist/. $(EMBED_DIR)/

## build-all: 先构建前端，再编译 Go 二进制；端到端产出含 GUI 的二进制
##
## 这是开发期"想看完整效果"的标准目标。CI / Docker 构建路径不走本目标，
## 它们各自有更精确的多阶段流程。
.PHONY: build-all
build-all: web build

## test: 跑全部 Go 单元测试
.PHONY: test
test:
	$(GO) test ./...

## vet: 静态检查
.PHONY: vet
vet:
	$(GO) vet ./...

## tidy: 清理 go.mod / go.sum
.PHONY: tidy
tidy:
	$(GO) mod tidy

## clean: 清理 Go 产物 + 前端构建中间产物（不动 internal/web/dist——它是 commit 入仓的副本）
.PHONY: clean
clean:
	rm -rf $(OUT)
	rm -rf $(WEB_DIR)/dist
	rm -rf $(WEB_DIR)/node_modules

## help: 列出常用目标
.PHONY: help
help:
	@grep -E '^##' Makefile | sed -e 's/## /  /'
