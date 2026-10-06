//go:build fault

package admission_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G3、G5、G11、R3、V4
func TestLocalMetricGuidanceExplainsRetainedMissingOrUninterpretableOperationMetadata(t *testing.T) {
	for _, mode := range []string{"queryability-missing", "effect-uninterpretable"} {
		t.Run(mode, func(t *testing.T) {
			target := simulator.New("opaque")
			target.SetBehavior("accept-and-delay")
			f := newFixtureWithTarget(t, 100, 80, false, target)
			a, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || original.Effect.Outcome != "UNKNOWN" {
				t.Fatalf("actual original unknown: %v %v", original, e)
			}
			changed := proto.Clone(original).(*v1.Operation)
			if mode == "queryability-missing" {
				changed.Execution.Attempt.Capabilities = nil
			} else {
				changed.Effect.Outcome = "UNINTERPRETABLE_FUTURE_OUTCOME"
			}
			encoded, e := proto.Marshal(changed)
			if e != nil {
				t.Fatal(e)
			}
			// 仅故障构建可损坏真实原记录；断言仍从公开负责方读取，不手工造指标对象。
			if e = f.h.StorageFaultSQL("UPDATE operations SET record=X'" + hex.EncodeToString(encoded) + "' WHERE user_id='u' AND domain_id='d/ledger' AND id='" + a.OperationId.LocalId + "'"); e != nil {
				t.Fatal(e)
			}
			metrics, e := f.h.QueryMetrics(f.ctx, f.caller)
			if e != nil || metrics.Ledger.Source.Availability != "PARTIAL" || mode == "queryability-missing" && (metrics.Ledger.Unknown.Total != 1 || metrics.Ledger.Unknown.QueryabilityMissing != 1) || mode == "effect-uninterpretable" && (metrics.Ledger.Unknown.Total != 0 || metrics.Ledger.UninterpretableOperations != 1) {
				t.Fatalf("retained metadata blind spot became a healthy zero: %v %v", metrics, e)
			}
			body, e := protojson.Marshal(metrics)
			if e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(strings.Join(readMetricGuidance(t, body), "\n"), "missing or uninterpretable operation metadata") {
				t.Error("original metadata blind spot lacks local investigation guidance")
			}
			after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			requests, effects := target.Snapshot()
			if e != nil || !proto.Equal(changed, after) || len(requests) != 1 || len(effects) != 0 || f.calls.Load() != 1 {
				t.Fatalf("guidance rewrote original metadata or caused I/O: %v %v", after, e)
			}
		})
	}
}
