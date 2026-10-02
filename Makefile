.PHONY: setup dev dev-single dev-stop deps-up deps-down build check contracts-check sql-generate sql-check durable-check migrate-postgres migrate-sqlite

setup:
	node dev/run.mjs init
	go mod download
	pnpm install --frozen-lockfile
	python3 -m venv dev/.state/python
	dev/.state/python/bin/python -m pip install -r tools/requirements.txt

dev:
	node dev/run.mjs up

dev-single:
	node dev/run.mjs single

dev-stop:
	node dev/run.mjs stop

deps-up:
	node dev/run.mjs deps-up

deps-down:
	node dev/run.mjs deps-down

build:
	mkdir -p build
	go build -o build/harness ./cmd/harness
	go build -o build/harnessd ./cmd/harnessd
	go build -o build/harness-sim ./cmd/harness-sim
	go build -o build/harness-migrate ./cmd/harness-migrate
	pnpm -r run build

check:
	$(MAKE) domain-check
	go test -race ./...
	go vet ./...
	pnpm -r run typecheck
	$(MAKE) sql-check
	$(MAKE) contracts-check

contracts-check:
	node tools/check-contracts.mjs

sql-generate:
	node tools/generate-sql.mjs

sql-check:
	node tools/generate-sql.mjs --check

domain-generate:
	python3 tools/generate-domain.py

domain-check:
	python3 tools/generate-domain.py --check

durable-check:
	node tools/test-durable.mjs -v

migrate-postgres:
	node tools/migrate-local.mjs postgres

migrate-sqlite:
	node tools/migrate-local.mjs sqlite
