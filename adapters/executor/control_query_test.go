package executor

import (
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
)

func TestPairedPeerReadsOriginalTaskStopGateBeforeAnyOperationAdmission(t *testing.T) {
	f := newDeviceFixture(t)
	stop := f.control(t, "active", "paused", 2)
	_, receipt := f.command(t, "execution.control", stop.TaskID, execution.ControlInput{TaskRef: f.b.Intent.TaskRef, Snapshot: stop})
	if receipt.Stage != "applied" {
		t.Fatalf("original stop gate: %+v", receipt)
	}
	view := deviceQuery[execution.ControlView](t, f, "execution.control.get", stop.TaskID, execution.ControlGetInput{TaskID: stop.TaskID})
	if view.Gate.TaskRef.OwnerID != f.b.AuthorityID || view.Gate.Control != "paused" || view.Gate.ControlRevision != 2 || len(view.Windows) != 1 || view.Windows[0].WindowID != stop.WindowID {
		t.Fatalf("original task gate identity: %+v", view)
	}
	bad := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.b.EndpointID, QueryID: api.NewID("query"), Method: "task.get", TargetID: stop.TaskID, Payload: api.Raw(map[string]string{"task_id": stop.TaskID})}
	if _, _, err := f.h.Call(f.ctx, f.peer, "query", api.Raw(bad)); !api.IsCode(err, "unsupported") {
		t.Fatalf("device disclosed cloud Task: %v", err)
	}
}
