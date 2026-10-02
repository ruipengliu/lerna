.PHONY: setup dev dev-single dev-stop deps-up deps-down build check contracts-check

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
	pnpm -r run build

check:
	go test -race ./...
	go vet ./...
	pnpm -r run typecheck
	$(MAKE) contracts-check

contracts-check:
	node tools/check-contracts.mjs
