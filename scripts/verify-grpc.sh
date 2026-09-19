#!/bin/sh
# grpc-execution-v1: actual processes, persistent delivery, shared domain cases.
set -eu
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.26.1
mkdir -p build/grpc
modules=not_run
generation=not_run
build=not_run
analysis=not_run
grpc=not_run
regression=not_run
execution=not_run
async=not_run
grants=not_run
result=1
write_status() {
 printf '{"profile":"grpc-execution-v1","dependencies":"%s","generation":"%s","build":"%s","analysis":"%s","grpc":"%s","regression":"%s","execution":"%s","async":"%s","grants":"%s","exit_code":%s}\n' "$modules" "$generation" "$build" "$analysis" "$grpc" "$regression" "$execution" "$async" "$grants" "$result" > build/grpc/status.json
}
trap write_status EXIT
write_status
go env -json GOVERSION GOOS GOARCH > build/grpc/environment.json
go list -m -json all > build/grpc/modules.json
modules=failed
if go mod verify > build/grpc/dependencies.log 2>&1; then modules=passed; fi
generation=failed
grpc_generated=$(mktemp -d build/grpc/generated.XXXXXX)
cp gen/harness/v1/*.pb.go "$grpc_generated/"
if sh scripts/generate.sh > build/grpc/generation.log 2>&1 && diff -r "$grpc_generated" gen/harness/v1 >> build/grpc/generation.log; then generation=passed; fi
rm -r "$grpc_generated"
build=failed
if go build -mod=readonly ./... > build/grpc/build.log 2>&1 && go build -mod=readonly -o build/grpc/contractcheck ./cmd/contractcheck >> build/grpc/build.log 2>&1; then build=passed; fi
analysis=failed
if go vet -mod=readonly ./... > build/grpc/analysis.log 2>&1; then analysis=passed; fi
grpc=failed
if go test -mod=readonly -p 1 -race -count=1 -timeout=3m ./adapters/transport/grpc ./profiles/asynccheck -run 'Test(Finite|DurableDelivery|GRPC)' -json > build/grpc/grpc.jsonl; then grpc=passed; fi
regression=failed
if go test -mod=readonly -p 1 -race -count=1 -timeout=10m ./authorization ./execution ./sdk ./profiles/asynccheck ./profiles/executioncheck ./adapters/transport/nodetls ./adapters/authorization/josegrant ./adapters/execution/local ./adapters/execution/router -json > build/grpc/regression.jsonl; then regression=passed; fi
if [ "$build" = passed ]; then
 execution=failed
 if build/grpc/contractcheck -profile synchronous-execution-v1 > build/grpc/execution.json; then execution=passed; fi
 async=failed
 if build/grpc/contractcheck -profile async-recovery-v1 > build/grpc/async.json; then async=passed; fi
 grants=failed
 if build/grpc/contractcheck -profile restricted-grants-v1 > build/grpc/grants.json; then grants=passed; fi
fi
if [ "$modules/$generation/$build/$analysis/$grpc/$regression/$execution/$async/$grants" = passed/passed/passed/passed/passed/passed/passed/passed/passed ]; then result=0; fi
exit "$result"
