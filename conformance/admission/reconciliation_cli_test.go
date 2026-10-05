package admission_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G11、R6
func TestCLIReconciliationRequestPauseResumeAndRecover(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	cli := interaction.CLI{Ledger: f.h.Ledger, Sessions: f.h.Sessions, Observations: f.h.Content, Grants: f.h.Grants, Caller: &v1.Caller{UserId: "u", IssuerId: "local-cli"}, Domain: "d"}
	write := func(name string, m proto.Message) string {
		t.Helper()
		b, e := protojson.Marshal(m)
		if e != nil {
			t.Fatal(e)
		}
		path := filepath.Join(t.TempDir(), name)
		if e = os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
		return path
	}
	command := reconciliationCommand(f, a, cap, grant)
	command.Header.Identity.IssuerId = "local-cli"
	var out bytes.Buffer
	if e = cli.Run(f.ctx, []string{"request-reconciliation", "--json", write("request.json", command)}, &out); e != nil {
		t.Fatal(e)
	}
	receipt := new(v1.CommandReceipt)
	if e = protojson.Unmarshal(out.Bytes(), receipt); e != nil {
		t.Fatal(e)
	}
	accepted(t, receipt, nil)
	for _, action := range []string{"PAUSE", "RESUME"} {
		p, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
		if e != nil {
			t.Fatal(e)
		}
		h := ledgerHeader("cli-" + action)
		h.Identity.IssuerId = "local-cli"
		out.Reset()
		if e = cli.Run(f.ctx, []string{"control-reconciliation", "--json", write(action+".json", &v1.ControlReconciliationCommand{Header: h, OperationId: a.OperationId, ExpectedRevision: p.Ref.Revision, Action: action})}, &out); e != nil {
			t.Fatal(e)
		}
		receipt = new(v1.CommandReceipt)
		if e = protojson.Unmarshal(out.Bytes(), receipt); e != nil {
			t.Fatal(e)
		}
		accepted(t, receipt, nil)
	}
	if e = cli.Run(f.ctx, []string{"recover"}, &out); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e = cli.Run(f.ctx, []string{"reconciliation", a.OperationId.LocalId}, &out); e != nil {
		t.Fatal(e)
	}
	p := new(v1.Reconciliation)
	if e = protojson.Unmarshal(out.Bytes(), p); e != nil || p.State != "COMPLETED" {
		t.Fatalf("CLI state: %v %v", p, e)
	}
	out.Reset()
	if e = cli.Run(f.ctx, []string{"reconciliation-query", "--json", write("ref.json", p.QueryRefs[0])}, &out); e != nil {
		t.Fatal(e)
	}
	q := new(v1.ReconciliationQuery)
	if e = protojson.Unmarshal(out.Bytes(), q); e != nil || q.ObservationRef == nil || q.InterpretationRef == nil {
		t.Fatalf("CLI relation: %v %v", q, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("CLI repeat: %v %v", requests, effects)
	}
}
