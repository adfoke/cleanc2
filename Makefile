# CleanC2 — 日常构建入口。
# 默认使用仓库本地的 Go 缓存（?= 语义，可被环境或命令行覆盖）：
#   GOCACHE=/tmp/x make build   # 换缓存目录
#   make lint test check        # 组合目标
export GOCACHE    ?= $(CURDIR)/.gocache_local
export GOMODCACHE ?= $(CURDIR)/.gomodcache_local

.DEFAULT_GOAL := all

GO        := go
GOFMT     := gofmt
# go 源码所在目录（gofmt 递归遍历）
GO_DIRS   := cmd internal
# 非生成码的 go 文件：internal/protocol/pb 是 protoc 产物，不本地改写
GEN_PATTERNS := internal/protocol/pb
GO_SRCS   := $(shell find $(GO_DIRS) -name '*.go' ! -path '$(GEN_PATTERNS)/*')

.PHONY: all build test test-race vet fmt fmt-check lint proto clean check help

all: build ## 默认目标，等同 build

build: ## 编译三个二进制到 ./bin（server / agent / cleanc2）
	mkdir -p bin
	$(GO) build -o ./bin/server ./cmd/server
	$(GO) build -o ./bin/agent ./cmd/agent
	$(GO) build -o ./bin/cleanc2 ./cmd/cli

test: ## go test ./...
	$(GO) test ./...

test-race: ## go test -race ./...
	$(GO) test -race ./...

vet: ## go vet ./...
	$(GO) vet ./...

fmt: ## gofmt -w 原地格式化（不含 internal/protocol/pb 生成码）
	@if [ -n "$(GO_SRCS)" ]; then $(GOFMT) -w $(GO_SRCS); fi

fmt-check: ## gofmt -l 校验，有文件未格式化即失败
	@bad=$$($(GOFMT) -l $(GO_DIRS)); \
	if [ -n "$$bad" ]; then \
		echo "gofmt: 以下文件需要格式化:"; \
		echo "$$bad"; \
		exit 1; \
	fi

lint: fmt-check vet ## fmt-check + vet

proto: ## 重新生成 protobuf 码（委托 scripts/gen-proto.sh）
	./scripts/gen-proto.sh

clean: ## 删除 ./bin、仓库根 cleanc2.sock 与 *.db*（不碰 .gocache_local/.gomodcache_local、源码、git）
	rm -rf bin
	rm -f cleanc2.sock
	find . -maxdepth 1 -type f -name '*.db*' -delete

check: lint test ## 提交前一条龙：lint + test

help: ## 列出所有目标
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
