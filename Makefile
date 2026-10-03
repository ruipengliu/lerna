SHELL := /bin/bash
export GOTOOLCHAIN := local
export GOFLAGS := -mod=readonly
FORMATTED := scripts/*.mjs sdk/typescript/src/**/*.ts sdk/typescript/src/*.ts sdk/typescript/*.json contract/schema/**/*.json conformance/fixtures/**/*.json package.json pnpm-workspace.yaml .prettierrc.json .github/workflows/*.yaml

.PHONY: bootstrap generate fmt lint test test-race test-contract build check
bootstrap:
	node scripts/bootstrap.mjs
generate:
	pnpm generate
fmt:
	gofmt -w contract conformance
	pnpm exec prettier --write $(FORMATTED)
lint:
	node scripts/check-go-format.mjs
	pnpm exec prettier --check $(FORMATTED)
	go vet ./...
	pnpm lint
test:
	go test ./...
	pnpm test
test-race:
	go test -race ./...
test-contract:
	node scripts/test-contract.mjs
build:
	go build ./...
	pnpm build
check: lint
	pnpm check:generated
	$(MAKE) test test-contract build
