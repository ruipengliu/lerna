//go:build fault

package fault_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type querySyncEgress struct{ command *v1.StartExecutionCommand }

func (g *querySyncEgress) Invoke(_ context.Context, _ *v1.Caller, cmd *v1.StartExecutionCommand) (*v1.CommandReceipt, error) {
	g.command = proto.Clone(cmd).(*v1.StartExecutionCommand)
	return nil, errors.New("query boundary captured")
}

// 规则：G3、G5、G11
func TestQueryActualSyncFailurePreventsRead(t *testing.T) {
	for _, mode := range []string{"vfs", "native"} {
		if mode == "native" && runtime.GOOS != "darwin" {
			continue
		}
		t.Run(mode, func(t *testing.T) {
			target := simulator.New("queryable")
			target.SetBehavior("drop-after-apply")
			server := httptest.NewServer(target)
			defer server.Close()
			path := filepath.Join(t.TempDir(), "state.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			c := prepareQueryFault(t, h, server.URL)
			actor := &v1.Caller{UserId: "u", IssuerId: "host"}
			r, e := h.Ledger.RequestReconciliation(context.Background(), actor, c)
			requireAccepted(t, r, e)
			gate := new(querySyncEgress)
			h.Ledger.WithReconciliation(h.Tasks, h.Grants, gate)
			if e = h.Ledger.ProcessReconciliations(context.Background(), actor); e == nil || gate.command == nil {
				t.Fatalf("query not captured: %v", e)
			}
			r, e = h.Tasks.StartExecution(context.Background(), &v1.Caller{UserId: "u", IssuerId: "egress"}, gate.command)
			requireAccepted(t, r, e)
			plan, e := h.Ledger.QueryReconciliation(context.Background(), actor, c.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			r, e = h.Ledger.ControlReconciliation(context.Background(), actor, &v1.ControlReconciliationCommand{Header: executionHeader("hold-query-sync"), OperationId: c.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "PAUSE"})
			requireAccepted(t, r, e)
			writeStart(t, path, gate.command)
			h.Close()
			child := exec.Command(os.Args[0], "-test.run=^TestDispatchSyncChild$")
			child.Env = append(os.Environ(), "LERNA_DISPATCH_SYNC_DB="+path, "LERNA_DISPATCH_SYNC_MODE="+mode)
			if out, e := child.CombinedOutput(); e != nil {
				t.Fatalf("child: %v %s", e, out)
			}
			b, e := os.ReadFile(path + ".sync-result")
			if e != nil {
				t.Fatal(e)
			}
			var result struct {
				Failed         bool
				NativeFailures uint64
			}
			if e = json.Unmarshal(b, &result); e != nil {
				t.Fatal(e)
			}
			if !result.Failed || mode == "native" && result.NativeFailures == 0 {
				t.Fatalf("sync failure not exercised: %+v", result)
			}
			requests, effects := target.Snapshot()
			if len(requests) != 1 || len(effects) != 1 {
				t.Fatalf("P5 barrier leaked query: %v %v", requests, effects)
			}
		})
	}
}
