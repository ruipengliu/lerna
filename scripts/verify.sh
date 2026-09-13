#!/bin/sh
# One complete verification run; failures remain failures in the stage report.
set -eu
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.26.1
mkdir -p build
rm -f build/verification-stages.json build/sdk-contract-report.json build/sdk-sample.json build/local-auth-report.json build/durable-tasks-report.json build/bounded-worker-report.json build/task-control-report.json build/restricted-grants-report.json build/controlled-content-report.json build/bounded-answer-report.json build/task-updates-report.json build/synchronous-execution-report.json build/async-recovery-report.json build/resource-control-report.json build/capability-catalog-report.json build/api-brain-report.json build/bound-credentials-report.json build/credential-lifecycle-report.json build/long-term-memory-report.json build/personalized-context-report.json build/memory-deletion-report.json build/memory-extraction-quality-report.json build/fetch-local-report.json build/fetch-replay-report.json
dependencies=not_run
generation=not_run
compilation=not_run
analysis=not_run
tests=not_run
fixture=not_run
sample=not_run
authorization=not_run
tasks=not_run
worker=not_run
control=not_run
grants=not_run
content=not_run
answer=not_run
updates=not_run
execution=not_run
async=not_run
resources=not_run
catalog=not_run
api_brain=not_run
credential_broker=not_run
credential_lifecycle=not_run
long_term_memory=not_run
personalized_context=not_run
memory_deletion=not_run
memory_extraction_quality=not_run
fetch_local=not_run
fetch_replay=not_run
write_stages() {
  printf '{"go_target":"go1.26.1","protoc_target":"36.1","generator_target":"v1.36.11","dependencies":"%s","generation":"%s","compilation":"%s","static_analysis":"%s","tests":"%s","contract_fixture":"%s","sdk_sample":"%s","local_authorization":"%s","durable_tasks":"%s","bounded_worker":"%s","task_control":"%s","restricted_grants":"%s","controlled_content":"%s","bounded_answer":"%s","task_updates":"%s","synchronous_execution":"%s","async_recovery":"%s","resource_control":"%s","capability_catalog":"%s","api_brain":"%s","bound_credentials":"%s","credential_lifecycle":"%s","long_term_memory":"%s","personalized_context":"%s","memory_deletion":"%s","memory_extraction_quality":"%s","fetch_local":"%s","fetch_replay":"%s"}\n' \
    "$dependencies" "$generation" "$compilation" "$analysis" "$tests" "$fixture" "$sample" "$authorization" "$tasks" "$worker" "$control" "$grants" "$content" "$answer" "$updates" "$execution" "$async" "$resources" "$catalog" "$api_brain" "$credential_broker" "$credential_lifecycle" "$long_term_memory" "$personalized_context" "$memory_deletion" "$memory_extraction_quality" "$fetch_local" "$fetch_replay" > build/verification-stages.json
}
trap write_stages EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Replace any earlier success report before beginning work; a forced kill must
# not leave a previous run's passes looking like evidence for this candidate.
write_stages
dependencies=failed
go mod verify
dependencies=passed
generation=failed
verification_generated=$(mktemp -d build/generated.XXXXXX)
cp gen/harness/v1/*.pb.go "$verification_generated/"
sh scripts/generate.sh
diff -r "$verification_generated" gen/harness/v1
rm -r "$verification_generated"
generation=passed
compilation=failed
go build -mod=readonly ./...
go build -mod=readonly -trimpath -o build/contractcheck ./cmd/contractcheck
compilation=passed
analysis=failed
go vet -mod=readonly ./...
analysis=passed
tests=failed
# Keep package-level disk/process load bounded; individual concurrency tests
# still run their real competing goroutines, handles and child processes.
# Package suites now include multiple bounded recovery processes; their total
# exceeded 30 minutes in ticket 19 (the active case was only 19 seconds old).
# Allow 45 minutes per package; every case keeps its own existing deadline.
go test -mod=readonly -p 1 -race -timeout 45m ./...
tests=passed
fixture=failed
./build/contractcheck -profile sdk-contract-v1 > build/sdk-contract-report.json
fixture=passed
sample=failed
go run -mod=readonly ./examples/sdk > build/sdk-sample.json
sample=passed

authorization=failed
./build/contractcheck -profile local-auth-v1 > build/local-auth-report.json
authorization=passed

tasks=failed
./build/contractcheck -profile durable-tasks-v1 > build/durable-tasks-report.json
tasks=passed

worker=failed
./build/contractcheck -profile bounded-worker-v1 > build/bounded-worker-report.json
worker=passed

control=failed
./build/contractcheck -profile task-control-v1 > build/task-control-report.json
control=passed

grants=failed
./build/contractcheck -profile restricted-grants-v1 > build/restricted-grants-report.json
grants=passed

content=failed
./build/contractcheck -profile controlled-content-v1 > build/controlled-content-report.json
content=passed

answer=failed
./build/contractcheck -profile bounded-answer-v1 > build/bounded-answer-report.json
answer=passed

updates=failed
./build/contractcheck -profile task-updates-v1 > build/task-updates-report.json
updates=passed

execution=failed
./build/contractcheck -profile synchronous-execution-v1 > build/synchronous-execution-report.json
execution=passed

async=failed
./build/contractcheck -profile async-recovery-v1 > build/async-recovery-report.json
async=passed

resources=failed
./build/contractcheck -profile resource-control-v1 > build/resource-control-report.json
resources=passed

catalog=failed
./build/contractcheck -profile capability-catalog-v1 > build/capability-catalog-report.json
catalog=passed

api_brain=failed
./build/contractcheck -profile api-brain-v1 > build/api-brain-report.json
api_brain=passed

credential_broker=failed
./build/contractcheck -profile bound-credentials-v1 > build/bound-credentials-report.json
credential_broker=passed

credential_lifecycle=failed
./build/contractcheck -profile credential-lifecycle-v1 > build/credential-lifecycle-report.json
credential_lifecycle=passed

long_term_memory=failed
./build/contractcheck -profile long-term-memory-v1 > build/long-term-memory-report.json
long_term_memory=passed

personalized_context=failed
./build/contractcheck -profile personalized-context-v1 > build/personalized-context-report.json
personalized_context=passed

memory_deletion=failed
./build/contractcheck -profile memory-deletion-v1 > build/memory-deletion-report.json
memory_deletion=passed

# Extraction execution/recovery is covered by the full race suite above. This
# additional gate reports only the frozen local rules dataset, not model quality.
memory_extraction_quality=failed
go run -mod=readonly ./cmd/extractioncheck -profile local-rules-quality-v1 > build/memory-extraction-quality-report.json
memory_extraction_quality=passed

# Separate actual loopback HTTP from fixed-data replay. Public HTTPS remains
# an explicit bounded observation, never silently replaced by either profile.
fetch_local=failed
go run -mod=readonly ./cmd/fetchcheck -profile local-task-v1 > build/fetch-local-report.json
fetch_local=passed

fetch_replay=failed
go run -mod=readonly ./cmd/fetchcheck -profile fixed-replay-v1 > build/fetch-replay-report.json
fetch_replay=passed
