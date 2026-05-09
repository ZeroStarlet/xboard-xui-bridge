# xboard-xui-bridge 构建脚本。
#
# 设计原则：
#   - 单一 Make 目标对应单一动作；不在一个目标里做两件事。
#   - 所有 Go 命令都通过环境变量 GO 引用，便于切换 toolchain。
#   - 版本号通过 ldflags 注入到 main.version；CI 构建时由 git tag 提供。

GO       ?= go
GOFLAGS  ?= -trimpath
LDFLAGS  ?= -s -w -X main.version=$(VERSION)
VERSION  ?= $(shell git -C $(CURDIR) describe --tags --always 2>/dev/null || echo dev)
OUT      ?= dist
BIN      ?= xboard-xui-bridge

.PHONY: all
all: build

## build: 编译当前平台二进制到 dist/<bin>
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

## test: 跑全部单元测试
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

## clean: 清理产物
.PHONY: clean
clean:
	rm -rf $(OUT)

## help: 列出常用目标
.PHONY: help
help:
	@grep -E '^##' Makefile | sed -e 's/## /  /'
