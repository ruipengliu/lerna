//go:build fault

package admission_test

import (
	"encoding/hex"
	"testing"
	"time"

	"strings"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G11、开始-2、V4
func TestRestartRefusesUnsupportedOriginalExecutionImplementation(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, _ := prepareStart(t, f)
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	changed := proto.Clone(original).(*v1.Operation)
	changed.Execution.Attempt.Capabilities.ProtocolVersion = "lerna-simulator-v2"
	b, e := proto.Marshal(changed)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.StorageFaultSQL("UPDATE operations SET record=X'" + hex.EncodeToString(b) + "' WHERE user_id='u' AND domain_id='d/ledger' AND id='" + a.OperationId.LocalId + "'"); e != nil {
		t.Fatal(e)
	}
	h, e := assembly.Open(f.path, "u", "d")
	if h != nil {
		if closeErr := h.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	if e == nil {
		t.Error("restart accepted original future protocol")
	}
	after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(after, changed) || f.calls.Load() != 0 {
		t.Fatalf("restart rewrote or sent original responsibility: %v %v calls=%d", after, e, f.calls.Load())
	}
}

// 规则：G1、G3、G11、R3、V4
func TestUnknownMetricAgeMissingAndBackclockRemainUnknown(t *testing.T) {
	for _, mode := range []string{"missing", "backclock"} {
		t.Run(mode, func(t *testing.T) {
			target := simulator.New("opaque")
			target.SetBehavior("accept-and-delay")
			f := newFixtureWithTarget(t, 100, 80, false, target)
			a, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || op.Effect.Outcome != "UNKNOWN" {
				t.Fatalf("actual unknown: %v %v", op, e)
			}
			changed := proto.Clone(op).(*v1.Operation)
			changed.Execution.Attempt.FirstPossibleSendAtUnixMs = 0
			if mode == "backclock" {
				changed.Execution.Attempt.FirstPossibleSendAtUnixMs = time.Now().Add(time.Hour).UnixMilli()
			}
			b, e := proto.Marshal(changed)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.h.StorageFaultSQL("UPDATE operations SET record=X'" + hex.EncodeToString(b) + "' WHERE user_id='u' AND domain_id='d/ledger' AND id='" + a.OperationId.LocalId + "'"); e != nil {
				t.Fatal(e)
			}
			metrics, e := f.h.Ledger.QueryMetrics(f.ctx, f.caller)
			if e != nil || metrics.Source.Availability != "PARTIAL" || metrics.Unknown.Total != 1 || metrics.Unknown.AgeSamples != 0 || metrics.Unknown.OldestAgeMs != nil || len(metrics.Unknown.AgeBuckets) != 0 || mode == "missing" && metrics.Unknown.AgeMissing != 1 || mode == "backclock" && metrics.Unknown.AgeClockInvalid != 1 {
				t.Fatalf("missing time fabricated zero age: %v %v", metrics, e)
			}
			local, e := f.h.QueryMetrics(f.ctx, f.caller)
			if e != nil {
				t.Fatal(e)
			}
			encoded, e := protojson.Marshal(local)
			if e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(strings.Join(readMetricGuidance(t, encoded), "\n"), "missing or invalid age") {
				t.Error("actual age blind spot lacks local investigation guidance")
			}
			after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			requests, effects := target.Snapshot()
			if e != nil || !proto.Equal(after, changed) || len(requests) != 1 || len(effects) != 0 {
				t.Fatalf("metric changed responsibility or target: %v %v requests=%v effects=%v", after, e, requests, effects)
			}
		})
	}
}

// 规则：G1、G3、G4、G11、开始-1、V4
func TestHeldExecutionClaimRefusesFutureCurrentJobBeforeP4(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, start := prepareStart(t, f)
	original, e := f.h.LedgerWork.QueryJob(f.ctx, f.caller, start.Claim.Ref.Name)
	if e != nil {
		t.Fatal(e)
	}
	changed := proto.Clone(original).(*v1.Job)
	changed.ContractVersion = 2
	b, e := proto.Marshal(changed)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.StorageFaultSQL("UPDATE ledger_jobs SET record=X'" + hex.EncodeToString(b) + "' WHERE user_id='u' AND domain_id='d/ledger' AND id='" + original.Ref.Name.LocalId + "'"); e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Tasks.StartExecution(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	if e != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED || r.GetError().GetCode() != "UNSUPPORTED_CONTRACT" {
		t.Fatalf("held job started: %v %v", r, e)
	}
	assertUnstarted(t, f, a, start)
}

// 规则：G1、G3、G4、G11、R7、V4
func TestStartupRefusesOriginalUnsupportedModelSettingsBeforeOtherOwnersRecover(t *testing.T) {
	for _, field := range []string{"provider", "encoder", "policy"} {
		t.Run(field, func(t *testing.T) {
			f := modelFixture(t)
			call := compatibilityModelCall(t, f)
			changed := proto.Clone(call).(*v1.ModelCall)
			switch field {
			case "provider":
				changed.Settings.Provider = "future-provider"
			case "encoder":
				changed.Settings.EncoderVersion = "reference-model-v2"
			case "policy":
				changed.Settings.PolicyVersion = "m1-v2"
			}
			b, e := proto.Marshal(changed)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.h.StorageFaultSQL("UPDATE model_calls SET record=X'" + hex.EncodeToString(b) + "' WHERE user_id='u' AND domain_id='d' AND id='" + call.Ref.Name.LocalId + "'"); e != nil {
				t.Fatal(e)
			}
			goal := &v1.SubmitGoalCommand{Identity: header("compatibility-original-goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "preserve pending original responsibility"}
			original, e := f.h.Sessions.SubmitGoal(f.ctx, f.caller, goal)
			if e != nil {
				t.Fatal(e)
			}
			h, e := assembly.Open(f.path, "u", "d")
			if h != nil {
				if closeErr := h.Close(); closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			if e == nil {
				t.Error("startup accepted unsupported original model implementation")
			}
			after, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, goal.Identity)
			if e != nil || !proto.Equal(after.Receipt, original) {
				t.Fatalf("startup advanced another original owner: %v %v", after, e)
			}
			job, e := f.h.Durable.QueryJob(f.ctx, f.caller, original.JobRef.Name)
			if e != nil || job.State != "READY" || job.ClaimEpoch != 0 || f.calls.Load() != 0 {
				t.Fatalf("startup claimed or sent: %v %v target=%d", job, e, f.calls.Load())
			}
		})
	}
}

func compatibilityModelCall(t *testing.T, f *fixture) *v1.ModelCall {
	t.Helper()
	snapshot, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("compatibility-model-request"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	claim, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("compatibility-model-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"PROPOSE"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "compatibility-model-worker"})
	accepted(t, claim, e)
	call, e := f.h.Tasks.PrepareModelCall(f.ctx, f.caller, &v1.PrepareModelCallCommand{Header: header("compatibility-model-prepare"), RequestRef: snapshot.RequestRef, Claim: claim.Jobs[0], CapabilityRef: f.capability, Settings: &v1.ModelSettings{Provider: "reference", Model: "model-v1", EncoderVersion: "reference-model-v1", PolicyVersion: "m1-v1", ParametersJson: []byte(`{}`), ToolSchemasJson: []byte(`[]`), MaxOutputTokens: 100}, InputRefs: snapshot.ContentRefs})
	if e != nil {
		t.Fatal(e)
	}
	return call
}

// 规则：G1、G2、G3、R3、R6、V4
func TestMetricCollectionFailureRetainsOtherActualOwners(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("metric-partial-original"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	before, e := f.h.QueryMetrics(f.ctx, f.caller)
	if e != nil || before.Trace == nil || before.Ledger == nil || before.Admission == nil {
		t.Fatalf("initial owner collection: %v %v", before, e)
	}
	// 只破坏可重建索引的读取来源，准入和执行管理仍使用实际原负责方。
	if e = f.h.StorageFaultSQL("DROP TABLE trace_indexes"); e != nil {
		t.Fatal(e)
	}
	partial, e := f.h.QueryMetrics(f.ctx, f.caller)
	if e != nil || partial.Trace != nil || partial.Ledger == nil || partial.Admission.GetSamples() != 1 || len(partial.UnavailableSources) != 1 || partial.UnavailableSources[0].OwnerDomainId != "d/trace" || partial.UnavailableSources[0].Availability != "MISSING" || partial.UnavailableSources[0].ErrorCode != "DEPENDENCY_UNAVAILABLE" || partial.Ledger.Source.GetSourceRevision() != before.Ledger.Source.GetSourceRevision() || partial.CrossDomainAtomic || f.calls.Load() != 0 {
		t.Fatalf("partial source fabricated healthy zero or erased other owner: %v %v", partial, e)
	}
}
