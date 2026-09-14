#!/bin/sh
# ws-reconnect-v1: durable invocation delivery and real process recovery.
set -eu
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.26.1
if [ -x .tools/protoc-36.1/bin/protoc ]; then PATH="$PWD/.tools/protoc-36.1/bin:$PATH"; export PATH; fi
mkdir -p build/reconnect
result=0
go env -json GOVERSION GOOS GOARCH > build/reconnect/environment.json
go list -m -json all > build/reconnect/modules.json
printf '{"profile":"ws-reconnect-v1"' > build/reconnect/status.json
stage() {
 name=$1
 shift
 if "$@"; then outcome=passed; else outcome=failed; result=1; fi
 printf ',"%s":"%s"' "$name" "$outcome" >> build/reconnect/status.json
}
stage dependencies go mod verify > build/reconnect/dependencies.log 2>&1
reconnect_generated=$(mktemp -d build/reconnect/generated.XXXXXX)
cp gen/harness/v1/*.pb.go "$reconnect_generated/"
stage generation sh scripts/generate.sh > build/reconnect/generation.log 2>&1
stage generated_diff diff -r "$reconnect_generated" gen/harness/v1 >> build/reconnect/generation.log 2>&1
rm -r "$reconnect_generated"
stage build go build -mod=readonly ./... > build/reconnect/build.log 2>&1
stage analysis go vet -mod=readonly ./... > build/reconnect/analysis.log 2>&1
stage reconnect go test -mod=readonly -p 1 -race -count=1 -timeout=3m ./authorization ./adapters/wsbinding ./profiles/asynccheck -run 'Test(Delivery|Reliable|WSReliable)' -json > build/reconnect/reconnect.jsonl 2> build/reconnect/reconnect.stderr
stage regression go test -mod=readonly -p 1 -race -count=1 -timeout=10m ./authorization ./tasks ./execution ./sdk ./protocol ./schema ./adapters/nodetls ./adapters/sqliteauth ./adapters/wsbinding ./adapters/grpcbinding ./profiles/sdkcontract ./profiles/executioncheck ./profiles/asynccheck -json > build/reconnect/regression.jsonl 2> build/reconnect/regression.stderr
printf ',"exit_code":%s}\n' "$result" >> build/reconnect/status.json
exit "$result"
