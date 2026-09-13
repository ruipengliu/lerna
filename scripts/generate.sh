#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.26.1
test "$(protoc --version)" = 'libprotoc 36.1' || {
  echo 'protoc 36.1 required' >&2; exit 1;
}
mkdir -p .tools
go build -mod=readonly -trimpath -o .tools/protoc-gen-go google.golang.org/protobuf/cmd/protoc-gen-go
protoc --plugin=protoc-gen-go=.tools/protoc-gen-go --go_out=. --go_opt=module=lerna proto/harness/v1/contract.proto proto/harness/v1/authorization.proto proto/harness/v1/tasks.proto proto/harness/v1/artifacts.proto proto/harness/v1/execution.proto proto/harness/v1/memory.proto proto/harness/v1/connection.proto
go build -mod=readonly -trimpath -o .tools/protoc-gen-go-grpc google.golang.org/grpc/cmd/protoc-gen-go-grpc
protoc --plugin=protoc-gen-go-grpc=.tools/protoc-gen-go-grpc --go-grpc_out=. --go-grpc_opt=module=lerna proto/harness/v1/execution.proto proto/harness/v1/connection.proto
