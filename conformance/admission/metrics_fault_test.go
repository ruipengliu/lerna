//go:build fault

package admission_test

import (
	"testing"

	"strings"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G10、G11、R3、V4
func TestAdmissionReceiptLossDoesNotReconstructLatencyOnQueryReplayOrRestart(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	p := f.propose(t, nil)
	c := &v1.AdmitCommand{Header: header("metrics-lost-original"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant}
	ctx, e := sqlite.WithFault(f.ctx, "tasks.admit", sqlite.LoseReceipt)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.Admit(ctx, f.caller, c)
	if e == nil || r != nil {
		t.Fatalf("actual lost commit endpoint: %v %v", r, e)
	}
	q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, c.Header.Identity)
	if e != nil || q.GetReceipt().GetDecision() != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("actual durable original decision: %v %v", q, e)
	}
	missing, e := f.h.Tasks.QueryAdmissionMetrics(f.ctx, f.caller)
	if e != nil || missing.Source.Availability != "MISSING" || missing.Samples != 0 || missing.DecisionsWithoutEndpoint != 1 || missing.DurationSumNs != nil || missing.LastSampleAtUnixMs != nil {
		t.Fatalf("query fabricated original monotonic endpoint: %v %v", missing, e)
	}
	local, e := f.h.QueryMetrics(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := protojson.Marshal(local)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(strings.Join(readMetricGuidance(t, encoded), "\n"), "original admission endpoint") {
		t.Error("lost original decision endpoint lacks local investigation guidance")
	}
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, c)
	accepted(t, r, e)
	if !proto.Equal(r, q.Receipt) {
		t.Fatal("lost original decision changed on replay")
	}
	replayed, e := f.h.Tasks.QueryAdmissionMetrics(f.ctx, f.caller)
	if e != nil || replayed.Samples != 0 || replayed.Replays != 1 || replayed.DecisionsWithoutEndpoint != 1 || replayed.DurationSumNs != nil {
		t.Fatalf("replay fabricated missing endpoint: %v %v", replayed, e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	restarted, e := f.h.Tasks.QueryAdmissionMetrics(f.ctx, f.caller)
	if e != nil || restarted.Source.Availability != "NO_SAMPLES" || restarted.Entries != 0 || restarted.Samples != 0 || restarted.DurationSumNs != nil || restarted.ProcessInstance == missing.ProcessInstance || f.calls.Load() != 0 {
		t.Fatalf("restart fabricated old process samples: %v %v target=%d", restarted, e, f.calls.Load())
	}
}
