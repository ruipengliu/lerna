#!/bin/sh
# authorization-offline-v1: authorization synchronization and finite offline execution.
set -eu
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.26.1
if [ -x .tools/protoc-36.1/bin/protoc ]; then PATH="$PWD/.tools/protoc-36.1/bin:$PATH"; export PATH; fi
mkdir -p build/offline
result=0
go env -json GOVERSION GOOS GOARCH > build/offline/environment.json
go list -m -json all > build/offline/modules.json
printf '{"profile":"authorization-offline-v1"' > build/offline/status.json
stage() {
 name=$1
 shift
 if "$@"; then outcome=passed; else outcome=failed; result=1; fi
 printf ',"%s":"%s"' "$name" "$outcome" >> build/offline/status.json
}
stage dependencies go mod verify > build/offline/dependencies.log 2>&1
offline_generated=$(mktemp -d build/offline/generated.XXXXXX)
cp gen/harness/v1/*.pb.go "$offline_generated/"
stage generation sh scripts/generate.sh > build/offline/generation.log 2>&1
stage generated_diff diff -r "$offline_generated" gen/harness/v1 >> build/offline/generation.log 2>&1
rm -r "$offline_generated"
stage build go build -mod=readonly ./... > build/offline/build.log 2>&1
stage analysis go vet -mod=readonly ./... > build/offline/analysis.log 2>&1
stage offline go test -mod=readonly -p 1 -race -count=1 -timeout=3m ./authorization ./adapters/transport/ws ./profiles/asynccheck -run 'Test(Offline|SystemOffline|WSOffline)' -json > build/offline/offline.jsonl 2> build/offline/offline.stderr
stage regression go test -mod=readonly -p 1 -race -count=1 -timeout=10m ./authorization ./tasks ./execution ./sdk ./protocol ./schema ./adapters/transport/nodetls ./adapters/authorization/josegrant ./adapters/authorization/sqlite ./adapters/transport/ws ./adapters/transport/grpc ./profiles/sdkcontract ./profiles/executioncheck ./profiles/asynccheck -json > build/offline/regression.jsonl 2> build/offline/regression.stderr
printf ',"exit_code":%s}\n' "$result" >> build/offline/status.json
exit "$result"
