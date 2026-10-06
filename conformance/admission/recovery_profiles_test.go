package admission_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G11、R7、开始-2、V4
func TestRestartReadsEveryOriginalSimulatorProfileWithoutRenewingExpiredKeys(t *testing.T) {
	for _, profile := range []string{"idempotent", "idempotent-expiring", "idempotent-evicting", "idempotent-queryable", "queryable", "opaque"} {
		t.Run(profile, func(t *testing.T) {
			target := simulator.NewBillingTarget(25)
			f := newFixtureWithTarget(t, 100, 80, false, target)
			cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
			if e != nil {
				t.Fatal(e)
			}
			cap.Ref, cap.ApprovedBy = nil, nil
			cap.AdapterRef.Name.LocalId = "simulator-" + profile
			finite := strings.HasSuffix(profile, "expiring") || strings.HasSuffix(profile, "evicting")
			if finite {
				cap.IdempotencyRetentionMs = 100
			}
			r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("recovery-profile"), Capability: cap})
			accepted(t, r, e)
			f.capability = r.ResultRef
			a, _ := prepareStart(t, f)
			original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			if finite {
				remaining := time.Until(time.UnixMilli(original.Execution.Attempt.GetKeyValidUntilUnixMs()))
				if remaining > 0 {
					time.Sleep(remaining + time.Millisecond)
				}
			}
			if e = f.h.Close(); e != nil {
				t.Fatal(e)
			}
			reopened, e := assembly.Open(f.path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			f.h = reopened
			after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || !proto.Equal(after, original) {
				t.Fatalf("original profile rewritten: %v %v", after, e)
			}
			calls, effects := target.Target.Snapshot()
			if len(calls) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
				t.Fatal("history read sent original operation")
			}
			if finite && after.Execution.Attempt.GetKeyValidUntilUnixMs() > time.Now().UnixMilli() {
				t.Fatal("expired original key was renewed")
			}
		})
	}
}
