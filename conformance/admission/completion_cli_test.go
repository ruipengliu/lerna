package admission_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/hosting"
	"google.golang.org/protobuf/encoding/protojson"
)

// 规则：G2、G6、完成-4、完成-5
func TestCLICompletesAndDisplaysFrozenResult(t *testing.T) {
	target := simulator.New("idempotent")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	scopeRequirement(t, f)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	cmd := &v1.BeginCompletionCommand{Header: header("cli-complete"), TaskId: f.task.Name, ProposalRef: p}
	body, e := protojson.Marshal(cmd)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "complete.json")
	if e = os.WriteFile(path, body, 0600); e != nil {
		t.Fatal(e)
	}
	cli := interaction.CLI{Sessions: f.h.Sessions, Tasks: f.h.Tasks, Durable: f.h.Durable, Caller: f.caller, Domain: "d"}
	cli.Progress = newManualProgress(t, hosting.ManualDependencies{Sessions: f.h.Sessions, Completions: f.h.Tasks, Cancellations: f.h.Tasks, TaskClosings: f.h.Tasks})
	var out bytes.Buffer
	if e = cli.Run(f.ctx, []string{"complete", "--json", path}, &out); e != nil {
		t.Fatal(e)
	}
	if e = cli.Run(f.ctx, []string{"recover"}, &out); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e = cli.Run(f.ctx, []string{"result", f.task.Name.LocalId}, &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "SUCCEEDED") || !strings.Contains(out.String(), "TARGET_RECORD") || !strings.Contains(out.String(), "TRUSTED_TEMPLATE") || !strings.Contains(out.String(), "SATISFIED") {
		t.Fatalf("result display %s", out.String())
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatal("completion repeated IO")
	}
}
