# command-ai — 构建、测试与发布任务
#
# 快速上手:
#   make            # 列出全部可用目标
#   make build      # 构建当前平台的二进制
#   make check      # gofmt 检查 + go vet + 全部测试
#   make dist       # 交叉编译 6 个平台到 dist/
#   make install    # 安装到 ~/.local/bin
#
# 版本号以 cmd/command-ai/main.go 中的 var version 为唯一来源，
# 可在命令行覆盖：make dist VERSION=2.0.0

SHELL := /bin/bash

BINARY := command-ai
CMD    := ./cmd/command-ai
DIST   := dist

GO      ?= go
GOFLAGS := -trimpath

# 版本号：默认从源码读取，保持单一来源。
VERSION ?= $(shell sed -n 's/^var version = "\(.*\)"/\1/p' $(CMD)/main.go)
LDFLAGS := -s -w -X main.version=$(VERSION)

# 支持的目标平台。
PLATFORMS := linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64

# 安装位置。
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin

# gccgo 不支持 go vet 与交叉编译，此时可用：
#   make test GO_TEST_FLAGS=-vet=off
GO_TEST_FLAGS ?=

.DEFAULT_GOAL := help
.PHONY: help build dist test cover vet fmt fmt-check check tidy install uninstall clean run version release

help: ## 显示本帮助
	@echo "command-ai $(VERSION)"
	@echo
	@echo "用法: make <目标>"
	@echo
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
	@echo
	@echo "变量: VERSION=$(VERSION)  PREFIX=$(PREFIX)  GO=$(GO)"

build: ## 构建当前平台的二进制到 dist/
	@mkdir -p $(DIST)
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(CMD)
	@echo "已构建 $(DIST)/$(BINARY) ($(VERSION))"

dist: ## 交叉编译全部平台到 dist/
	@rm -rf $(DIST)
	@mkdir -p $(DIST)
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		ext=""; [ "$$os" = windows ] && ext=".exe"; \
		out="$(DIST)/$(BINARY)-$$os-$$arch$$ext"; \
		printf '  %-42s' "$$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $$out $(CMD); \
		echo "ok"; \
	done
	@echo "已生成 $(words $(PLATFORMS)) 个二进制 ($(VERSION))"

test: ## 运行全部测试
	$(GO) test $(GO_TEST_FLAGS) ./...

cover: ## 运行测试并输出总覆盖率
	$(GO) test $(GO_TEST_FLAGS) -coverprofile=coverage.out ./...
	@$(GO) tool cover -func=coverage.out | tail -1

vet: ## 运行 go vet 静态检查
	$(GO) vet ./...

fmt: ## 格式化全部 Go 源码
	gofmt -l -w .

fmt-check: ## 检查格式，未格式化则失败
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
		echo "以下文件未格式化，请运行 make fmt:"; echo "$$out"; exit 1; \
	fi
	@echo "gofmt: 通过"

check: fmt-check vet test ## fmt-check + vet + test(提交前跑这个)

tidy: ## 整理 go.mod / go.sum
	$(GO) mod tidy

install: build ## 构建并安装到 PREFIX/bin(默认 ~/.local/bin)
	@mkdir -p $(BINDIR)
	install -m 0755 $(DIST)/$(BINARY) $(BINDIR)/$(BINARY)
	@echo "已安装 $(BINDIR)/$(BINARY)"

uninstall: ## 从 PREFIX/bin 卸载
	rm -f $(BINDIR)/$(BINARY)
	@echo "已卸载 $(BINDIR)/$(BINARY)"

release: ## 打 tag 并推送(需 TAG=vX.Y.Z，例如 make release TAG=v1.2.0)
	@test -n "$(TAG)" || { echo "用法: make release TAG=vX.Y.Z"; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "工作区不干净，请先提交"; exit 1; }
	git tag -a $(TAG) -m "command-ai $(TAG)"
	git push origin main
	git push origin $(TAG)
	@echo "已推送 $(TAG)：请在 GitHub 上创建 Release 并上传 make dist 的产物"

clean: ## 删除构建产物与覆盖率文件
	rm -rf $(DIST) coverage.out
	@echo "已清理"

run: ## 运行一次，参数用 ARGS 传入，如 make run ARGS='"列出文件"'
	$(GO) run $(CMD) $(ARGS)

version: ## 打印当前版本号
	@echo $(VERSION)
