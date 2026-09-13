export GOTOOLCHAIN := go1.26.1

.PHONY: generate build test verify sample
generate:
	sh scripts/generate.sh
build:
	go build -mod=readonly ./...
test:
	go test -mod=readonly -p 1 -race -timeout 45m ./...
verify:
	sh scripts/verify.sh
sample:
	go run -mod=readonly ./examples/sdk
