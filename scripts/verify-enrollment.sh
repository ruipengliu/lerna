#!/bin/sh
# node-enrollment-v1: real TLS and SQLite evidence, separate from future transports.
set -eu
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.26.1
mkdir -p build/enrollment
nodes=not_run
regression=not_run
grants=not_run
local_auth=not_run
result=1
write_status() {
 printf '{"profile":"node-enrollment-v1","nodes":"%s","regression":"%s","restricted_grants":"%s","local_auth":"%s","exit_code":%s}\n' "$nodes" "$regression" "$grants" "$local_auth" "$result" > build/enrollment/status.json
}
trap write_status EXIT
write_status
go env -json GOVERSION GOOS GOARCH > build/enrollment/environment.json
go list -m -json all > build/enrollment/modules.json
nodes=failed
if go test -mod=readonly -p 1 -race -count=1 -timeout=2m ./authorization -run 'TestNode|TestRealTLS' -json > build/enrollment/nodes.jsonl; then nodes=passed; fi
regression=failed
if go test -mod=readonly -p 1 -race -count=1 -timeout=10m ./authorization ./adapters/nodetls ./adapters/josegrant ./adapters/authlocal ./adapters/memoryauth ./execution ./sdk -json > build/enrollment/regression.jsonl; then regression=passed; fi
grants=failed
if go run -mod=readonly ./cmd/contractcheck -profile restricted-grants-v1 > build/enrollment/grants.json; then grants=passed; fi
local_auth=failed
if go run -mod=readonly ./cmd/contractcheck -profile local-auth-v1 > build/enrollment/local-auth.json; then local_auth=passed; fi
if [ "$nodes/$regression/$grants/$local_auth" = passed/passed/passed/passed ]; then result=0; fi
exit "$result"
