package asynccheck

import (
	"bytes"
	"context"
	"embed"
	"encoding/gob"
	"encoding/json"
	"lerna/authorization"
	"lerna/execution"
	"os"
	"path/filepath"
	"testing"
	"time"
)

//go:embed testdata/11/*/*
var old11 embed.FS

func TestActualV11Upgrade(t *testing.T) {
	for _, point := range []string{"cancel-registered", "report-saved"} {
		t.Run(point, func(t *testing.T) {
			ctx := context.Background()
			h, e := fresh(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer h.destroy()
			read := func(name string) []byte {
				b, e := old11.ReadFile("testdata/11/" + point + "/" + name)
				if e != nil {
					t.Fatal(e)
				}
				return b
			}
			var state authorization.State
			if e = gob.NewDecoder(bytes.NewReader(read("authority.gob"))).Decode(&state); e != nil {
				t.Fatal(e)
			}
			snap, e := h.db.Load(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if e = h.db.Commit(ctx, snap.Version, state); e != nil {
				t.Fatal(e)
			}
			var cp checkpoint
			if e = json.Unmarshal(read("checkpoint.json"), &cp); e != nil {
				t.Fatal(e)
			}
			h.close()
			if e = os.WriteFile(filepath.Join(h.root, "target.db"), read("target.db"), 0600); e != nil {
				t.Fatal(e)
			}
			reopened, e := open(ctx, h.root, cp.Token)
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.close()
			reopened.clock.advance(5 * time.Second)
			before, e := reopened.exec.GetInvocation(ctx, cp.Request.OperationID)
			if e != nil || before.Async == nil || !before.Started {
				t.Fatal(before, e)
			}
			// A second exact descriptor can coexist without reinterpreting the old partition.
			cap := capability()
			cap.Name = "independent.api"
			cap.Implementation = "separate-reference"
			other, e := execution.New(reopened.grants, reopened.work, reopened.access, reopened.target, reopened.binding, cap, config(), reopened.operation)
			if e != nil {
				t.Fatal(e)
			}
			if e = other.Drain(ctx, 1); e != nil {
				t.Fatal(e)
			}
			if _, e = reopened.client.Invoke(ctx, cp.Request, ""); e != nil {
				t.Fatal(e)
			}
			if point == "cancel-registered" {
				c, e := reopened.client.GetCancel(ctx, cp.Cancel)
				if e != nil || !c.Sent || c.Progress != "UNKNOWN" {
					t.Fatal(c, e)
				}
				if _, e = reopened.exec.RunCancel(ctx, cp.Cancel); e != nil {
					t.Fatal(e)
				}
				if e = reopened.target.Complete(ctx, cp.Request.OperationID); e != nil {
					t.Fatal(e)
				}
				if _, e = reopened.client.Reconcile(ctx, cp.Request.OperationID); e != nil {
					t.Fatal(e)
				}
			}
			if e = reopened.exec.Drain(ctx, 16); e != nil {
				t.Fatal(e)
			}
			after, e := reopened.exec.GetInvocation(ctx, cp.Request.OperationID)
			if e != nil || after.Permit != state.Signed.Uses[cp.Request.OperationID] || after.Request != before.Request || after.Checks < before.Checks || after.Applied != after.Revision || after.Effect != "CONFIRMED" {
				t.Fatal(after, e)
			}
			target, e := reopened.target.Snapshot(ctx)
			if e != nil || target.Jobs != 1 || target.Changes != 1 {
				t.Fatal(target, e)
			}
		})
	}
}
