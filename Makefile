SHELL := /bin/sh
TOOLS_DIR ?= $(shell go env GOPATH)/bin/lerna-tools
BUF := $(TOOLS_DIR)/buf-v1.73.0/buf
LINT := $(TOOLS_DIR)/golangci-lint-v2.14.0/golangci-lint
IMPORTS := $(TOOLS_DIR)/goimports-v0.51.0/goimports
PROTO := $(TOOLS_DIR)/protoc-gen-go-v1.36.12/protoc-gen-go
export PATH := $(dir $(PROTO)):$(PATH)
# 可覆盖为发布标签或其他兼容性基线；初始 main 无协议时跳过。
BUF_BASE ?= main

.PHONY: tools fmt lint test test-fault gen check check-fmt check-rules check-gen

tools: $(BUF) $(LINT) $(IMPORTS) $(PROTO)
$(BUF):
	GOBIN=$(dir $(BUF)) go install github.com/bufbuild/buf/cmd/buf@v1.73.0
$(LINT):
	GOBIN=$(dir $(LINT)) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
$(IMPORTS):
	GOBIN=$(dir $(IMPORTS)) go install golang.org/x/tools/cmd/goimports@v0.51.0
$(PROTO):
	GOBIN=$(dir $(PROTO)) go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12

fmt: $(IMPORTS)
	git ls-files -z --cached --others --exclude-standard -- '*.go' ':!:contracts/gen/**' | xargs -0 $(IMPORTS) -w

check-fmt: $(IMPORTS)
	@files=$$(git ls-files -z --cached --others --exclude-standard -- '*.go' ':!:contracts/gen/**' | xargs -0 $(IMPORTS) -l) || exit $$?; test -z "$$files" || { echo "Run make fmt:"; echo "$$files"; exit 1; }

lint: $(BUF) $(LINT)
	$(LINT) run
	$(LINT) run --build-tags fault
	$(BUF) format --diff --exit-code
	$(BUF) lint
	@git rev-parse --verify "$(BUF_BASE)^{commit}" >/dev/null
	@if test -n "$$(git ls-tree -r --name-only $(BUF_BASE) -- contracts/proto 2>/dev/null)"; then $(BUF) breaking --against ".git#ref=$(BUF_BASE)"; else echo "No protocol in $(BUF_BASE); breaking baseline starts with first protocol release."; fi

test:
	go test -race ./...

test-fault:
	@if test -d conformance/fault; then go test -race -timeout 30m -tags fault ./conformance/fault/...; else echo 'No fault suite yet.'; fi

gen: $(BUF) $(PROTO)
	$(BUF) generate

check-rules:
	go run ./scripts/checkrules

check-gen: $(BUF) $(PROTO)
	@sh scripts/check-generated.sh "$(BUF)"

check: check-fmt lint test test-fault check-rules check-gen
