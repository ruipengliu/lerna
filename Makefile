SHELL := /bin/bash
export GOTOOLCHAIN := local
export GOFLAGS := -mod=readonly
FORMATTED := scripts/*.mjs sdk/typescript/src/**/*.ts sdk/typescript/src/*.ts sdk/typescript/*.json contract/schema/**/*.json internal/**/*.json conformance/fixtures/**/*.json package.json pnpm-workspace.yaml .prettierrc.json .github/workflows/*.yaml

.PHONY: bootstrap generate fmt lint test test-race test-contract test-integration test-integration-race check-integration-prerequisites build check
bootstrap:
	node scripts/bootstrap.mjs
generate:
	pnpm generate
fmt:
	gofmt -w contract conformance runtime host internal adapters components domain cmd
	pnpm exec prettier --write $(FORMATTED)
lint:
	node scripts/check-go-format.mjs
	pnpm exec prettier --check $(FORMATTED)
	go vet ./...
	pnpm lint
test:
	node --test scripts/contract-runner.test.mjs scripts/bounded-build.test.mjs scripts/component-integration-race.test.mjs
	node scripts/test-generator.mjs
	go test ./...
	pnpm test
test-race:
	go test -race ./...
check-integration-prerequisites:
	@test "$$(go env CGO_ENABLED)" = 1 && test "$$(go env GOOS)" = linux || { echo "SQLite integration requires Linux, CGO_ENABLED=1 and a C compiler" >&2; exit 1; }
	@test -n "$$LERNA_TEST_POSTGRES_DSN" || { echo "LERNA_TEST_POSTGRES_DSN is required (dedicated PostgreSQL test database)" >&2; exit 1; }
	@command -v psql >/dev/null || { echo "psql is required for the complete historical PG dump restore" >&2; exit 1; }
	@test -n "$$LERNA_TEST_OWNED_SCOPE_REGISTRY" || { echo "LERNA_TEST_OWNED_SCOPE_REGISTRY is required (absolute durable ownership ledger)" >&2; exit 1; }
	psql --version
	cd conformance/fixtures/durable-work/pg-v1 && sha256sum -c SHA256SUMS
	cd conformance/fixtures/durable-work/sqlite-v1 && sha256sum -c SHA256SUMS
	cd conformance/fixtures/durable-work/pg-v2 && sha256sum -c SHA256SUMS
	cd conformance/fixtures/durable-work/sqlite-v2 && sha256sum -c SHA256SUMS
test-integration: check-integration-prerequisites
	go test -count=1 -tags=integration -timeout=120s ./conformance/recovery/...
	go test -p=1 -count=1 -tags=integration -timeout=120s ./conformance/component ./conformance/internal/decisionfixture ./conformance/internal/contentfixture ./adapters/objectstore/local
test-integration-race: check-integration-prerequisites
	go test -p=1 -count=1 -race -tags=integration -timeout=120s ./conformance/recovery/...
	bash scripts/test-component-integration-race.sh
	go test -p=1 -count=1 -race -tags=integration -timeout=120s ./conformance/internal/decisionfixture ./conformance/internal/contentfixture ./adapters/objectstore/local
test-contract:
	node scripts/test-contract.mjs
	node scripts/test-contract.mjs --reverse
	node scripts/test-contract-1_2.mjs
	node scripts/test-contract-1_2.mjs --reverse
build:
	go build ./...
	pnpm build
check: lint
	pnpm check:generated
	$(MAKE) test test-contract build
