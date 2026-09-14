#!/bin/sh
# internal-agents-v1: bounded parent/child handoff and reconciliation.
set -eu
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.26.1
if [ -x .tools/protoc-36.1/bin/protoc ]; then PATH="$PWD/.tools/protoc-36.1/bin:$PATH"; export PATH; fi
mkdir -p build/agents
result=0
go env -json GOVERSION GOOS GOARCH > build/agents/environment.json
go list -m -json all > build/agents/modules.json
printf '{"profile":"internal-agents-v1"' > build/agents/status.json
stage() {
 name=$1
 shift
 if "$@"; then outcome=passed; else outcome=failed; result=1; fi
 printf ',"%s":"%s"' "$name" "$outcome" >> build/agents/status.json
}
stage dependencies go mod verify > build/agents/dependencies.log 2>&1
agents_generated=$(mktemp -d build/agents/generated.XXXXXX)
cp gen/harness/v1/*.pb.go "$agents_generated/"
stage generation sh scripts/generate.sh > build/agents/generation.log 2>&1
stage generated_diff diff -r "$agents_generated" gen/harness/v1 >> build/agents/generation.log 2>&1
rm -r "$agents_generated"
stage build go build -mod=readonly ./... > build/agents/build.log 2>&1
stage analysis go vet -mod=readonly ./... > build/agents/analysis.log 2>&1
stage agents go test -mod=readonly -p 1 -race -count=1 -timeout=3m ./tasks ./brain ./adapters/wsbinding ./profiles/asynccheck -run 'Test(Delegation|WSDelegation)' -json > build/agents/agents.jsonl 2> build/agents/agents.stderr
stage regression go test -mod=readonly -p 1 -race -count=1 -timeout=10m ./authorization ./tasks ./brain ./execution ./sdk ./protocol ./schema ./adapters/nodetls ./adapters/josegrant ./adapters/sqliteauth ./adapters/wsbinding ./adapters/grpcbinding ./profiles/sdkcontract ./profiles/executioncheck ./profiles/asynccheck -json > build/agents/regression.jsonl 2> build/agents/regression.stderr
# Answer-profile failures predate this ticket. Compare the exact failing test
# identities against the frozen pre-change commit; retain both raw failed runs.
answer_compatibility() {
 agents_root=$PWD
 agents_base=$(mktemp -d build/agents/baseline.XXXXXX)
 git archive 7bd33ab7981a511832fa39791bbcd3b4c22cdbb2 | tar -x -C "$agents_base"
 go test -mod=readonly -race -count=1 ./profiles/answer -json > build/agents/answer.jsonl 2> build/agents/answer.stderr || agents_answer_exit=$?
 (cd "$agents_base" && go test -mod=readonly -race -count=1 ./profiles/answer -json > "$agents_root/build/agents/baseline-answer.jsonl" 2> "$agents_root/build/agents/baseline-answer.stderr") || agents_baseline_exit=$?
 rm -r "$agents_base"
 python3 - <<'PY_COMPARE'
import json
from pathlib import Path
def failures(name):
    events = [json.loads(line) for line in Path('build/agents/'+name+'.jsonl').read_text().splitlines()]
    assert any(e['Action'] in ('pass','fail') and 'Test' not in e for e in events), 'incomplete run'
    return sorted(e['Test'] for e in events if e['Action']=='fail' and 'Test' in e)
current, baseline = failures('answer'), failures('baseline-answer')
result = {'baseline_commit':'7bd33ab7981a511832fa39791bbcd3b4c22cdbb2','current_failures':current,'baseline_failures':baseline,'same_failures':current==baseline}
Path('build/agents/answer-compatibility.json').write_text(json.dumps(result,indent=2)+'\n')
assert len(baseline)==46 and current==baseline, 'answer profile failure set changed'
PY_COMPARE
}
stage answer_compatibility answer_compatibility > build/agents/answer-comparison.log 2>&1
printf ',"exit_code":%s}\n' "$result" >> build/agents/status.json
exit "$result"
