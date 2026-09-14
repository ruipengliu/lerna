#!/bin/sh
# ws-query-v1: real mTLS WebSocket peers, duplex reads and bounded progress.
set -eu
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.26.1
if [ -x .tools/protoc-36.1/bin/protoc ]; then PATH="$PWD/.tools/protoc-36.1/bin:$PATH"; export PATH; fi
mkdir -p build/ws
result=0
go env -json GOVERSION GOOS GOARCH > build/ws/environment.json
go list -m -json all > build/ws/modules.json
printf '{"profile":"ws-query-v1"' > build/ws/status.json
stage() {
 name=$1
 shift
 if "$@"; then outcome=passed; else outcome=failed; result=1; fi
 printf ',"%s":"%s"' "$name" "$outcome" >> build/ws/status.json
}
stage dependencies go mod verify > build/ws/dependencies.log 2>&1
ws_generated=$(mktemp -d build/ws/generated.XXXXXX)
cp gen/harness/v1/*.pb.go "$ws_generated/"
stage generation sh scripts/generate.sh > build/ws/generation.log 2>&1
stage generated_diff diff -r "$ws_generated" gen/harness/v1 >> build/ws/generation.log 2>&1
rm -r "$ws_generated"
# Existing .proto consumers may still import TaskRef through contract.proto.
mkdir -p build/ws/proto-compat
cat > build/ws/proto-compat/consumer.proto <<'PROTO'
syntax = "proto3";
package compatibility;
import "proto/harness/v1/contract.proto";
message Consumer { harness.v1.TaskRef task = 1; }
PROTO
stage source_compatibility protoc -I . --descriptor_set_out=build/ws/proto-compat/consumer.pb build/ws/proto-compat/consumer.proto > build/ws/proto-compat.log 2>&1
stage build go build -mod=readonly ./... > build/ws/build.log 2>&1
stage analysis go vet -mod=readonly ./... > build/ws/analysis.log 2>&1
stage websocket go test -mod=readonly -p 1 -race -count=1 -timeout=3m ./adapters/wsbinding ./profiles/asynccheck -run 'Test(WS|FiniteConfiguration)' -json > build/ws/websocket.jsonl 2> build/ws/websocket.stderr
stage regression go test -mod=readonly -p 1 -race -count=1 -timeout=10m ./authorization ./execution ./catalog ./schema ./protocol ./sdk ./adapters/nodetls ./adapters/catalogauth ./adapters/cataloglocal ./adapters/sqlitecatalog ./adapters/grpcbinding ./profiles/sdkcontract ./profiles/catalogcheck ./profiles/asynccheck -json > build/ws/regression.jsonl 2> build/ws/regression.stderr
printf ',"exit_code":%s}\n' "$result" >> build/ws/status.json
exit "$result"
