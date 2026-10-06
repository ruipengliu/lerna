//go:build fault && darwin && cgo

package fault_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G5、G10、G11、R7、开始-4、开始-5
func TestAPI429DueWakeCrashBoundariesPreserveOriginalWaitAndFees(t *testing.T) {
	for _, category := range []string{"RATE", "CONCURRENCY", "RESOURCE_CONFLICT"} {
		for _, point := range []string{"durable.jobs", "ledger.resend", "tasks.start", "ledger.dispatch", "content.observation", "body.accept", "content.publish", "ledger.observation", "content.observation_ack", "budget.usage", "trace.accept", "ledger.usage_ack", "ledger.trace_ack", "ledger.interpret", "trace.source_ack", "trace.index"} {
			for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
				t.Run(category+"/"+point+"/"+string(mode), func(t *testing.T) {
					var calls, effects atomic.Int64
					var mu sync.Mutex
					var identities []string
					target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						n := calls.Add(1)
						mu.Lock()
						identities = append(identities, r.Header.Get("Idempotency-Key")+"/"+r.Header.Get("Lerna-Attempt"))
						mu.Unlock()
						if n == 1 {
							w.Header().Set("Lerna-Rate-Category", category)
							w.Header().Set("Retry-After", "0")
							w.WriteHeader(429)
							return
						}
						effects.Add(1)
						_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": true, "terminal": true})
					}))
					defer target.Close()
					path := filepath.Join(t.TempDir(), "due-api.db")
					h, keychain, a, first := prepareAPIStartFault(t, path, target.URL)
					ctx, actor := context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}
					r, err := h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, first)
					requireAccepted(t, r, err)
					before, err := h.Ledger.QueryOperation(ctx, actor, a.OperationId)
					if err != nil || before.ApiWait == nil || before.ApiWait.Category != category || calls.Load() != 1 || effects.Load() != 0 {
						t.Fatal("initial classified wait missing", err)
					}
					if err = h.Trace.Recover(ctx, actor); err != nil {
						t.Fatal(err)
					}
					writeStart(t, path, first)
					if err = h.Close(); err != nil {
						t.Fatal(err)
					}
					time.Sleep(max(0, time.Until(time.UnixMilli(before.ApiWait.ReadyAtUnixMs))) + 20*time.Millisecond)
					child := exec.Command(os.Args[0], "-test.run=^TestAPI429DueWakeCrashChild$")
					child.Env = append(os.Environ(), "LERNA_API_DUE_DB="+path, "LERNA_API_DUE_POINT="+point, "LERNA_API_DUE_MODE="+string(mode), "LERNA_API_DUE_KEYCHAIN="+keychain.Path())
					out, err := child.CombinedOutput()
					if mode == sqlite.LoseReceipt {
						if err != nil {
							t.Fatalf("child%v %s", err, out)
						}
					} else {
						var exit *exec.ExitError
						if !errors.As(err, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
							t.Fatalf("fault not reached%v %s", err, out)
						}
					}
					h, err = assembly.OpenWithOptions(path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
					if err != nil {
						t.Fatal(err)
					}
					defer h.Close()
					claim, err := h.LedgerWork.ExecuteJob(ctx, actor, apiDueClaim())
					requireAccepted(t, claim, err)
					if len(claim.Jobs) != 1 {
						t.Fatal("original due claim lost")
					}
					resend := &v1.PrepareResendCommand{Header: executionHeader("api-due-resend"), OperationId: a.OperationId, PreviousSendRef: before.Execution.Send.Ref, Claim: claim.Jobs[0]}
					r, err = h.Ledger.PrepareResend(ctx, actor, resend)
					requireAccepted(t, r, err)
					second := faultResendStart(t, h, first, resend)
					r, err = h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
					requireAccepted(t, r, err)
					after, err := h.Ledger.QueryOperation(ctx, actor, a.OperationId)
					if err != nil || after.Execution.Send.SendSeq != 2 || len(after.Execution.PreviousSends) != 1 || !proto.Equal(after.Execution.Attempt.Ref.Name, before.Execution.Attempt.Ref.Name) || after.Execution.Attempt.ExternalKey != before.Execution.Attempt.ExternalKey || !proto.Equal(after.Execution.CallDescriptor, before.Execution.CallDescriptor) || !proto.Equal(after.Execution.PreviousSends[0].ApiWait, before.ApiWait) {
						t.Fatal("wake changed original attempt/key/wait", err)
					}
					wantCalls, wantEffects := int64(2), int64(1)
					lost := point == "content.observation" && mode == sqlite.CrashBeforeCommit
					if point == "ledger.dispatch" && mode != sqlite.CrashBeforeCommit {
						wantCalls, wantEffects, lost = 1, 0, true
					}
					if calls.Load() != wantCalls || effects.Load() != wantEffects {
						t.Fatal("recovery duplicated actual request/effect")
					}
					mu.Lock()
					for _, identity := range identities {
						if identity != before.Execution.Attempt.ExternalKey+"/"+before.Execution.Attempt.Ref.Name.LocalId {
							t.Error("target received changed identity")
						}
					}
					mu.Unlock()
					if lost {
						if after.Effect.Outcome != "UNKNOWN" || after.Effect.LateEffect != "MAY_OCCUR" {
							t.Fatal("missing evidence became terminal")
						}
					} else if after.Effect.Outcome != "APPLIED" || after.Effect.LateEffect != "RULED_OUT" {
						t.Fatal("actual terminal evidence lost")
					}
					historical, err := h.Ledger.QueryOperationVersion(ctx, actor, before.Ref)
					if err != nil || !proto.Equal(historical, before) {
						t.Fatal("wake rewrote historical responsibility", err)
					}
					budget, err := h.Budget.QueryBudget(ctx, actor, a.TaskId)
					if err != nil || budget.Reserved != 60 {
						t.Fatal("wake removed independent original fee", err)
					}
					for _, send := range []*v1.PhysicalSend{after.Execution.Send, after.Execution.PreviousSends[0]} {
						bill, err := h.Budget.QueryBillingSource(ctx, actor, send.Ref)
						if err != nil || bill == nil || bill.Status != "PENDING" || bill.Amount != nil {
							t.Fatal("wake lost pending fee source", err)
						}
					}
					assertAPIRecoveredSources(t, h, after, 1, keychain.Path(), target.URL, "synthetic-api-fault-secret-f17e4038")
					assertRecoveredResendSource(t, h, resend)
					r, err = h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
					requireAccepted(t, r, err)
					if calls.Load() != wantCalls || effects.Load() != wantEffects {
						t.Fatal("wake replay repeated target request")
					}
				})
			}
		}
	}
}

func apiDueClaim() *v1.JobCommand {
	return &v1.JobCommand{Identity: executionHeader("api-due-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "api-due-worker"}
}

// 规则：G1、G3、G4、G5、G10、G11、R7
func TestAPI429DueWakeCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_API_DUE_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, err := assembly.OpenWithOptions(path, "u", "d", assembly.Options{APIKeychainPath: os.Getenv("LERNA_API_DUE_KEYCHAIN")})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ctx, err := sqlite.WithFault(context.Background(), os.Getenv("LERNA_API_DUE_POINT"), sqlite.FaultMode(os.Getenv("LERNA_API_DUE_MODE")))
	if err != nil {
		t.Fatal(err)
	}
	actor := &v1.Caller{UserId: "u", IssuerId: "host"}
	first := readStart(t, path)
	claim, err := h.LedgerWork.ExecuteJob(ctx, actor, apiDueClaim())
	if err == nil {
		requireAccepted(t, claim, err)
		op, queryErr := h.Ledger.QueryOperation(ctx, actor, first.Binding.OperationId)
		if queryErr != nil || len(claim.Jobs) != 1 {
			t.Fatal("due claim missing", queryErr)
		}
		resend := &v1.PrepareResendCommand{Header: executionHeader("api-due-resend"), OperationId: op.Ref.Name, PreviousSendRef: op.Execution.Send.Ref, Claim: claim.Jobs[0]}
		r, prepareErr := h.Ledger.PrepareResend(ctx, actor, resend)
		err = prepareErr
		if err == nil {
			requireAccepted(t, r, err)
			second := faultResendStart(t, h, first, resend)
			_, err = h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
		}
	}
	if err == nil {
		err = h.Trace.Recover(ctx, actor)
	}
	if err == nil {
		t.Fatal("fault not reached")
	}
}
