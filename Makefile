# 常用命令，含义见 docs/development.md 第 4 节。

GO ?= go
BIN := $(CURDIR)/bin
export PATH := $(BIN):$(PATH)

# 工具版本固定在这里；升级单独提交。
BUF_VERSION := v1.73.0
PROTOC_GEN_GO_VERSION := v1.36.12
GOLANGCI_LINT_VERSION := v2.14.0
GOIMPORTS_VERSION := v0.38.0

BUF := $(BIN)/buf
GOLANGCI_LINT := $(BIN)/golangci-lint
GOIMPORTS := $(BIN)/goimports

# 开发规范第 5 节：这些包必须至少有一个带规则标注的测试（包尚不存在时跳过）。
RULE_PACKAGES := core/egress core/grants core/budget core/durable core/ledger infra/postgres infra/sqlite

# 需要格式化的 Go 文件：生成代码除外。
GO_FILES = $(shell git ls-files --cached --others --exclude-standard '*.go' | grep -v '^contracts/gen/')

.PHONY: tools fmt lint test test-fault gen check check-gen check-rules buf-lint buf-breaking

tools: $(BUF) $(GOLANGCI_LINT) $(GOIMPORTS) $(BIN)/protoc-gen-go

$(BUF):
	GOBIN=$(BIN) $(GO) install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)

$(BIN)/protoc-gen-go:
	GOBIN=$(BIN) $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)

$(GOLANGCI_LINT):
	GOBIN=$(BIN) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

$(GOIMPORTS):
	GOBIN=$(BIN) $(GO) install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)

fmt: $(GOIMPORTS) $(BUF)
ifneq ($(strip $(GO_FILES)),)
	gofmt -w $(GO_FILES)
	$(GOIMPORTS) -local github.com/ruipengliu/lerna -w $(GO_FILES)
endif
	$(BUF) format -w

lint: $(GOLANGCI_LINT) buf-lint buf-breaking
	$(GOLANGCI_LINT) run ./...

buf-lint: $(BUF)
	$(BUF) format --diff --exit-code
	$(BUF) lint

# 破坏性变更检查：与 main 分支上的协议比较；main 上还没有协议时跳过。
buf-breaking: $(BUF)
	@if git cat-file -e main:contracts/proto 2>/dev/null; then \
		$(BUF) breaking --against '.git#branch=main'; \
	else \
		echo "buf-breaking: main 上尚无 contracts/proto，跳过"; \
	fi

test:
	$(GO) test -race -count=1 ./...

test-fault:
	$(GO) test -race -count=1 -tags fault ./conformance/fault/...

gen: $(BUF) $(BIN)/protoc-gen-go
	rm -rf contracts/gen/go
	$(BUF) generate

# 生成代码是否最新：重新生成到临时目录，与仓库中的生成代码比较。
check-gen: $(BUF) $(BIN)/protoc-gen-go
	@tmp=$$(mktemp -d); \
	$(BUF) generate -o $$tmp && \
	if [ -d $$tmp/contracts/gen/go ] || [ -d contracts/gen/go ]; then \
		diff -r $$tmp/contracts/gen/go contracts/gen/go >/dev/null 2>&1 || { echo "生成代码过期：运行 make gen"; rm -rf $$tmp; exit 1; }; \
	fi; \
	rm -rf $$tmp

# 规则标注检查：测试函数上方一行必须是 "// 规则：..."。
check-rules:
	@fail=0; \
	for pkg in $(RULE_PACKAGES); do \
		[ -d $$pkg ] || continue; \
		if ! grep -A1 -h '^// 规则：' $$pkg/*_test.go 2>/dev/null | grep -q '^func Test'; then \
			echo "规则标注缺失：$$pkg 没有带 '// 规则：' 标注的测试"; fail=1; \
		fi; \
	done; \
	exit $$fail

check: fmt-check lint test check-rules check-gen

.PHONY: fmt-check
fmt-check: $(GOIMPORTS)
ifneq ($(strip $(GO_FILES)),)
	@out=$$(gofmt -l $(GO_FILES)); \
	if [ -n "$$out" ]; then echo "未格式化：$$out"; exit 1; fi
	@out=$$($(GOIMPORTS) -local github.com/ruipengliu/lerna -l $(GO_FILES)); \
	if [ -n "$$out" ]; then echo "imports 未整理：$$out"; exit 1; fi
endif
