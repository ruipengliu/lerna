//go:build darwin && cgo

package admission_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G5、G10、G11、开始-5
func TestAPIThirdSendRequiresNewQueryObservation(t *testing.T) {
	type received struct{ method, operation, attempt, key, send, subjectOperation, subjectAttempt, subjectKey string }
	var mu sync.Mutex
	var requests []received
	effects := map[string]bool{}
	bills := map[string]int64{}
	writes := 0
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-api-third-send-secret" {
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
			writes++
			effects[got.key] = true
			bills[got.send] = 7
			if writes < 3 {
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
		response := apiQueryResponse(r)
		response["applied"], response["terminal"] = false, false
		response["billing"] = apiQueryBoundaryBill(r, 2)
		_ = json.NewEncoder(w).Encode(response)
	})
	f := newFixtureWithTarget(t, 200, 200, false, target)
	d, cap, grant := configureAPIQueryable(t, f, true)
	configureResendLimit(t, f, 3)
	keychain := withAPIKeychain(t, f, d, []byte("synthetic-api-third-send-secret"))
	a, first := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, err := f.h.Egress.Invoke(f.ctx, actor, first)
	accepted(t, r, err)
	original, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || original.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("lost first write: %v %v", original, err)
	}
	r, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
	accepted(t, r, err)
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || plan.CheckCount != 1 || len(plan.QueryRefs) != 1 || plan.State != "WAITING" {
		t.Fatalf("first weak query: %v %v", plan, err)
	}
	firstRelation, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
	if err != nil {
		t.Fatal(err)
	}
	claimFor := func(seq int) *v1.Job {
		t.Helper()
		claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader(fmt.Sprintf("third-send-claim-%d", seq)).Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: fmt.Sprintf("third-send-worker-%d", seq)})
		accepted(t, claim, e)
		if len(claim.Jobs) != 1 {
			t.Fatalf("missing real execution claim: %v", claim)
		}
		return claim.Jobs[0]
	}
	startFor := func(x *v1.Execution, claim *v1.Job) *v1.StartExecutionCommand {
		t.Helper()
		binding := proto.Clone(first.Binding).(*v1.ExitCredentialBinding)
		binding.SendSeq, binding.ExecutorInstance = x.Send.SendSeq, x.Send.ProcessInstance
		receipt, e := f.h.Grants.IssueCredential(f.ctx, f.caller, &v1.IssueExitCredentialCommand{Header: header(fmt.Sprintf("third-send-credential-%d", x.Send.SendSeq)), AdmissionRef: a.Ref, Binding: binding, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli()})
		accepted(t, receipt, e)
		h := header("start:" + x.Send.Ref.Name.LocalId)
		h.Identity.IssuerId = "egress"
		return &v1.StartExecutionCommand{Header: h, AdmissionRef: a.Ref, CredentialRef: receipt.ResultRef, Binding: binding, CallDescriptor: x.CallDescriptor, Claim: claim}
	}
	claim2 := claimFor(2)
	prepare2 := &v1.PrepareResendCommand{Header: ledgerHeader("explicit-second-send"), OperationId: a.OperationId, PreviousSendRef: original.Execution.Send.Ref, Claim: claim2}
	r, err = f.h.Ledger.PrepareResend(f.ctx, f.caller, prepare2)
	accepted(t, r, err)
	second, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if err != nil || !proto.Equal(second.Send.ResendQueryObservationRef, firstRelation.ObservationRef) {
		t.Fatalf("second send query binding: %v %v", second, err)
	}
	secondStart := startFor(second, claim2)
	r, err = f.h.Egress.Invoke(f.ctx, actor, secondStart)
	accepted(t, r, err)
	secondOp, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || secondOp.Effect.Outcome != "UNKNOWN" || secondOp.Effect.LateEffect != "MAY_OCCUR" {
		t.Fatalf("lost second write: %v %v", secondOp, err)
	}
	claim3 := claimFor(3)
	denied := &v1.PrepareResendCommand{Header: ledgerHeader("third-send-old-query"), OperationId: a.OperationId, PreviousSendRef: secondOp.Execution.Send.Ref, Claim: claim3}
	r, err = f.h.Ledger.PrepareResend(f.ctx, f.caller, denied)
	if err != nil || r.GetError().GetCode() != "RESEND_QUERY_FIRST" {
		t.Fatalf("reused first query: %v %v", r, err)
	}
	unchanged, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || !proto.Equal(unchanged, secondOp) {
		t.Fatalf("rejected resend changed original: %v %v", unchanged, err)
	}
	mu.Lock()
	countBeforeQuery2 := len(requests)
	mu.Unlock()
	if countBeforeQuery2 != 3 {
		t.Fatalf("denied resend reached target: %d", countBeforeQuery2)
	}
	// 只等待公开计划给出的到期时间，由生产核对入口创建第二个独立查询。
	time.Sleep(max(0, time.Until(time.UnixMilli(plan.NextReconcileAtUnixMs))) + 20*time.Millisecond)
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	plan, err = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || plan.CheckCount != 2 || len(plan.QueryRefs) != 2 || plan.State != "WAITING" {
		t.Fatalf("second weak query: %v %v", plan, err)
	}
	secondRelation, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[1])
	if err != nil || proto.Equal(secondRelation.ObservationRef, firstRelation.ObservationRef) || proto.Equal(secondRelation.QueryOperationRef, firstRelation.QueryOperationRef) {
		t.Fatalf("query responsibility reused: %v %v", secondRelation, err)
	}
	// 旧拒绝命令的重放仍返回旧决定；新命令才可消费新证据。
	r, err = f.h.Ledger.PrepareResend(f.ctx, f.caller, denied)
	if err != nil || r.GetError().GetCode() != "RESEND_QUERY_FIRST" {
		t.Fatalf("replayed denial changed: %v %v", r, err)
	}
	prepare3 := proto.Clone(denied).(*v1.PrepareResendCommand)
	prepare3.Header = ledgerHeader("explicit-third-send-after-new-query")
	r, err = f.h.Ledger.PrepareResend(f.ctx, f.caller, prepare3)
	accepted(t, r, err)
	third, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if err != nil || third.Send.SendSeq != 3 || !proto.Equal(third.Send.ResendQueryObservationRef, secondRelation.ObservationRef) || !proto.Equal(third.Attempt.Ref.Name, original.Execution.Attempt.Ref.Name) || third.Attempt.ExternalKey != original.Execution.Attempt.ExternalKey || !proto.Equal(third.CallDescriptor, original.Execution.CallDescriptor) || len(third.PreviousSends) != 2 || !proto.Equal(third.PreviousSends[0], secondOp.Execution.PreviousSends[0]) || !proto.Equal(third.PreviousSends[1], secondOp.Execution.Send) {
		t.Fatalf("third send lost original identity/history: %v %v", third, err)
	}
	thirdStart := startFor(third, claim3)
	r, err = f.h.Egress.Invoke(f.ctx, actor, thirdStart)
	accepted(t, r, err)
	after, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || after.Effect.Outcome != "APPLIED" || after.Effect.LateEffect != "RULED_OUT" || after.Effect.EvidenceConflict {
		t.Fatalf("third terminal write: %v %v", after, err)
	}
	var querySends []*v1.PhysicalSend
	for _, ref := range plan.QueryRefs {
		relation, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, ref)
		if e != nil {
			t.Fatal(e)
		}
		query, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
		if e != nil || query.Lifecycle != "SETTLED" || !proto.Equal(query.QuerySubject.OperationId, a.OperationId) || !proto.Equal(query.QuerySubject.AttemptId, original.Execution.Attempt.Ref.Name) || query.QuerySubject.ExternalKey != original.Execution.Attempt.ExternalKey {
			t.Fatalf("separate original subject: %v %v", query, e)
		}
		querySends = append(querySends, query.Execution.Send)
	}
	sources := append([]*v1.PhysicalSend{after.Execution.PreviousSends[0], after.Execution.PreviousSends[1], after.Execution.Send}, querySends...)
	seen := map[string]bool{}
	for i, send := range sources {
		source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, send.Ref)
		if e != nil || source == nil || seen[source.Ref.Name.LocalId] {
			t.Fatalf("merged/missing fee source: %v %v", source, e)
		}
		seen[source.Ref.Name.LocalId] = true
		if i < 2 {
			if source.Amount != nil {
				t.Fatalf("lost bill became known: %v", source)
			}
		} else {
			want := int64(2)
			if i == 2 {
				want = 7
			}
			if source.Amount == nil || *source.Amount != want || source.Status != "SETTLED" || source.Identity.NativeInstance != send.Ref.Name.LocalId {
				t.Fatalf("wrong independent fee: %v", source)
			}
		}
	}
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || budget.Reserved != 60 || budget.Settled != 11 {
		t.Fatalf("two lost bills must retain holds: %v %v", budget, err)
	}
	for _, c := range []*v1.PrepareResendCommand{prepare2, prepare3} {
		r, err = f.h.Ledger.PrepareResend(f.ctx, f.caller, c)
		accepted(t, r, err)
	}
	for _, c := range []*v1.StartExecutionCommand{first, secondStart, thirdStart} {
		r, err = f.h.Egress.Invoke(f.ctx, actor, c)
		accepted(t, r, err)
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
	restored, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || !proto.Equal(restored, after) {
		t.Fatalf("restart or replay changed original: %v %v", restored, err)
	}
	restoredBudget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || !proto.Equal(restoredBudget, budget) {
		t.Fatalf("restart changed fees: %v %v", restoredBudget, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 5 || len(effects) != 1 || len(bills) != 5 {
		t.Fatalf("actual requests/effects/bills: %v %d %d", requests, len(effects), len(bills))
	}
	for i, method := range []string{"POST", "GET", "POST", "GET", "POST"} {
		got := requests[i]
		if got.method != method {
			t.Fatalf("actual request order: %v", requests)
		}
		if method == "POST" {
			if got.operation != a.OperationId.LocalId || got.attempt != original.Execution.Attempt.Ref.Name.LocalId || got.key != original.Execution.Attempt.ExternalKey || got.send != sources[i/2].Ref.Name.LocalId {
				t.Fatalf("actual write identity: %v", got)
			}
		} else {
			query, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, []*v1.Ref{firstRelation.QueryOperationRef, secondRelation.QueryOperationRef}[i/2].Name)
			if e != nil || got.operation != query.Ref.Name.LocalId || got.attempt != query.Execution.Attempt.Ref.Name.LocalId || got.key != query.Execution.Attempt.ExternalKey || got.send != query.Execution.Send.Ref.Name.LocalId || got.subjectOperation != a.OperationId.LocalId || got.subjectAttempt != original.Execution.Attempt.Ref.Name.LocalId || got.subjectKey != original.Execution.Attempt.ExternalKey {
				t.Fatalf("actual query identity: %v %v", got, e)
			}
		}
	}
}
