//go:build fault

package fault_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G5、G8、G11、R7
func TestGrantRevocationRecoversOriginalClosureAcrossEveryCommit(t *testing.T) {
	for _, sent := range []bool{false, true} {
		for _, point := range []string{"grants.revoke", "ledger.grant_closure", "grants.revocation_receipt"} {
			for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
				phase := "before-physical-send"
				if sent {
					phase = "after-physical-send"
				}
				t.Run(phase+"/"+point+"/"+string(mode), func(t *testing.T) {
					var calls atomic.Int64
					target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
					defer target.Close()
					path := filepath.Join(t.TempDir(), "revocation.db")
					h, e := assembly.Open(path, "u", "d")
					if e != nil {
						t.Fatal(e)
					}
					ctx := context.Background()
					host := &v1.Caller{UserId: "u", IssuerId: "host"}
					egress := &v1.Caller{UserId: "u", IssuerId: "egress"}
					a, start := prepareStartFault(t, h, target.URL)
					var r *v1.CommandReceipt
					if sent {
						r, e = h.Egress.Invoke(ctx, egress, start)
					} else {
						r, e = h.Tasks.StartExecution(ctx, egress, start)
					}
					requireAccepted(t, r, e)
					revoke := &v1.RevokeGrantCommand{Header: admissionHeader("revoke"), GrantId: a.GrantRefs[0].Name}
					b, e := proto.Marshal(revoke)
					if e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(path+".revoke", b, 0600); e != nil {
						t.Fatal(e)
					}
					h.Close()
					child := exec.Command(os.Args[0], "-test.run=^TestGrantRevocationCrashChild$")
					child.Env = append(os.Environ(), "LERNA_REVOCATION_DB="+path, "LERNA_REVOCATION_POINT="+point, "LERNA_REVOCATION_MODE="+string(mode))
					out, e := child.CombinedOutput()
					if mode == sqlite.LoseReceipt {
						if e != nil {
							t.Fatalf("lost receipt child: %v %s", e, out)
						}
					} else {
						var exit *exec.ExitError
						if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
							t.Fatalf("fault not hit: %v %s", e, out)
						}
					}
					h, e = assembly.Open(path, "u", "d")
					if e != nil {
						t.Fatal(e)
					}
					defer h.Close()
					original, e := h.Durable.QueryReceipt(ctx, host, revoke.Header.Identity)
					if e != nil {
						t.Fatal(e)
					}
					absent := point == "grants.revoke" && mode == sqlite.CrashBeforeCommit
					if absent {
						if original.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
							t.Fatalf("partial revocation: %v", original)
						}
					} else if original.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
						t.Fatalf("lost original revoke: %v", original)
					}
					r, e = h.Grants.Revoke(ctx, host, revoke)
					requireAccepted(t, r, e)
					if !absent && !proto.Equal(r, original.Receipt) {
						t.Fatal("revocation identity changed")
					}
					if e = h.Grants.ProcessRevocations(ctx); e != nil {
						t.Fatal(e)
					}
					pending, e := h.Grants.QueryRevocation(ctx, host, r.ResultRef)
					if e != nil || pending.Status != "PENDING" || len(pending.Closures) != 1 {
						t.Fatalf("original intent changed: %v %v", pending, e)
					}
					current, e := h.Grants.QueryCurrentRevocation(ctx, host, pending.Ref.Name)
					if e != nil || current.Status != "COMPLETE" || len(current.Closures) != 1 || current.Closures[0].RecipientReceipt == nil || !proto.Equal(current.Closures[0].Command, pending.Closures[0].Command) {
						t.Fatalf("closure responsibility lost: %v %v", current, e)
					}
					actor := &v1.Caller{UserId: "u", IssuerId: "grants-revocation"}
					closure := current.Closures[0]
					destination, e := h.Egress.QueryReceipt(ctx, actor, closure.Command.Header.Identity)
					if e != nil || !proto.Equal(destination.Receipt, closure.RecipientReceipt) {
						t.Fatalf("destination original lost: %v %v", destination, e)
					}
					ackID := &v1.CommandIdentity{UserId: "u", IssuerId: "grants-revocation", TargetDomainId: "d", CommandId: "ack:" + closure.Command.Header.Identity.CommandId}
					ack, e := h.Durable.QueryReceipt(ctx, actor, ackID)
					if e != nil || ack.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || !proto.Equal(ack.Receipt.ResultRef, current.Ref) {
						t.Fatalf("source original ack lost: %v %v", ack, e)
					}
					proof, e := h.Ledger.QueryGrantExitClosure(ctx, host, closure.RecipientReceipt.ResultRef)
					if e != nil || proof.PhysicalSendWasPossible != sent || !proto.Equal(proof.RevocationRef, pending.Ref) {
						t.Fatalf("closure proof changed: %v %v", proof, e)
					}
					op, e := h.Ledger.QueryOperation(ctx, host, a.OperationId)
					if e != nil || op.Dispatch != "SEALED" || len(op.ClosureEvidenceRefs) != 1 {
						t.Fatalf("duplicate/missing closure: %v %v", op, e)
					}
					expectedOutcome, expectedLate := "NOT_APPLIED", "RULED_OUT"
					expectedCalls := int64(0)
					if sent {
						expectedOutcome = "UNKNOWN"
						expectedLate = "MAY_OCCUR"
						expectedCalls = 1
					}
					if op.Effect.Outcome != expectedOutcome || op.Effect.LateEffect != expectedLate {
						t.Fatalf("invented effect certainty: %v", op.Effect)
					}
					if e = h.Budget.ProcessClosures(ctx); e != nil {
						t.Fatal(e)
					}
					expectedHold := int64(0)
					if sent {
						expectedHold = 30
					}
					budget, e := h.Budget.QueryBudget(ctx, host, nil)
					if e != nil || budget.Reserved != expectedHold || budget.Settled != 0 {
						t.Fatalf("budget responsibility lost: %v %v", budget, e)
					}
					release, e := h.Budget.QueryReservationRelease(ctx, host, a.BudgetBasis.ReservationRef)
					if e != nil {
						t.Fatal(e)
					}
					if sent {
						if release != nil {
							t.Fatal("released post-P5 uncertainty")
						}
					} else if release == nil || !proto.Equal(release.ClosureRef, proof.Ref) || release.Released != 30 {
						t.Fatalf("missing authoritative unused release %v", release)
					}
					historical, e := h.Budget.QueryReservation(ctx, host, a.BudgetBasis.ReservationRef)
					if e != nil || historical.Status != "RESERVED" || historical.Ceiling != 30 {
						t.Fatalf("reservation history rewritten %v %v", historical, e)
					}
					reservations, e := h.Budget.QueryReservations(ctx, host, a.TaskId)
					if e != nil || len(reservations) != 1 || reservations[0].ConsumedSends != 1 {
						t.Fatalf("original send allowance changed: %v %v", reservations, e)
					}
					if e = h.Grants.ProcessRevocations(ctx); e != nil {
						t.Fatal(e)
					}
					_, _ = h.Egress.Invoke(ctx, egress, start)
					replay, e := h.Grants.QueryCurrentRevocation(ctx, host, current.Ref.Name)
					if e != nil || !proto.Equal(replay, current) || calls.Load() != expectedCalls {
						t.Fatalf("recovery replay mutated/resends: %v %v calls %d", replay, e, calls.Load())
					}
				})
			}
		}
	}
}

// 规则：G3、G8、R7
func TestGrantRevocationCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_REVOCATION_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	b, e := os.ReadFile(path + ".revoke")
	if e != nil {
		t.Fatal(e)
	}
	c := new(v1.RevokeGrantCommand)
	if e = proto.Unmarshal(b, c); e != nil {
		t.Fatal(e)
	}
	mode := sqlite.FaultMode(os.Getenv("LERNA_REVOCATION_MODE"))
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_REVOCATION_POINT"), mode)
	if e != nil {
		t.Fatal(e)
	}
	r, e := h.Grants.Revoke(ctx, &v1.Caller{UserId: "u", IssuerId: "host"}, c)
	if e == nil {
		requireAccepted(t, r, e)
		e = h.Grants.ProcessRevocations(ctx)
	}
	if mode == sqlite.LoseReceipt && e != nil {
		return
	}
	t.Fatalf("fault not reached: %v %v", r, e)
}
