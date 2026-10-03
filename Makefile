SHELL := /bin/bash
export GOTOOLCHAIN := local
export GOFLAGS := -mod=readonly
FORMATTED := scripts/*.mjs sdk/typescript/src/**/*.ts sdk/typescript/src/*.ts sdk/typescript/*.json contract/schema/**/*.json internal/**/*.json conformance/fixtures/**/*.json package.json pnpm-workspace.yaml .prettierrc.json .github/workflows/*.yaml

.PHONY: bootstrap generate fmt lint test test-race test-contract test-integration build check
bootstrap:
	node scripts/bootstrap.mjs
generate:
	pnpm generate
fmt:
	gofmt -w contract conformance runtime host internal adapters cmd
	pnpm exec prettier --write $(FORMATTED)
lint:
	node scripts/check-go-format.mjs
	pnpm exec prettier --check $(FORMATTED)
	go vet ./...
	pnpm lint
test:
	node --test scripts/contract-runner.test.mjs
	node scripts/test-generator.mjs
	go test ./...
	pnpm test
test-race:
	go test -race ./...
test-integration:
	@test -n "$$LERNA_TEST_POSTGRES_DSN" || { echo "LERNA_TEST_POSTGRES_DSN is required (dedicated PostgreSQL test database)" >&2; exit 1; }
	cd conformance/fixtures/durable-work/pg-v1 && sha256sum -c SHA256SUMS
	go test -tags=integration -timeout=120s ./conformance/recovery/...
test-contract:
	node scripts/test-contract.mjs
	node scripts/test-contract.mjs --reverse
build:
	go build ./...
	pnpm build
check: lint
	pnpm check:generated
	$(MAKE) test test-contract build
