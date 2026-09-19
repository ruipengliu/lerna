#!/bin/sh
# task-owner-handoff-v1: sealed ownership transfer and recovery.
set -eu
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.26.1
if [ -x .tools/protoc-36.1/bin/protoc ]; then PATH="$PWD/.tools/protoc-36.1/bin:$PATH"; export PATH; fi
mkdir -p build/handoff
result=0
go env -json GOVERSION GOOS GOARCH > build/handoff/environment.json
go list -m -json all > build/handoff/modules.json
printf '{"profile":"task-owner-handoff-v1"' > build/handoff/status.json
stage() {
 name=$1
 shift
 if "$@"; then outcome=passed; else outcome=failed; result=1; fi
 printf ',"%s":"%s"' "$name" "$outcome" >> build/handoff/status.json
}
stage dependencies go mod verify > build/handoff/dependencies.log 2>&1
handoff_generated=$(mktemp -d build/handoff/generated.XXXXXX)
cp gen/harness/v1/*.pb.go "$handoff_generated/"
stage generation sh scripts/generate.sh > build/handoff/generation.log 2>&1
stage generated_diff diff -r "$handoff_generated" gen/harness/v1 >> build/handoff/generation.log 2>&1
rm -r "$handoff_generated"
stage build go build -mod=readonly ./... > build/handoff/build.log 2>&1
stage analysis go vet -mod=readonly ./... > build/handoff/analysis.log 2>&1
stage handoff go test -mod=readonly -p 1 -race -count=1 -timeout=3m ./tasks ./brain ./adapters/transport/ws ./profiles/asynccheck -run 'Test(Handoff|WSHandoff)' -json > build/handoff/handoff.jsonl 2> build/handoff/handoff.stderr
stage regression go test -mod=readonly -p 1 -race -count=1 -timeout=10m ./authorization ./tasks ./brain ./execution ./sdk ./protocol ./schema ./adapters/transport/nodetls ./adapters/authorization/josegrant ./adapters/authorization/sqlite ./adapters/transport/ws ./adapters/transport/grpc ./profiles/sdkcontract ./profiles/executioncheck ./profiles/asynccheck -json > build/handoff/regression.jsonl 2> build/handoff/regression.stderr
# Answer-profile failures predate this ticket. Compare the exact failing test
# identities against the frozen pre-change commit; retain both raw failed runs.
answer_compatibility() {
 handoff_root=$PWD
 handoff_base=$(mktemp -d build/handoff/baseline.XXXXXX)
 git archive 5e912aa29640b78ce7c4fc30ef55cf451eccf804 | tar -x -C "$handoff_base"
 go test -mod=readonly -race -count=1 ./profiles/answer -json > build/handoff/answer.jsonl 2> build/handoff/answer.stderr || handoff_answer_exit=$?
 (cd "$handoff_base" && go test -mod=readonly -race -count=1 ./profiles/answer -json > "$handoff_root/build/handoff/baseline-answer.jsonl" 2> "$handoff_root/build/handoff/baseline-answer.stderr") || handoff_baseline_exit=$?
 rm -r "$handoff_base"
 python3 - <<'PY_COMPARE'
import json
from pathlib import Path
def failures(name):
    events = [json.loads(line) for line in Path('build/handoff/'+name+'.jsonl').read_text().splitlines()]
    assert any(e['Action'] in ('pass','fail') and 'Test' not in e for e in events), 'incomplete run'
    return sorted(e['Test'] for e in events if e['Action']=='fail' and 'Test' in e)
current, baseline = failures('answer'), failures('baseline-answer')
result = {'baseline_commit':'5e912aa29640b78ce7c4fc30ef55cf451eccf804','current_failures':current,'baseline_failures':baseline,'same_failures':current==baseline}
Path('build/handoff/answer-compatibility.json').write_text(json.dumps(result,indent=2)+'\n')
assert len(baseline)==46 and current==baseline, 'answer profile failure set changed'
PY_COMPARE
}
stage answer_compatibility answer_compatibility > build/handoff/answer-comparison.log 2>&1
printf ',"exit_code":%s}\n' "$result" >> build/handoff/status.json
exit "$result"
