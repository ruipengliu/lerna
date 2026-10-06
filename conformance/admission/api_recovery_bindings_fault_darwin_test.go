//go:build fault && darwin && cgo

package admission_test

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G7、G10、G11、R7、V4
func TestStartupRefusesOriginalAPIAttemptIdentityBeforeAnyOtherOwnerRecovery(t *testing.T) {
	f, options, original, counters := nativeCompatibilityHistory(t, "registered")
	changed := proto.Clone(original).(*v1.Operation)
	changed.Execution.Attempt.Ref.Name.UserId = "foreign-user"
	changed.Execution.Send.AttemptId.UserId = "foreign-user"
	back := proto.Clone(changed).(*v1.Operation)
	back.Execution.Attempt.Ref.Name.UserId = "u"
	back.Execution.Send.AttemptId.UserId = "u"
	if !proto.Equal(back, original) {
		t.Fatal("compatibility metadata fault changed business facts")
	}
	faultOriginalAPIRecord(t, f, changed)
	assertOriginalAPIRefusedBeforeOwners(t, f, options, changed, counters)
}

// 规则：G1、G3、G4、G7、G10、G11、R7、V4
func TestStartupRejectsOriginalAPIProfilesBindingsAndNestedHistoryBeforeOwners(t *testing.T) {
	for _, test := range []struct{ kind, field string }{
		{"no-execution", "adapter-domain"}, {"no-execution", "api-version"},
		{"registered", "serialization"}, {"registered", "declaration-concurrency"}, {"registered", "account-scope"},
		{"registered", "target-binding"}, {"registered", "credential-version"}, {"registered", "expiry"},
		{"registered", "attempt-operation"}, {"registered", "send-attempt"},
		{"closed", "credential-unknown"}, {"closed", "descriptor-account"},
		{"query", "query-subject"}, {"query", "query-declaration-binding"}, {"query", "subject-unknown"},
		{"previous-send", "previous-attempt"},
	} {
		t.Run(test.kind+"/"+test.field, func(t *testing.T) {
			f, options, original, counters := nativeCompatibilityHistory(t, test.kind)
			changed := proto.Clone(original).(*v1.Operation)
			mutateOriginalAPIMetadata(changed, test.field, false)
			back := proto.Clone(changed).(*v1.Operation)
			mutateOriginalAPIMetadata(back, test.field, true)
			if !proto.Equal(back, original) {
				t.Fatal("metadata inverse did not preserve exact original business history")
			}
			faultOriginalAPIRecord(t, f, changed)
			assertOriginalAPIRefusedBeforeOwners(t, f, options, changed, counters)
		})
	}
}

// 规则：G1、G3、G4、G7、G10、G11、R7、V4
func TestStartupPreservesOriginalAPIQueryResendAndExpiredHistories(t *testing.T) {
	for _, kind := range []string{"query", "previous-send", "expired", "non-idempotent"} {
		t.Run(kind, func(t *testing.T) {
			f, options, original, counters := nativeCompatibilityHistory(t, kind)
			before := counters()
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "query" {
				declaration := original.Execution.Attempt.Capabilities
				if declaration.Effect != "READ" || declaration.ProtocolVersion != "lerna-reference-api-query-v1" || declaration.Idempotent || declaration.Queryable || declaration.IdempotencyMechanism != "NATIVE_KEY" || declaration.ConcurrencyGuarantee != "SAME_KEY_ALL_SENDS" || declaration.ParameterBinding != "EXACT_REQUEST" || !declaration.RejectsExpiredKeys {
					t.Fatalf("actual compiled QUERY profile lost original attributes: %v", declaration)
				}
			}
			if kind == "previous-send" && len(original.Execution.PreviousSends) != 1 {
				t.Fatal("no actual original previous send")
			}
			if kind == "expired" {
				until := original.Execution.Attempt.KeyValidUntilUnixMs
				if until == nil {
					t.Fatal("actual original finite key missing")
				}
				if delay := time.Until(time.UnixMilli(*until)); delay > 0 {
					time.Sleep(delay + time.Millisecond)
				}
			}
			if e = f.h.Ledger.CheckStartupCompatibility(f.ctx); e != nil {
				t.Fatal("pure compatible old API history refused", e)
			}
			if e = f.h.Close(); e != nil {
				t.Fatal(e)
			}
			next, e := assembly.OpenWithOptions(f.path, "u", "d", options)
			if e != nil {
				t.Fatal("supported original API history refused", e)
			}
			f.h = next
			after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, original.Ref.Name)
			if e != nil || !proto.Equal(after, original) || fmt.Sprint(before) != fmt.Sprint(counters()) {
				t.Fatalf("startup rewrote or sent original API history: %v %v", after, e)
			}
			actualBudget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || !proto.Equal(actualBudget, budget) {
				t.Fatal("startup changed original per-send fees", e)
			}
			t.Logf("actual_kind=%s original_POST_GET_effect_bill=%v startup_new_requests=0 original_descriptor_attempt_key_expiry_previoussends_query_subject=UNCHANGED", kind, before)
		})
	}
}

// mutateOriginalAPIMetadata 只修改原已保存格式/绑定，摘要重新计算后仍由独立绑定约束拒绝。
func mutateOriginalAPIMetadata(op *v1.Operation, field string, inverse bool) {
	cap := op.CapabilitySnapshot
	x := op.Execution
	set := func(current *string, future, original string) {
		*current = future
		if inverse {
			*current = original
		}
	}
	switch field {
	case "adapter-domain":
		set(&cap.AdapterRef.Name.AuthorityDomainId, "future-adapter", "adapter")
	case "api-version":
		set(&cap.ApiDescriptor.Version, "2", "1")
	case "serialization":
		set(&cap.ApiDescriptor.Serialization, "reference-json-v2", "reference-json-v1")
	case "declaration-concurrency":
		set(&x.Attempt.Capabilities.ConcurrencyGuarantee, "DIFFERENT_KEYS_ALLOWED", "SAME_KEY_ALL_SENDS")
	case "account-scope":
		set(&x.Attempt.Capabilities.AccountScope, "foreign-user", "u")
	case "target-binding":
		set(&x.CallDescriptor.Target, cap.Resource+"/other-resource", cap.Resource)
	case "credential-version":
		cap.ApiDescriptor.Binding.CredentialRef.Revision = 2
		if inverse {
			cap.ApiDescriptor.Binding.CredentialRef.Revision = 1
		}
	case "expiry":
		deadline := int64(1)
		x.CallDescriptor.KeyValidUntilUnixMs = &deadline
		if inverse {
			x.CallDescriptor.KeyValidUntilUnixMs = nil
		}
	case "attempt-operation":
		set(&x.Attempt.OperationId.LocalId, op.Ref.Name.LocalId+"-other", op.Ref.Name.LocalId)
	case "send-attempt":
		set(&x.Send.AttemptId.LocalId, x.Attempt.Ref.Name.LocalId+"-other", x.Attempt.Ref.Name.LocalId)
	case "descriptor-account":
		set(&x.CallDescriptor.ApiDescriptor.Binding.Account, "other-supplier-account", cap.ApiDescriptor.Binding.Account)
	case "query-subject":
		set(&x.CallDescriptor.QuerySubject.ExternalKey, op.QuerySubject.ExternalKey+"-other", op.QuerySubject.ExternalKey)
	case "query-declaration-binding":
		set(&x.Attempt.Capabilities.ParameterBinding, "", "EXACT_REQUEST")
	case "previous-attempt":
		set(&x.PreviousSends[0].AttemptId.LocalId, x.Attempt.Ref.Name.LocalId+"-other", x.Attempt.Ref.Name.LocalId)
	case "credential-unknown", "subject-unknown":
		var m proto.Message = cap.ApiDescriptor.Binding.CredentialRef
		if field == "subject-unknown" {
			m = x.CallDescriptor.QuerySubject
		}
		unknown := protowire.AppendVarint(protowire.AppendTag(nil, 19001, protowire.VarintType), 1)
		if inverse {
			unknown = nil
		}
		m.ProtoReflect().SetUnknown(unknown)
	}
	cap.ApiDescriptor.Digest = command.APIDescriptorDigest(cap.ApiDescriptor)
	if x != nil {
		x.CallDescriptor.ApiDescriptor.Digest = command.APIDescriptorDigest(x.CallDescriptor.ApiDescriptor)
		descriptor := proto.Clone(x.CallDescriptor).(*v1.CallDescriptor)
		descriptor.Digest = ""
		x.CallDescriptor.Digest = command.SemanticFingerprint("call-descriptor-v1", descriptor)
	}
}

// nativeCompatibilityHistory 经真实平台凭据和公开负责方产生原动作，不用 SQL 创建业务事实。
func nativeCompatibilityHistory(t *testing.T, kind string) (*fixture, assembly.Options, *v1.Operation, func() []int) {
	t.Helper()
	var mu sync.Mutex
	posts, gets, bills := 0, 0, 0
	effects := make(map[string]bool)
	counters := func() []int { mu.Lock(); defer mu.Unlock(); return []int{posts, gets, len(effects), bills} }
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-api-original-history" {
			t.Error("original native credential missing")
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		amount := int64(7)
		if r.Method == "POST" {
			posts++
			effects[r.Header.Get("Idempotency-Key")] = true
		} else {
			gets++
			amount = 2
		}
		bills++
		if r.Method == "POST" {
			w.WriteHeader(500)
			_ = json.NewEncoder(w).Encode(map[string]any{"billing": apiQueryBoundaryBill(r, amount)})
			return
		}
		response := apiQueryResponse(r)
		response["applied"] = effects[r.Header.Get("Lerna-Query-Key")]
		response["billing"] = apiQueryBoundaryBill(r, amount)
		_ = json.NewEncoder(w).Encode(response)
	})
	f := newFixtureWithTarget(t, 200, 200, false, target)
	var descriptor *v1.ApiDescriptor
	var queryCap, queryGrant *v1.Ref
	if kind == "query" {
		descriptor, queryCap, queryGrant = configureAPIQueryable(t, f, true)
	} else {
		descriptor = configureAPI(t, f)
	}
	if kind == "expired" || kind == "non-idempotent" {
		cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
		if e != nil {
			t.Fatal(e)
		}
		cap.Ref, cap.ApprovedBy = nil, nil
		if kind == "expired" {
			cap.IdempotencyRetentionMs = 500
		} else {
			cap.ApiDescriptor.Idempotent = false
			cap.ApiDescriptor.Digest = command.APIDescriptorDigest(cap.ApiDescriptor)
		}
		r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("api-original-history-profile"), Capability: cap})
		accepted(t, r, e)
		f.capability = r.ResultRef
	}
	keychain := withAPIKeychain(t, f, descriptor, []byte("synthetic-api-original-history"))
	options := assembly.Options{APIKeychainPath: keychain.Path()}
	var admission *v1.Admission
	var start *v1.StartExecutionCommand
	var e error
	if kind == "no-execution" {
		proposal := f.propose(t, nil)
		r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("api-original-unprepared"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant})
		accepted(t, r, e)
		admission, e = f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
		if e != nil {
			t.Fatal(e)
		}
		if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
			t.Fatal(e)
		}
	} else {
		if kind == "previous-send" {
			configureResendLimit(t, f, 2)
		}
		admission, start = prepareStart(t, f)
		if kind != "registered" {
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
		}
		if kind == "previous-send" {
			next := prepareNextResend(t, f, admission, start)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, next)
			accepted(t, r, e)
		}
		if kind == "closed" {
			r, e := beginNonSuccessClose(t, f, "api-original-fixed-close", "FAILED", "UNABLE_TO_COMPLETE")
			accepted(t, r, e)
			if e = processNonSuccessClosings(f, "FAILED"); e != nil {
				t.Fatal(e)
			}
		}
		if kind == "query" {
			r, e := f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, admission, queryCap, queryGrant))
			accepted(t, r, e)
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, admission.OperationId)
			if e != nil || plan.State != "COMPLETED" || len(plan.QueryRefs) != 1 {
				t.Fatalf("actual independent original query: %v %v", plan, e)
			}
			relation, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
			if e != nil {
				t.Fatal(e)
			}
			admission, e = f.h.Tasks.QueryAdmission(f.ctx, f.caller, relation.AdmissionReceipt.ResultRef)
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
	if e != nil || original == nil {
		t.Fatalf("actual original native operation: %v %v", original, e)
	}
	return f, options, original, counters
}

func faultOriginalAPIRecord(t *testing.T, f *fixture, changed *v1.Operation) {
	t.Helper()
	body, e := proto.Marshal(changed)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.StorageFaultSQL("UPDATE operations SET record=X'" + hex.EncodeToString(body) + "' WHERE user_id='u' AND domain_id='d/ledger' AND id='" + changed.Ref.Name.LocalId + "'"); e != nil {
		t.Fatal(e)
	}
	actual, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, changed.Ref.Name)
	if e != nil || !proto.Equal(actual, changed) {
		t.Fatalf("actual original metadata mutation missing: %v %v", actual, e)
	}
}

func assertOriginalAPIRefusedBeforeOwners(t *testing.T, f *fixture, options assembly.Options, changed *v1.Operation, counters func() []int) {
	t.Helper()
	pending := &v1.SubmitGoalCommand{Identity: header("api-history-unrelated-ready").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "keep other real owner READY"}
	receipt, e := f.h.Sessions.SubmitGoal(f.ctx, f.caller, pending)
	if e != nil {
		t.Fatal(e)
	}
	job, e := f.h.Durable.QueryJob(f.ctx, f.caller, receipt.JobRef.Name)
	if e != nil || job.State != "READY" || job.ClaimEpoch != 0 || job.Attempts != 0 {
		t.Fatalf("actual other owner frontier: %v %v", job, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	sources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	originalFacts := originalAPICompatibilityFacts(t, f, changed)
	before := counters()
	h, openErr := assembly.OpenWithOptions(f.path, "u", "d", options)
	if h != nil {
		if e = h.Close(); e != nil {
			t.Fatal(e)
		}
	}
	var failure *command.Failure
	if !errors.As(openErr, &failure) || (failure.Detail.Code != "PREPARATION_UNRECOVERABLE" && failure.Detail.Code != "UNSUPPORTED_FEATURE") {
		t.Errorf("unsupported original native history accepted or wrong failure: %v", openErr)
	}
	after, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, pending.Identity)
	if e != nil || !proto.Equal(after.GetReceipt(), receipt) {
		t.Errorf("other original receipt advanced before refusal: %v %v", after, e)
	}
	afterJob, e := f.h.Durable.QueryJob(f.ctx, f.caller, job.Ref.Name)
	if e != nil || !proto.Equal(afterJob, job) {
		t.Errorf("other original READY epoch/attempt advanced: %v %v", afterJob, e)
	}
	retained, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, changed.Ref.Name)
	if e != nil || !proto.Equal(retained, changed) {
		t.Errorf("original saved record changed: %v %v", retained, e)
	}
	afterBudget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || !proto.Equal(afterBudget, budget) {
		t.Errorf("original budget changed: %v %v", afterBudget, e)
	}
	afterResult, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(afterResult, result) {
		t.Errorf("original fixed Result changed: %v %v", afterResult, e)
	}
	afterSources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil || len(afterSources) != len(sources) {
		t.Errorf("source population advanced: %v", e)
	} else {
		for i := range sources {
			if !proto.Equal(afterSources[i], sources[i]) {
				t.Errorf("original source/ACK changed: %d", i)
			}
		}
	}
	if fmt.Sprint(before) != fmt.Sprint(counters()) {
		t.Error("startup issued another request/effect/bill")
	}
	assertCompatibilityFacts(t, originalFacts, originalAPICompatibilityFacts(t, f, changed))
	t.Logf("actual_original_POST_GET_effect_bill=%v compatibility_refused=%t", before, openErr != nil)
}

// originalAPICompatibilityFacts 只查询原负责方的回执、发送证据和固定关闭事实。
func originalAPICompatibilityFacts(t *testing.T, f *fixture, changed *v1.Operation) map[string]proto.Message {
	t.Helper()
	facts := make(map[string]proto.Message)
	readReceipt := func(id *v1.CommandIdentity) {
		facts["receipt/"+id.TargetDomainId+"/"+id.IssuerId+"/"+id.CommandId] = originalAPIReceipt(t, f, id)
	}
	readReceipt(header("goal").Identity)
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	facts["original-planning"] = planning
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	if result != nil {
		facts["fixed-result"] = result
		view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
		if e != nil {
			t.Fatal(e)
		}
		facts["original-closing-view"] = view
		readReceipt(header("api-original-fixed-close").Identity)
	}
	ops := []*v1.Operation{changed}
	if changed.QuerySubject != nil {
		original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, changed.QuerySubject.OperationId)
		if e != nil || original == nil {
			t.Fatalf("original query subject operation missing: %v %v", original, e)
		}
		ops = append(ops, original)
	}
	for _, op := range ops {
		facts["operation/"+op.Ref.Name.LocalId] = op
		admission, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, op.AdmissionRef)
		if e != nil || admission == nil {
			t.Fatalf("original API admission missing: %v %v", admission, e)
		}
		facts["admission/"+op.Ref.Name.LocalId] = admission
		readReceipt(admission.HandoffIdentity)
		if op.Execution == nil {
			continue
		}
		for _, send := range append([]*v1.PhysicalSend{op.Execution.Send}, op.Execution.PreviousSends...) {
			if send.StartReceipt != nil {
				readReceipt(send.StartReceipt.Identity)
			}
			if send.ObservationRef != nil {
				raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, send.ObservationRef)
				if e != nil || raw == nil {
					t.Fatalf("original API observation missing: %v %v", raw, e)
				}
				facts["observation/"+send.Ref.Name.LocalId] = raw
			}
			source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, send.Ref)
			if e != nil {
				t.Fatal(e)
			}
			facts["billing-source/"+send.Ref.Name.LocalId] = source
		}
	}
	return facts
}
