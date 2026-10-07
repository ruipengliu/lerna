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
	"google.golang.org/protobuf/encoding/protojson"
)

// 规则：G1、G4、G5、R3
func TestCLIExecutesThroughGateAndDisplaysPersistedEffect(t *testing.T) {
	target := simulator.New("idempotent")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, c := prepareStart(t, f)
	cli := interaction.CLI{Sessions: f.h.Sessions, Tasks: f.h.Tasks, Durable: f.h.Durable, Ledger: f.h.Ledger, Egress: f.h.Egress, Content: f.h.Content, Caller: &v1.Caller{UserId: "u", IssuerId: "egress"}, Domain: "d"}
	path := filepath.Join(t.TempDir(), "start.json")
	body, e := protojson.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, body, 0600); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e = cli.Run(f.ctx, []string{"execute", path}, &out); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e = cli.Run(f.ctx, []string{"operation", a.OperationId.LocalId}, &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "APPLIED") || !strings.Contains(out.String(), "SETTLED") {
		t.Fatalf("operation display %s", out.String())
	}
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e = cli.Run(f.ctx, []string{"observation", x.Send.ObservationRef.Name.LocalId}, &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "TRUSTED_IO") || !strings.Contains(out.String(), "bodyRef") {
		t.Fatalf("evidence display %s", out.String())
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatal("CLI bypassed gate")
	}
}
