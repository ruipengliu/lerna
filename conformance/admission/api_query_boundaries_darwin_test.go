//go:build darwin && cgo

package admission_test

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G5、G10、G11
func TestAPIQueryDeadlineStopsNewReadsAndRetainsOriginalResponsibility(t *testing.T) {
	var writes, queries, effects, bills atomic.Int64
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-api-deadline-secret" {
			t.Error("native credential missing")
			w.WriteHeader(401)
			return
		}
		bills.Add(1)
		if r.Method == "POST" {
			writes.Add(1)
			effects.Add(1)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		if r.Method != "GET" {
			t.Error("unexpected method")
			w.WriteHeader(405)
			return
		}
		queries.Add(1)
		response := apiQueryResponse(r)
		response["applied"], response["terminal"] = false, false
		response["billing"] = apiQueryBoundaryBill(r, 2)
		_ = json.NewEncoder(w).Encode(response)
	})
	f := newFixtureWithTarget(t, 200, 200, false, target)
	d, queryCap, queryGrant := configureAPIQueryable(t, f, false)
	keychain := withAPIKeychain(t, f, d, []byte("synthetic-api-deadline-secret"))
	a, start := prepareStart(t, f)
	receipt, err := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, receipt, err)
	before, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	request := reconciliationCommand(f, a, queryCap, queryGrant)
	request.Limits.DeadlineUnixMs = time.Now().Add(time.Second).UnixMilli()
	receipt, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, request)
	accepted(t, receipt, err)
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || plan.State != "WAITING" || plan.CheckCount != 1 {
		t.Fatalf("weak query: %v %v", plan, err)
	}
	time.Sleep(max(0, time.Until(time.UnixMilli(max(plan.NextReconcileAtUnixMs, request.Limits.DeadlineUnixMs)))) + 20*time.Millisecond)
	if err = f.h.Close(); err != nil {
		t.Fatal(err)
	}
	f.h, err = assembly.OpenWithOptions(f.path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || plan.State != "PAUSED" || plan.PauseReason != "TIME_LIMIT" || plan.CheckCount != 1 || len(plan.QueryRefs) != 1 {
		t.Fatalf("deadline: %v %v", plan, err)
	}
	after, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || after.Effect.Outcome != "UNKNOWN" || after.Effect.LateEffect != "MAY_OCCUR" || !proto.Equal(before.Execution, after.Execution) {
		t.Fatalf("original responsibility: %v %v", after, err)
	}
	relation, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
	if err != nil {
		t.Fatal(err)
	}
	query, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
	if err != nil || query.Lifecycle != "SETTLED" || !proto.Equal(query.QuerySubject.OperationId, a.OperationId) || !proto.Equal(query.QuerySubject.AttemptId, before.Execution.Attempt.Ref.Name) || query.QuerySubject.ExternalKey != before.Execution.Attempt.ExternalKey {
		t.Fatalf("query subject: %v %v", query, err)
	}
	source, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, query.Execution.Send.Ref)
	if err != nil || source.Amount == nil || *source.Amount != 2 || source.Status != "SETTLED" {
		t.Fatalf("query bill: %v %v", source, err)
	}
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || budget.Reserved != 30 || budget.Settled != 2 {
		t.Fatalf("original and query fees: %v %v", budget, err)
	}
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 1 || queries.Load() != 1 || effects.Load() != 1 || bills.Load() != 2 {
		t.Fatalf("target writes=%d queries=%d effects=%d bills=%d", writes.Load(), queries.Load(), effects.Load(), bills.Load())
	}
}

func apiQueryBoundaryBill(r *http.Request, amount int64) map[string]any {
	return map[string]any{"rule": "reference-billing-v1", "namespace": "lerna-reference", "account": r.Header.Get("Lerna-Account"), "native_instance": r.Header.Get("Lerna-Send-Id"), "component": "call", "send_id": r.Header.Get("Lerna-Send-Id"), "external_key": r.Header.Get("Idempotency-Key"), "source_version": 1, "unit": "USD_MICRO", "amount": amount, "final": true, "price_version": "reference-price-v1"}
}

// 规则：G1、G3、G5、G10、G11
func TestAPIQueryNetworkTimeoutKeepsQueryAndOriginalFeesUnknown(t *testing.T) {
	var writes, queries, effects, bills atomic.Int64
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-api-timeout-secret" {
			t.Error("native credential missing")
			w.WriteHeader(401)
			return
		}
		bills.Add(1)
		if r.Method == "POST" {
			writes.Add(1)
			effects.Add(1)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		if r.Method != "GET" {
			t.Error("unexpected method")
			w.WriteHeader(405)
			return
		}
		queries.Add(1)
		// 真实目标保持连接，不主动丢回执；由生产网络出口的截止时间结束读取。
		<-r.Context().Done()
	})
	f := newFixtureWithTarget(t, 200, 200, false, target)
	d, cap, grant := configureAPIQueryable(t, f, false)
	keychain := withAPIKeychain(t, f, d, []byte("synthetic-api-timeout-secret"))
	a, start := prepareStart(t, f)
	r, err := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, err)
	before, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	r, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
	accepted(t, r, err)
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || plan.State != "PAUSED" || plan.PauseReason != "QUERY_RESULT_UNKNOWN" || plan.CheckCount != 1 || len(plan.QueryRefs) != 1 {
		t.Fatalf("timeout responsibility: %v %v", plan, err)
	}
	relation, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
	if err != nil {
		t.Fatal(err)
	}
	query, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
	if err != nil || query.Effect.Outcome != "UNKNOWN" || query.Effect.LateEffect != "MAY_OCCUR" || query.Lifecycle == "SETTLED" {
		t.Fatalf("query incorrectly terminal: %v %v", query, err)
	}
	raw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, query.Execution.Send.ObservationRef)
	if err != nil || raw.TransportError != "READ_FAILED" || raw.FinishedAtUnixMs-raw.StartedAtUnixMs < 4000 || !proto.Equal(raw.OperationId, query.Ref.Name) || !proto.Equal(raw.AttemptId, query.Execution.Attempt.Ref.Name) || raw.ExternalKey != query.Execution.Attempt.ExternalKey {
		t.Fatalf("actual timed-out query: %v %v", raw, err)
	}
	if !proto.Equal(raw.QuerySubject.OperationId, a.OperationId) || !proto.Equal(raw.QuerySubject.AttemptId, before.Execution.Attempt.Ref.Name) || raw.QuerySubject.ExternalKey != before.Execution.Attempt.ExternalKey || raw.QuerySubject.TargetScope != d.Binding.Resource {
		t.Fatalf("query subject drifted: %v", raw.QuerySubject)
	}
	for _, send := range []*v1.PhysicalSend{before.Execution.Send, query.Execution.Send} {
		source, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, send.Ref)
		if err != nil || source == nil || source.Amount != nil {
			t.Fatalf("unknown fee lost: %v %v", source, err)
		}
	}
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || budget.Reserved != 35 || budget.Settled != 0 {
		t.Fatalf("unresolved actual fees: %v %v", budget, err)
	}
	if err = f.h.Close(); err != nil {
		t.Fatal(err)
	}
	f.h, err = assembly.OpenWithOptions(f.path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	after, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || after.Effect.Outcome != "UNKNOWN" || after.Effect.LateEffect != "MAY_OCCUR" || !proto.Equal(before.Execution, after.Execution) {
		t.Fatalf("original after timeout: %v %v", after, err)
	}
	restored, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, query.Ref.Name)
	if err != nil || !proto.Equal(restored, query) || writes.Load() != 1 || queries.Load() != 1 || effects.Load() != 1 || bills.Load() != 2 {
		t.Fatalf("restart changed query or retried: %v %v writes=%d queries=%d effects=%d bills=%d", restored, err, writes.Load(), queries.Load(), effects.Load(), bills.Load())
	}
}

// 规则：G1、G3、G4、G5、G10、G11
func TestAPIQueryCurrentCapabilityReplacementStopsOldPlan(t *testing.T) {
	for _, mode := range []string{"replacement", "weakened"} {
		t.Run(mode, func(t *testing.T) {
			var writes, queries, effects, bills atomic.Int64
			target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer synthetic-api-deadline-secret" {
					t.Error("native credential missing")
					w.WriteHeader(401)
					return
				}
				bills.Add(1)
				if r.Method == "POST" {
					writes.Add(1)
					effects.Add(1)
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				if r.Method != "GET" {
					t.Error("unexpected method")
					w.WriteHeader(405)
					return
				}
				queries.Add(1)
				response := apiQueryResponse(r)
				response["applied"], response["terminal"] = false, false
				response["billing"] = apiQueryBoundaryBill(r, 2)
				_ = json.NewEncoder(w).Encode(response)
			})
			f := newFixtureWithTarget(t, 200, 200, false, target)
			d, queryCap, queryGrant := configureAPIQueryable(t, f, false)
			keychain := withAPIKeychain(t, f, d, []byte("synthetic-api-deadline-secret"))
			a, start := prepareStart(t, f)
			receipt, err := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, receipt, err)
			before, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if err != nil {
				t.Fatal(err)
			}
			request := reconciliationCommand(f, a, queryCap, queryGrant)
			receipt, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, request)
			accepted(t, receipt, err)
			if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
				t.Fatal(err)
			}
			plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if err != nil || plan.State != "WAITING" || plan.CheckCount != 1 {
				t.Fatalf("weak query: %v %v", plan, err)
			}
			oldCapability, err := f.h.Tasks.QueryCapability(f.ctx, f.caller, queryCap)
			if err != nil {
				t.Fatal(err)
			}
			replacement := proto.Clone(oldCapability).(*v1.Capability)
			replacement.Ref, replacement.ApprovedBy = nil, nil
			if mode == "weakened" {
				replacement.ApiDescriptor.Queryable = false
				replacement.ApiDescriptor.Digest = command.APIDescriptorDigest(replacement.ApiDescriptor)
			}
			receipt, err = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("replace-query-cap"), Capability: replacement, Replaces: queryCap})
			accepted(t, receipt, err)
			if receipt.ResultRef.Revision != queryCap.Revision+1 {
				t.Fatal("query capability not replaced")
			}
			historical, err := f.h.Tasks.QueryCapability(f.ctx, f.caller, queryCap)
			if err != nil || !proto.Equal(historical, oldCapability) {
				t.Fatalf("old query descriptor changed: %v %v", historical, err)
			}
			time.Sleep(max(0, time.Until(time.UnixMilli(plan.NextReconcileAtUnixMs))) + 20*time.Millisecond)
			if err = f.h.Close(); err != nil {
				t.Fatal(err)
			}
			f.h, err = assembly.OpenWithOptions(f.path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
			if err != nil {
				t.Fatal(err)
			}
			plan, err = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if err != nil || plan.State != "PAUSED" || plan.PauseReason != "CAPABILITY_UNAVAILABLE" || plan.CheckCount != 1 || len(plan.QueryRefs) != 1 {
				t.Fatalf("current capability gate: %v %v", plan, err)
			}
			after, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if err != nil || after.Effect.Outcome != "UNKNOWN" || after.Effect.LateEffect != "MAY_OCCUR" || !proto.Equal(before.Execution, after.Execution) {
				t.Fatalf("original responsibility: %v %v", after, err)
			}
			relation, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
			if err != nil {
				t.Fatal(err)
			}
			query, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
			if err != nil || query.Lifecycle != "SETTLED" || !proto.Equal(query.QuerySubject.OperationId, a.OperationId) || !proto.Equal(query.QuerySubject.AttemptId, before.Execution.Attempt.Ref.Name) || query.QuerySubject.ExternalKey != before.Execution.Attempt.ExternalKey {
				t.Fatalf("query subject: %v %v", query, err)
			}
			source, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, query.Execution.Send.Ref)
			if err != nil || source.Amount == nil || *source.Amount != 2 || source.Status != "SETTLED" {
				t.Fatalf("query bill: %v %v", source, err)
			}
			budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
			if err != nil || budget.Reserved != 30 || budget.Settled != 2 {
				t.Fatalf("original and query fees: %v %v", budget, err)
			}
			if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
				t.Fatal(err)
			}
			if writes.Load() != 1 || queries.Load() != 1 || effects.Load() != 1 || bills.Load() != 2 {
				t.Fatalf("target writes=%d queries=%d effects=%d bills=%d", writes.Load(), queries.Load(), effects.Load(), bills.Load())
			}
		})
	}
}

// 规则：G1、G3、G5、G10、G11、R7、开始-5
func TestAPIQueryFirstProofPrecedesLateOriginalObservationAndBill(t *testing.T) {
	type received struct{ method, operation, attempt, key, send, subjectOperation, subjectAttempt, subjectKey string }
	var mu sync.Mutex
	var requests []received
	var effects = map[string]bool{}
	var bills = map[string]int64{}
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-api-query-first-secret" {
			t.Error("native credential missing")
			w.WriteHeader(401)
			return
		}
		got := received{r.Method, r.Header.Get("Lerna-Operation"), r.Header.Get("Lerna-Attempt"), r.Header.Get("Idempotency-Key"), r.Header.Get("Lerna-Send-Id"), r.Header.Get("Lerna-Query-Operation"), r.Header.Get("Lerna-Query-Attempt"), r.Header.Get("Lerna-Query-Key")}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, got)
		if r.Method == "POST" {
			if string(body) != `{"quantity":1,"value":"hello"}` {
				t.Error("noncanonical write")
			}
			effects[got.key] = true
			bills[got.send] = 4
			_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": got.key, "attempt_id": got.attempt, "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": true, "terminal": true, "billing": apiQueryBoundaryBill(r, 4)})
			return
		}
		if r.Method != "GET" || len(body) != 0 {
			t.Error("unexpected query payload")
			w.WriteHeader(405)
			return
		}
		bills[got.send] = 2
		response := apiQueryResponse(r)
		response["applied"] = effects[got.subjectKey]
		response["billing"] = apiQueryBoundaryBill(r, 2)
		_ = json.NewEncoder(w).Encode(response)
	})
	f := newFixtureWithTarget(t, 200, 200, false, target)
	d, cap, grant := configureAPIQueryable(t, f, true)
	configureResendLimit(t, f, 2)
	keychain := withAPIKeychain(t, f, d, []byte("synthetic-api-query-first-secret"))
	a, start := prepareStart(t, f)
	// 使用实际 P4/P5 和原生 HTTP 取得回报，暂缓回报交接，模拟合法迟到来源。
	late := performAPIWithoutObservation(t, f, a, start, keychain.Store())
	before, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || before.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("unreported write: %v %v", before, err)
	}
	r, err := f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("api-before-query"), OperationId: a.OperationId, PreviousSendRef: before.Execution.Send.Ref, Claim: start.Claim})
	if err != nil || r.GetError().GetCode() != "RESEND_QUERY_FIRST" {
		t.Fatalf("query-first gate: %v %v", r, err)
	}
	r, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
	accepted(t, r, err)
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || plan.State != "COMPLETED" || plan.CheckCount != 1 {
		t.Fatalf("actual query proof: %v %v", plan, err)
	}
	queried, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || queried.Effect.Outcome != "APPLIED" || queried.Effect.LateEffect != "RULED_OUT" || !proto.Equal(queried.Execution, before.Execution) {
		t.Fatalf("query relabeled original: %v %v", queried, err)
	}
	relation, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
	if err != nil {
		t.Fatal(err)
	}
	query, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
	if err != nil {
		t.Fatal(err)
	}
	r, err = f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("api-after-query-proof"), OperationId: a.OperationId, PreviousSendRef: before.Execution.Send.Ref, Claim: start.Claim})
	if err == nil && r.GetDecision() == v1.Decision_DECISION_ACCEPTED {
		t.Fatal("query-proven terminal write permitted resend")
	}
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || budget.Reserved != 30 || budget.Settled != 2 {
		t.Fatalf("before late original bill: %v %v", budget, err)
	}
	savePhysicalObservation(t, f, late)
	after, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || after.Effect.Outcome != "APPLIED" || after.Effect.LateEffect != "RULED_OUT" || after.Effect.EvidenceConflict || len(after.Effect.EvidenceRefs) != 2 || !proto.Equal(after.Execution.Attempt.Ref.Name, before.Execution.Attempt.Ref.Name) || after.Execution.Attempt.ExternalKey != before.Execution.Attempt.ExternalKey || !proto.Equal(after.Execution.CallDescriptor, before.Execution.CallDescriptor) || after.Execution.Send.Ref.Name.LocalId != before.Execution.Send.Ref.Name.LocalId || after.Execution.Send.SendSeq != 1 {
		t.Fatalf("late original evidence: %v %v", after, err)
	}
	raw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, late.Observation.Ref)
	if err != nil || !proto.Equal(raw.OperationId, a.OperationId) || !proto.Equal(raw.AttemptId, before.Execution.Attempt.Ref.Name) || raw.ExternalKey != before.Execution.Attempt.ExternalKey || raw.QuerySubject != nil {
		t.Fatalf("late original relabeled: %v %v", raw, err)
	}
	queryRaw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, relation.ObservationRef)
	if err != nil || !proto.Equal(queryRaw.OperationId, query.Ref.Name) || !proto.Equal(queryRaw.AttemptId, query.Execution.Attempt.Ref.Name) || !proto.Equal(queryRaw.QuerySubject.OperationId, a.OperationId) {
		t.Fatalf("query source relabeled: %v %v", queryRaw, err)
	}
	for index, send := range []*v1.PhysicalSend{after.Execution.Send, query.Execution.Send} {
		expected := int64(4)
		if index == 1 {
			expected = 2
		}
		source, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, send.Ref)
		if err != nil || source.Amount == nil || *source.Amount != expected || source.Identity.NativeInstance != send.Ref.Name.LocalId {
			t.Fatalf("separate source: %v %v", source, err)
		}
	}
	budget, err = f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || budget.Reserved != 0 || budget.Settled != 6 {
		t.Fatalf("late fees: %v %v", budget, err)
	}
	savePhysicalObservation(t, f, late)
	if err = f.h.Close(); err != nil {
		t.Fatal(err)
	}
	f.h, err = assembly.OpenWithOptions(f.path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || !proto.Equal(restored, after) {
		t.Fatalf("restart changed original: %v %v", restored, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || requests[0].method != "POST" || requests[1].method != "GET" || len(effects) != 1 || len(bills) != 2 {
		t.Fatalf("actual order/effects/bills: %v %d %d", requests, len(effects), len(bills))
	}
	if requests[0].operation != a.OperationId.LocalId || requests[0].attempt != before.Execution.Attempt.Ref.Name.LocalId || requests[0].key != before.Execution.Attempt.ExternalKey || requests[1].operation != query.Ref.Name.LocalId || requests[1].attempt != query.Execution.Attempt.Ref.Name.LocalId || requests[1].key != query.Execution.Attempt.ExternalKey || requests[1].subjectOperation != requests[0].operation || requests[1].subjectAttempt != requests[0].attempt || requests[1].subjectKey != requests[0].key {
		t.Fatalf("actual identities: %v", requests)
	}
}

// 规则：G1、G3、G4、G5、G10、G11、开始-5
func TestAPIQueryFirstWeakOrLostReadAllowsOnlyExplicitOriginalKeyResend(t *testing.T) {
	for _, mode := range []string{"weak", "drop"} {
		t.Run(mode, func(t *testing.T) {
			type received struct{ method, operation, attempt, key, send, subjectOperation, subjectAttempt, subjectKey string }
			var mu sync.Mutex
			var requests []received
			effects := map[string]bool{}
			bills := map[string]int64{}
			writeCount := 0
			target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer synthetic-api-query-resend-secret" {
					t.Error("native credential missing")
					w.WriteHeader(401)
					return
				}
				got := received{r.Method, r.Header.Get("Lerna-Operation"), r.Header.Get("Lerna-Attempt"), r.Header.Get("Idempotency-Key"), r.Header.Get("Lerna-Send-Id"), r.Header.Get("Lerna-Query-Operation"), r.Header.Get("Lerna-Query-Attempt"), r.Header.Get("Lerna-Query-Key")}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				requests = append(requests, got)
				if r.Method == "POST" {
					if string(body) != `{"quantity":1,"value":"hello"}` {
						t.Error("noncanonical resend")
					}
					writeCount++
					effects[got.key] = true
					bills[got.send] = 7
					if writeCount == 1 {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = conn.Close()
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": got.key, "attempt_id": got.attempt, "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": true, "terminal": true, "billing": apiQueryBoundaryBill(r, 7)})
					return
				}
				if r.Method != "GET" || len(body) != 0 {
					t.Error("unexpected query payload")
					w.WriteHeader(405)
					return
				}
				bills[got.send] = 2
				if mode == "drop" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				response := apiQueryResponse(r)
				response["applied"], response["terminal"] = false, false
				response["billing"] = apiQueryBoundaryBill(r, 2)
				_ = json.NewEncoder(w).Encode(response)
			})
			f := newFixtureWithTarget(t, 200, 200, false, target)
			d, cap, grant := configureAPIQueryable(t, f, true)
			configureResendLimit(t, f, 2)
			keychain := withAPIKeychain(t, f, d, []byte("synthetic-api-query-resend-secret"))
			a, start := prepareStart(t, f)
			r, err := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, err)
			before, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if err != nil {
				t.Fatal(err)
			}
			claim, err := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("api-query-first-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "api-retry-worker"})
			accepted(t, claim, err)
			if len(claim.Jobs) != 1 {
				t.Fatalf("real retry claim: %v", claim)
			}
			prepare := &v1.PrepareResendCommand{Header: ledgerHeader("api-resend-before-query"), OperationId: a.OperationId, PreviousSendRef: before.Send.Ref, Claim: claim.Jobs[0]}
			r, err = f.h.Ledger.PrepareResend(f.ctx, f.caller, prepare)
			if err != nil || r.GetError().GetCode() != "RESEND_QUERY_FIRST" {
				t.Fatalf("query-first bypass: %v %v", r, err)
			}
			r, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
			accepted(t, r, err)
			if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
				t.Fatal(err)
			}
			plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if err != nil || plan.CheckCount != 1 || len(plan.QueryRefs) != 1 {
				t.Fatalf("actual query: %v %v", plan, err)
			}
			relation, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
			if err != nil {
				t.Fatal(err)
			}
			query, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
			if err != nil {
				t.Fatal(err)
			}
			prepare.Header = ledgerHeader("api-explicit-resend-after-query")
			r, err = f.h.Ledger.PrepareResend(f.ctx, f.caller, prepare)
			accepted(t, r, err)
			next, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if err != nil || !proto.Equal(next.Send.ResendQueryObservationRef, relation.ObservationRef) || !proto.Equal(next.Attempt.Ref.Name, before.Attempt.Ref.Name) || next.Attempt.ExternalKey != before.Attempt.ExternalKey || !proto.Equal(next.CallDescriptor, before.CallDescriptor) {
				t.Fatalf("resend changed identity/evidence: %v %v", next, err)
			}
			r, err = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, resendStart(t, f, a, start, next, claim.Jobs[0]))
			accepted(t, r, err)
			after, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if err != nil || after.Effect.Outcome != "APPLIED" || after.Effect.LateEffect != "RULED_OUT" || len(after.Execution.PreviousSends) != 1 || after.Execution.Send.SendSeq != 2 {
				t.Fatalf("explicit same-key outcome: %v %v", after, err)
			}
			queryAfter, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, query.Ref.Name)
			if err != nil || !proto.Equal(queryAfter, query) {
				t.Fatalf("original success erased query responsibility: %v %v", queryAfter, err)
			}
			wantReserve, wantSettled := int64(30), int64(9)
			if mode == "drop" {
				wantReserve, wantSettled = 35, 7
				if query.Effect.Outcome != "UNKNOWN" {
					t.Fatal("lost query became terminal")
				}
			}
			budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
			if err != nil || budget.Reserved != wantReserve || budget.Settled != wantSettled {
				t.Fatalf("three fee responsibilities: %v %v", budget, err)
			}
			seen := map[string]bool{}
			for _, send := range []*v1.PhysicalSend{after.Execution.PreviousSends[0], query.Execution.Send, after.Execution.Send} {
				source, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, send.Ref)
				if err != nil || source == nil || seen[source.Ref.Name.LocalId] {
					t.Fatalf("missing/merged source: %v %v", source, err)
				}
				seen[source.Ref.Name.LocalId] = true
			}
			if err = f.h.Close(); err != nil {
				t.Fatal(err)
			}
			f.h, err = assembly.OpenWithOptions(f.path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
			if err != nil {
				t.Fatal(err)
			}
			restored, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if err != nil || !proto.Equal(restored, after) {
				t.Fatalf("restart rewrote original: %v %v", restored, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 3 || requests[0].method != "POST" || requests[1].method != "GET" || requests[2].method != "POST" || len(effects) != 1 || len(bills) != 3 {
				t.Fatalf("actual ordering/effects/bills: %v %d %d", requests, len(effects), len(bills))
			}
			if requests[0].operation != a.OperationId.LocalId || requests[0].attempt != before.Attempt.Ref.Name.LocalId || requests[0].key != before.Attempt.ExternalKey || requests[2].operation != requests[0].operation || requests[2].attempt != requests[0].attempt || requests[2].key != requests[0].key || requests[2].send == requests[0].send || requests[1].operation != query.Ref.Name.LocalId || requests[1].attempt != query.Execution.Attempt.Ref.Name.LocalId || requests[1].key != query.Execution.Attempt.ExternalKey || requests[1].subjectOperation != requests[0].operation || requests[1].subjectAttempt != requests[0].attempt || requests[1].subjectKey != requests[0].key {
				t.Fatalf("actual identity drift: %v", requests)
			}
		})
	}
}
