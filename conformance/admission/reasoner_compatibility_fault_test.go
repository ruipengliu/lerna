//go:build fault

package admission_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G10、G11、R7、V4
func TestStartupRejectsSavedDriverHistoryBeforeAnyOwnerRecovery(t *testing.T) {
	for _, test := range []struct{ table, field string }{
		{"reasoner_drivers", "contract"},
		{"reasoner_driver_versions", "contract"},
		{"reasoner_driver_versions", "implementation"},
		{"reasoner_drivers", "provider"}, {"reasoner_driver_versions", "provider"},
		{"reasoner_drivers", "implementation"}, {"reasoner_driver_versions", "encoder"},
		{"reasoner_drivers", "settings-policy"}, {"reasoner_driver_versions", "view-policy"},
		{"reasoner_drivers", "policy-unknown"}, {"reasoner_driver_versions", "settings-unknown"},
		{"reasoner_driver_versions", "action-unknown"}, {"reasoner_driver_versions", "capability-ref-unknown"},
		{"reasoner_drivers", "identity-unknown"}, {"reasoner_driver_versions", "session-unknown"},
	} {
		t.Run(test.table+"/"+test.field, func(t *testing.T) {
			f, provider, target, first, current := disabledCompatibilityDriver(t)
			original := first
			if test.table == "reasoner_drivers" {
				original = current
			}
			changed := faultSavedDriver(t, f, test.table, test.field, original)
			if test.table == "reasoner_drivers" {
				current = changed
			} else {
				first = changed
			}
			pending := &v1.SubmitGoalCommand{Identity: header("compatibility-unrelated-ready").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "keep another original owner READY"}
			receipt, e := f.h.Sessions.SubmitGoal(f.ctx, f.caller, pending)
			if e != nil {
				t.Fatal(e)
			}
			job, e := f.h.Durable.QueryJob(f.ctx, f.caller, receipt.JobRef.Name)
			if e != nil || job.State != "READY" || job.ClaimEpoch != 0 || job.Attempts != 0 {
				t.Fatalf("actual unrelated READY responsibility: %v %v", job, e)
			}
			originalFacts := disabledDriverFacts(t, f)
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil {
				t.Fatal(e)
			}
			sources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
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
				t.Error("unsupported original driver history accepted at startup")
			}
			after, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, pending.Identity)
			if e != nil || !proto.Equal(after.GetReceipt(), receipt) {
				t.Errorf("another owner advanced before compatibility refusal: %v %v", after, e)
			}
			afterJob, e := f.h.Durable.QueryJob(f.ctx, f.caller, job.Ref.Name)
			if e != nil || !proto.Equal(afterJob, job) {
				t.Errorf("unrelated READY epoch/attempt changed: %v %v", afterJob, e)
			}
			assertCompatibilityFacts(t, originalFacts, disabledDriverFacts(t, f))
			assertCompatibilityDriver(t, f, first, current)
			budgetAfter, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || !proto.Equal(budgetAfter, budget) {
				t.Errorf("original budget changed: %v %v", budgetAfter, e)
			}
			sourcesAfter, e := f.h.Trace.QuerySources(f.ctx, f.caller)
			if e != nil || len(sourcesAfter) != len(sources) {
				t.Errorf("original source population advanced: %v", e)
			} else {
				for i := range sources {
					if !proto.Equal(sourcesAfter[i], sources[i]) {
						t.Errorf("original source/ACK changed: %v", sourcesAfter[i])
					}
				}
			}
			requests, effects := target.Target.Snapshot()
			if provider.Calls() != 0 || len(provider.Bills()) != 0 || f.calls.Load() != 0 || len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
				t.Fatal("qualification created external call/effect/bill")
			}
		})
	}
}

// 规则：G1、G3、G4、G10、G11、R7、V4
func TestStartupPreservesCompatibleDisabledDriverHistoryAndRawSettings(t *testing.T) {
	f, provider, target, first, current := disabledCompatibilityDriver(t)
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, current.Policy.ModelGrantRef)
	if e != nil {
		t.Fatal(e)
	}
	if until := time.Until(time.UnixMilli(grant.ValidUntilUnixMs)); until > 0 {
		time.Sleep(until + time.Millisecond)
	}
	if current.Policy.Settings.ViewPolicy != "" || string(current.Policy.Settings.ParametersJson) != " { \"temperature\" : 0.2, \"seed\" : 7 } " || string(current.Policy.Settings.ToolSchemasJson) != " [ ] " {
		t.Fatal("fixture lost actual originally supported noncanonical settings")
	}
	originalFacts := disabledDriverFacts(t, f)
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil {
		t.Fatal(e)
	}
	sources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Tasks.CheckStartupCompatibility(f.ctx); e != nil {
		t.Fatal("compatible history was treated as current grant eligibility", e)
	}
	assertCompatibilityDriver(t, f, first, current)
	afterSources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil || len(afterSources) != len(sources) {
		t.Fatal("pure compatibility advanced source positions", e)
	}
	for i := range sources {
		if !proto.Equal(afterSources[i], sources[i]) {
			t.Fatal("pure compatibility rewrote original source/ACK")
		}
	}
	pending := &v1.SubmitGoalCommand{Identity: header("compatible-unrelated-ready").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "normal recovery remains legal"}
	if _, e = f.h.Sessions.SubmitGoal(f.ctx, f.caller, pending); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal("supported original settings/history refused", e)
	}
	assertCompatibilityDriver(t, f, first, current)
	assertCompatibilityFacts(t, originalFacts, disabledDriverFacts(t, f))
	q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, pending.Identity)
	if e != nil || q.GetReceipt().GetDecision() != v1.Decision_DECISION_ACCEPTED || q.GetReceipt().GetTaskRef() == nil {
		t.Fatalf("qualified ordinary owner recovery blocked: %v %v", q, e)
	}
	afterBudget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || !proto.Equal(afterBudget, budget) {
		t.Fatalf("compatibility changed original budget: %v %v", afterBudget, e)
	}
	requests, effects := target.Target.Snapshot()
	if provider.Calls() != 0 || len(provider.Bills()) != 0 || len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
		t.Fatal("disabled compatibility history sampled/sent")
	}
	t.Log("current_and_immutable_history=SUPPORTED raw_settings=UNCHANGED ViewPolicy=EMPTY original_model_name=RETAINED old_grant=EXPIRED model_calls=0 model_bills=0 target_calls=0 effects=0 target_bills=0 unrelated_READY=LEGALLY_RECOVERED")
}

// disabledCompatibilityDriver 经公开配置替换保存真实两版，原合法设置保持非规范化字节。
func disabledCompatibilityDriver(t *testing.T) (*fixture, *simulator.ModelProvider, *simulator.BillingTarget, *v1.ReasonerDriver, *v1.ReasonerDriver) {
	t.Helper()
	f, provider, target, policy := cancellationDriverPublicFixture(t)
	policy.Settings.ParametersJson = []byte(" { \"temperature\" : 0.2, \"seed\" : 7 } ")
	policy.Settings.ToolSchemasJson = []byte(" [ ] ")
	policy.Settings.Model = "original-model-name"
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	policy.SessionId = goal.Receipt.SessionRef.Name
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, policy.ModelGrantRef)
	if e != nil {
		t.Fatal(e)
	}
	grant.Ref, grant.Issuer, grant.Status = nil, nil, ""
	grant.UsePoolId = "compatibility-history"
	grant.ValidUntilUnixMs = time.Now().Add(150 * time.Millisecond).UnixMilli()
	short, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("compatibility-short-model-grant"), Grant: grant})
	accepted(t, short, e)
	policy.ModelGrantRef = short.ResultRef
	r, e := f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, &v1.ConfigureReasonerDriverCommand{Header: header("compatibility-configure-first"), TaskId: f.task.Name, Policy: policy, Disabled: true})
	accepted(t, r, e)
	first, e := f.h.Tasks.QueryReasonerDriverVersion(f.ctx, f.caller, r.ResultRef)
	if e != nil || first.State != "STOPPED" || first.Enabled {
		t.Fatalf("first actual disabled configuration: %v %v", first, e)
	}
	nextPolicy := proto.Clone(policy).(*v1.ReasonerDriverPolicy)
	nextPolicy.Actions = nil
	r, e = f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, &v1.ConfigureReasonerDriverCommand{Header: header("compatibility-configure-second"), TaskId: f.task.Name, Policy: nextPolicy, Replaces: first.Ref, Disabled: true})
	accepted(t, r, e)
	current, e := f.h.Tasks.QueryReasonerDriver(f.ctx, f.caller, f.task.Name)
	if e != nil || current.Ref.Revision != first.Ref.Revision+1 || current.Enabled || current.State != "STOPPED" || current.Policy.Settings.ViewPolicy != "" || !proto.Equal(current.Policy.Settings, policy.Settings) {
		t.Fatalf("actual supported newer disabled configuration: %v %v", current, e)
	}
	return f, provider, target, first, current
}

// faultSavedDriver 只修改真实保存消息的格式元数据，反向恢复证明原业务字段未改变。
func faultSavedDriver(t *testing.T, f *fixture, table, field string, original *v1.ReasonerDriver) *v1.ReasonerDriver {
	t.Helper()
	changed := proto.Clone(original).(*v1.ReasonerDriver)
	mutateDriverMetadata(changed, field, false)
	back := proto.Clone(changed).(*v1.ReasonerDriver)
	mutateDriverMetadata(back, field, true)
	if !proto.Equal(back, original) {
		t.Fatal("compatibility metadata fault altered business fields")
	}
	body, e := proto.Marshal(changed)
	if e != nil {
		t.Fatal(e)
	}
	key := "task_id='" + original.TaskId.LocalId + "'"
	if table == "reasoner_driver_versions" {
		key = fmt.Sprintf("id='%s' AND revision=%d", original.Ref.Name.LocalId, original.Ref.Revision)
	}
	if e = f.h.StorageFaultSQL("UPDATE " + table + " SET record=X'" + hex.EncodeToString(body) + "' WHERE user_id='u' AND domain_id='d' AND " + key); e != nil {
		t.Fatal(e)
	}
	var saved *v1.ReasonerDriver
	if table == "reasoner_driver_versions" {
		saved, e = f.h.Tasks.QueryReasonerDriverVersion(f.ctx, f.caller, original.Ref)
	} else {
		saved, e = f.h.Tasks.QueryReasonerDriver(f.ctx, f.caller, original.TaskId)
	}
	if e != nil || !proto.Equal(saved, changed) {
		t.Fatalf("actual saved metadata mutation missing: %v %v", saved, e)
	}
	return changed
}

func mutateDriverMetadata(d *v1.ReasonerDriver, field string, inverse bool) {
	switch field {
	case "provider":
		d.Policy.Settings.Provider = "future-provider"
		if inverse {
			d.Policy.Settings.Provider = "reference"
		}
	case "encoder":
		d.Policy.Settings.EncoderVersion = "reference-model-v2"
		if inverse {
			d.Policy.Settings.EncoderVersion = "reference-model-v1"
		}
	case "settings-policy":
		d.Policy.Settings.PolicyVersion = "m1-v2"
		if inverse {
			d.Policy.Settings.PolicyVersion = "m1-v1"
		}
	case "view-policy":
		d.Policy.Settings.ViewPolicy = "m1-default-v2"
		if inverse {
			d.Policy.Settings.ViewPolicy = ""
		}
	case "contract":
		d.ContractVersion = 2
		if inverse {
			d.ContractVersion = 1
		}
	case "implementation":
		d.ImplementationVersion = "default-v2"
		if inverse {
			d.ImplementationVersion = "default-v1"
		}
	default:
		var m proto.Message
		switch field {
		case "policy-unknown":
			m = d.Policy
		case "settings-unknown":
			m = d.Policy.Settings
		case "action-unknown":
			m = d.Policy.Actions[0]
		case "capability-ref-unknown":
			m = d.Policy.ModelCapabilityRef
		case "identity-unknown":
			m = d.ConfiguredBy
		case "session-unknown":
			m = d.Policy.SessionId
		}
		b := protowire.AppendVarint(protowire.AppendTag(nil, 19001, protowire.VarintType), 1)
		if inverse {
			b = nil
		}
		m.ProtoReflect().SetUnknown(b)
	}
}

func assertCompatibilityDriver(t *testing.T, f *fixture, first, current *v1.ReasonerDriver) {
	t.Helper()
	old, e := f.h.Tasks.QueryReasonerDriverVersion(f.ctx, f.caller, first.Ref)
	if e != nil || !proto.Equal(old, first) {
		t.Errorf("original superseded driver history changed: %v %v", old, e)
	}
	latest, e := f.h.Tasks.QueryReasonerDriver(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(latest, current) {
		t.Errorf("current disabled driver changed: %v %v", latest, e)
	}
}

// 规则：G1、G3、G4、G5、G10、G11、R7、V4
func TestStartupRejectsCompletedDriverOriginalOutcomeClaimBeforeAnyOwnerRecovery(t *testing.T) {
	f, provider, target, driver := completedCompatibilityDriver(t)
	outcome, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, driver.OutcomeRef)
	if e != nil || outcome.Claim == nil || outcome.Claim.ContractVersion != 1 || outcome.Claim.Responsibility == nil || outcome.Claim.Ref == nil {
		t.Fatalf("actual original outcome claim missing: %v %v", outcome, e)
	}
	changed := proto.Clone(outcome).(*v1.ProposalOutcome)
	changed.Claim.ContractVersion = 2
	back := proto.Clone(changed).(*v1.ProposalOutcome)
	back.Claim.ContractVersion = 1
	if !proto.Equal(back, outcome) {
		t.Fatal("metadata fault changed original outcome business facts")
	}
	body, e := proto.Marshal(changed)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.StorageFaultSQL("UPDATE proposal_outcomes SET record=X'" + hex.EncodeToString(body) + "' WHERE user_id='u' AND domain_id='d' AND id='" + outcome.Ref.Name.LocalId + "'"); e != nil {
		t.Fatal(e)
	}
	actual, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, driver.OutcomeRef)
	if e != nil || !proto.Equal(actual, changed) {
		t.Fatalf("actual saved original claim fault missing: %v %v", actual, e)
	}
	assertCompletedHistoryRefused(t, f, provider, target, driver)
}

func assertCompletedHistoryRefused(t *testing.T, f *fixture, provider *simulator.ModelProvider, target *simulator.BillingTarget, driver *v1.ReasonerDriver) {
	t.Helper()
	pending := &v1.SubmitGoalCommand{Identity: header("completed-compatibility-unrelated-ready").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "keep unrelated owner at its original READY frontier"}
	receipt, e := f.h.Sessions.SubmitGoal(f.ctx, f.caller, pending)
	if e != nil {
		t.Fatal(e)
	}
	job, e := f.h.Durable.QueryJob(f.ctx, f.caller, receipt.JobRef.Name)
	if e != nil || job.State != "READY" || job.ClaimEpoch != 0 || job.Attempts != 0 {
		t.Fatalf("unrelated original frontier: %v %v", job, e)
	}
	before := completedDriverFacts(t, f, driver)
	h, openErr := assembly.Open(f.path, "u", "d")
	if h != nil {
		if e = h.Close(); e != nil {
			t.Fatal(e)
		}
	}
	if openErr == nil {
		t.Error("unsupported original outcome claim accepted at startup")
	}
	after, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, pending.Identity)
	if e != nil || !proto.Equal(after.GetReceipt(), receipt) {
		t.Errorf("unrelated owner advanced before refusal: %v %v", after, e)
	}
	afterJob, e := f.h.Durable.QueryJob(f.ctx, f.caller, job.Ref.Name)
	if e != nil || !proto.Equal(afterJob, job) {
		t.Errorf("unrelated READY epoch/attempt changed: %v %v", afterJob, e)
	}
	assertCompatibilityFacts(t, before, completedDriverFacts(t, f, driver))
	assertCompletedCompatibilityCounters(t, f, provider, target)
}

// 规则：G1、G3、G4、G5、G10、G11、R7、V4
func TestStartupRejectsCompletedDriverNestedOriginalHistoryBeforeAnyOwnerRecovery(t *testing.T) {
	for _, field := range []string{"historical-driver-settings", "original-request", "original-outcome-claim", "original-outcome-proposal"} {
		t.Run(field, func(t *testing.T) {
			f, provider, target, driver := completedCompatibilityDriver(t)
			unknown := protowire.AppendVarint(protowire.AppendTag(nil, 19001, protowire.VarintType), 1)
			switch field {
			case "historical-driver-settings":
				ref := proto.Clone(driver.Ref).(*v1.Ref)
				ref.Revision = 1
				version, e := f.h.Tasks.QueryReasonerDriverVersion(f.ctx, f.caller, ref)
				if e != nil {
					t.Fatal(e)
				}
				faultSavedDriver(t, f, "reasoner_driver_versions", "settings-unknown", version)
			case "original-request":
				original, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, driver.RequestRef)
				if e != nil {
					t.Fatal(e)
				}
				changed := proto.Clone(original).(*v1.ProposalRequest)
				changed.ProtoReflect().SetUnknown(unknown)
				back := proto.Clone(changed).(*v1.ProposalRequest)
				back.ProtoReflect().SetUnknown(nil)
				if !proto.Equal(back, original) {
					t.Fatal("request metadata fault changed business facts")
				}
				// 查询回报回执是负责方动态投影；仅在原保存字节末尾追加格式元数据，绝不把投影写回。
				if e = f.h.StorageFaultSQL("UPDATE proposal_requests SET record=CAST(record || X'" + hex.EncodeToString(unknown) + "' AS BLOB) WHERE user_id='u' AND domain_id='d' AND id='" + original.Ref.Name.LocalId + "'"); e != nil {
					t.Fatal(e)
				}
				actual, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, driver.RequestRef)
				if e != nil || !proto.Equal(actual, changed) {
					t.Fatalf("actual request fault changed more than metadata: %v %v", actual, e)
				}
			default:
				original, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, driver.OutcomeRef)
				if e != nil {
					t.Fatal(e)
				}
				changed := proto.Clone(original).(*v1.ProposalOutcome)
				var m proto.Message = changed.Claim.Responsibility
				if field == "original-outcome-proposal" {
					m = changed.Proposal
				}
				m.ProtoReflect().SetUnknown(unknown)
				back := proto.Clone(changed).(*v1.ProposalOutcome)
				if field == "original-outcome-proposal" {
					back.Proposal.ProtoReflect().SetUnknown(nil)
				} else {
					back.Claim.Responsibility.ProtoReflect().SetUnknown(nil)
				}
				if !proto.Equal(back, original) {
					t.Fatal("outcome metadata fault changed business facts")
				}
				body, e := proto.Marshal(changed)
				if e != nil {
					t.Fatal(e)
				}
				if e = f.h.StorageFaultSQL("UPDATE proposal_outcomes SET record=X'" + hex.EncodeToString(body) + "' WHERE user_id='u' AND domain_id='d' AND id='" + original.Ref.Name.LocalId + "'"); e != nil {
					t.Fatal(e)
				}
				actual, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, driver.OutcomeRef)
				if e != nil || !proto.Equal(actual, changed) {
					t.Fatalf("actual saved outcome fault missing: %v %v", actual, e)
				}
			}
			assertCompletedHistoryRefused(t, f, provider, target, driver)
		})
	}
}

// 规则：G1、G3、G4、G5、G10、G11、R7、V4
func TestStartupPreservesCompletedDriverOriginalModelAndExpiredOutcomeClaim(t *testing.T) {
	f, provider, target, driver := completedCompatibilityDriver(t)
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, driver.RequestRef, 0)
	if e != nil || call.Settings.ViewPolicy != "m1-default-v1" || driver.Policy.Settings.ViewPolicy != "" || proto.Equal(call.Settings, driver.Policy.Settings) {
		t.Fatalf("original raw driver settings and separately normalized prepared ModelCall collapsed: %v %v", call, e)
	}
	// 先走一次原生产启动，让原 Trace 交接完成，随后观察真正静止的历史。
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal("compatible completed history refused", e)
	}
	outcome, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, driver.OutcomeRef)
	if e != nil || outcome.Claim.LeaseUntilUnixMs == 0 {
		t.Fatalf("original claim lease missing: %v %v", outcome, e)
	}
	before := completedDriverFacts(t, f, driver)
	if until := time.Until(time.UnixMilli(outcome.Claim.LeaseUntilUnixMs)); until > 0 {
		time.Sleep(until + time.Millisecond)
	}
	if time.Now().UnixMilli() < outcome.Claim.LeaseUntilUnixMs {
		t.Fatal("original historical claim has not expired")
	}
	if e = f.h.Tasks.CheckStartupCompatibility(f.ctx); e != nil {
		t.Fatal("historical expired claim treated as current advancement eligibility", e)
	}
	assertCompatibilityFacts(t, before, completedDriverFacts(t, f, driver))
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal("supported original history after lease expiry refused", e)
	}
	assertCompatibilityFacts(t, before, completedDriverFacts(t, f, driver))
	assertCompletedCompatibilityCounters(t, f, provider, target)
	t.Logf("original_outcome_claim=EXPIRED original_lease_until=%d current_and_all_immutable_versions=UNCHANGED raw_driver_settings=UNCHANGED prepared_ModelCall_settings=SEPARATE_NORMALIZED fixed_CANCELLED_Result=BYTE_IDENTICAL no_new_model_position=true", outcome.Claim.LeaseUntilUnixMs)
}

// completedCompatibilityDriver 只经原公开驱动、交接、取消和受信关闭固定两项历史。
func completedCompatibilityDriver(t *testing.T) (*fixture, *simulator.ModelProvider, *simulator.BillingTarget, *v1.ReasonerDriver) {
	t.Helper()
	f, provider, target, policy := cancellationDriverPublicFixture(t)
	policy.Settings.ParametersJson = []byte(" { \"seed\" : 7, \"temperature\" : 0.2 } ")
	policy.Settings.ToolSchemasJson = []byte(" [ ] ")
	policy.Settings.Model = "original-completed-model-name"
	r, e := f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, &v1.ConfigureReasonerDriverCommand{Header: header("compatibility-model-driver"), TaskId: f.task.Name, Policy: policy})
	accepted(t, r, e)
	var driver *v1.ReasonerDriver
	for range 3 {
		driver, e = f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 1})
		if e != nil {
			t.Fatal(e)
		}
	}
	if driver.AdmissionRef == nil || driver.OutcomeRef == nil || driver.RequestRef == nil {
		t.Fatalf("original driver frontier missing: %v", driver)
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	cancelTask(t, f, "compatibility-model-cancel")
	if e = f.h.Tasks.ProcessCancellations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Budget.ProcessClosures(f.ctx); e != nil {
		t.Fatal(e)
	}
	beginCancelledClose(t, f, "compatibility-model-close")
	if e = f.h.Tasks.ProcessTaskClosings(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Budget.ProcessSettlementFollowups(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	driver, e = f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 1})
	if e != nil || driver.State != "COMPLETED" {
		t.Fatalf("actual completed driver: %v %v", driver, e)
	}
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || view.Result == nil || view.Result.Outcome != "CANCELLED" || len(view.Result.OperationRefs) != 2 || len(view.ClosureSeals) != 2 || len(view.PendingClosureRefs) != 0 {
		t.Fatalf("actual fixed CANCELLED two-operation history: %v %v", view, e)
	}
	assertCompletedCompatibilityCounters(t, f, provider, target)
	return f, provider, target, driver
}

func assertCompletedCompatibilityCounters(t *testing.T, f *fixture, provider *simulator.ModelProvider, target *simulator.BillingTarget) {
	t.Helper()
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	requests, effects := target.Target.Snapshot()
	if e != nil || budget.Reserved != 0 || budget.Settled != 7 || provider.Calls() != 1 || len(provider.Bills()) != 1 || f.calls.Load() != 0 || len(requests) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
		t.Fatalf("original MODEL1/bill1/fee7 or unsent target0 changed: %v %v", budget, e)
	}
	t.Log("actual_original_model_calls=1 model_bills=1 settled_fee=7 reserved=0 original_target_POST=0 effects=0 target_bills=0 fixed_Result=CANCELLED")
}

// disabledDriverFacts 保留原配置回执、原任务/请求及未采样的模型位置。
func disabledDriverFacts(t *testing.T, f *fixture) map[string]proto.Message {
	t.Helper()
	facts := make(map[string]proto.Message)
	for _, id := range []string{"goal", "compatibility-configure-first", "compatibility-configure-second"} {
		q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header(id).Identity)
		if e != nil || q.GetReceipt() == nil {
			t.Fatalf("original receipt %s: %v %v", id, q, e)
		}
		facts["receipt/"+id] = q
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	facts["task"] = task
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	facts["planning"] = planning
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, planning.Snapshot.RequestRef)
	if e != nil {
		t.Fatal(e)
	}
	facts["request"] = request
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, request.Ref, 0)
	if e != nil || call != nil {
		t.Fatalf("disabled history sampled original model position: %v %v", call, e)
	}
	return facts
}

// completedDriverFacts 均从原负责方查询，包含当前/全部不可变版本、原凭据、发送事实和固定 Result 字节。
func completedDriverFacts(t *testing.T, f *fixture, original *v1.ReasonerDriver) map[string]proto.Message {
	t.Helper()
	facts := make(map[string]proto.Message)
	driver, e := f.h.Tasks.QueryReasonerDriver(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	facts["driver"] = driver
	for revision := uint64(1); revision <= driver.Ref.Revision; revision++ {
		ref := proto.Clone(driver.Ref).(*v1.Ref)
		ref.Revision = revision
		version, e := f.h.Tasks.QueryReasonerDriverVersion(f.ctx, f.caller, ref)
		if e != nil || version == nil {
			t.Fatalf("actual immutable driver revision %d: %v %v", revision, version, e)
		}
		facts[fmt.Sprintf("driver-version/%d", revision)] = version
	}
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	facts["closing-view"] = view
	result, e := proto.Marshal(view.Result)
	if e != nil || len(result) == 0 {
		t.Fatal("fixed original Result missing", e)
	}
	facts["fixed-result"] = view.Result
	cancellation, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	facts["cancellation"] = cancellation
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	facts["planning"] = planning
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, original.RequestRef)
	if e != nil {
		t.Fatal(e)
	}
	facts["request"] = request
	outcome, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, original.OutcomeRef)
	if e != nil {
		t.Fatal(e)
	}
	facts["outcome"] = outcome
	claimJob, e := f.h.Durable.QueryJob(f.ctx, f.caller, outcome.Claim.Ref.Name)
	if e != nil {
		t.Fatal(e)
	}
	facts["original-claim-job"] = claimJob
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, original.RequestRef, 0)
	if e != nil || call == nil || call.Result.GetStatus() != "COMPLETED" {
		t.Fatalf("original sealed model call: %v %v", call, e)
	}
	facts["model-call"] = call
	if next, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, original.RequestRef, 1); e != nil || next != nil {
		t.Fatalf("extra model position: %v %v", next, e)
	}
	input, e := f.h.Content.Read(f.ctx, f.caller, call.InputRef)
	if e != nil {
		t.Fatal(e)
	}
	facts["model-input"] = input
	output, e := f.h.Content.Read(f.ctx, f.caller, call.Result.OutputRef)
	if e != nil {
		t.Fatal(e)
	}
	facts["model-output"] = output
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil {
		t.Fatal(e)
	}
	facts["budget"] = budget
	sources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	for i, source := range sources {
		facts[fmt.Sprintf("source/%d", i)] = source
	}
	for _, id := range []*v1.CommandIdentity{header("goal").Identity, header("compatibility-model-driver").Identity, header("compatibility-model-cancel").Identity, header("compatibility-model-close").Identity, outcome.Identity} {
		caller := proto.Clone(f.caller).(*v1.Caller)
		caller.IssuerId = id.IssuerId
		receipt, e := f.h.Durable.QueryReceipt(f.ctx, caller, id)
		if e != nil || receipt.GetReceipt() == nil {
			t.Fatalf("original receipt: %v %v", receipt, e)
		}
		facts["receipt/"+id.CommandId] = receipt
	}
	for _, op := range view.Operations {
		if op.Execution != nil {
			source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, op.Execution.Send.Ref)
			if e != nil {
				t.Fatal(e)
			}
			facts["billing-source/"+op.Ref.Name.LocalId] = source
		}
	}
	return facts
}

func assertCompatibilityFacts(t *testing.T, before, after map[string]proto.Message) {
	t.Helper()
	if len(before) != len(after) {
		t.Errorf("original owner population changed: %d -> %d", len(before), len(after))
	}
	for key, original := range before {
		current := after[key]
		if !proto.Equal(current, original) {
			t.Errorf("original owner fact changed: %s", key)
		}
		if key == "fixed-result" {
			a, ea := proto.Marshal(original)
			b, eb := proto.Marshal(current)
			if ea != nil || eb != nil || !bytes.Equal(a, b) {
				t.Error("original fixed Result bytes changed")
			}
		}
	}
}
