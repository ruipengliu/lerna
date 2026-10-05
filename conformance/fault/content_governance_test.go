//go:build fault

package fault_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

func contentHeader(id string) *v1.CommandHeader {
	return &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "host", TargetDomainId: "local/content", CommandId: id}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
}
func contentOriginal() *v1.RegisterContentCommand {
	return &v1.RegisterContentCommand{Header: contentHeader("original"), Body: []byte{0, 255, 65}, MediaType: "application/octet-stream", SourceDescriptor: &v1.ContentSourceDescriptor{Kind: "HOST_IMPORT", Locator: "fixture:original", AcquisitionMethod: "LOCAL_IMPORT", ProviderVersion: "v1"}}
}

// 规则：G3、G11、R7
func TestContentHolderReceiptLossKeepsStagingInvisible(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "content.db")
	h, e := assembly.Open(path, "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	r, e := h.Content.Register(ctx, actor, contentOriginal())
	requireAccepted(t, r, e)
	fault, e := sqlite.WithFault(ctx, "body.accept", sqlite.LoseReceipt)
	if e != nil {
		t.Fatal(e)
	}
	e = h.Content.ProcessRegistrations(fault, actor)
	var failure *command.Failure
	if !errors.As(e, &failure) || failure.Detail.CommandAcceptance != v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN {
		t.Fatalf("loss %v", e)
	}
	staged, e := h.Content.QueryRegistration(ctx, actor, r.ResultRef)
	if e != nil || staged.State != "STAGED" || staged.BodyReceipt != nil {
		t.Fatalf("source acknowledged prematurely %v %v", staged, e)
	}
	held, e := h.Bodies.QueryReceipt(ctx, actor, staged.Deposit)
	if e != nil || held == nil || held.Deposit.ByteSize != 3 || held.DurabilityProfile != "LOCAL" {
		t.Fatalf("holder lost accepted bytes %v %v", held, e)
	}
	if _, e = h.Content.Read(ctx, actor, r.ResultRef); e == nil || e.Error() != "CONTENT_UNUSABLE" {
		t.Fatalf("staged read %v", e)
	}
	if e = h.Content.CheckUsable(ctx, actor, r.ResultRef); e == nil {
		t.Fatal("staged input usable")
	}
	h.Close()
	h, e = assembly.Open(path, "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	published, e := h.Content.QueryRegistration(ctx, actor, r.ResultRef)
	if e != nil || published.State != "PUBLISHED" || !proto.Equal(published.BodyReceipt, held) {
		t.Fatalf("publication %v %v", published, e)
	}
	same, e := h.Bodies.QueryReceipt(ctx, actor, published.Deposit)
	if e != nil || !proto.Equal(same, held) {
		t.Fatalf("holder was overwritten %v %v", same, e)
	}
	body, e := h.Content.Read(ctx, actor, r.ResultRef)
	if e != nil || !bytes.Equal(command.ContentBytes(body), []byte{0, 255, 65}) {
		t.Fatalf("body %v %v", body, e)
	}
	again, e := h.Content.Register(ctx, actor, contentOriginal())
	requireAccepted(t, again, e)
	if !proto.Equal(again, r) {
		t.Fatal("source receipt changed")
	}
}

// 规则：G3、G11、R7、V4
func TestContentRegistrationCrashMatrix(t *testing.T) {
	for _, point := range []string{"content.register", "body.accept", "content.publish"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "content.db")
				h, e := assembly.Open(path, "alice", "local")
				if e != nil {
					t.Fatal(e)
				}
				h.Close()
				child := exec.Command(os.Args[0], "-test.run=^TestContentRegistrationChild$")
				child.Env = append(os.Environ(), "LERNA_CONTENT_DB="+path, "LERNA_CONTENT_POINT="+point, "LERNA_CONTENT_MODE="+string(mode))
				out, e := child.CombinedOutput()
				if mode == sqlite.LoseReceipt {
					if e != nil {
						t.Fatalf("child %v %s", e, out)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("crash %v %s", e, out)
					}
				}
				h, e = assembly.Open(path, "alice", "local")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				ctx := context.Background()
				actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
				q, e := h.Content.QueryReceipt(ctx, actor, contentOriginal().Header.Identity)
				if e != nil {
					t.Fatal(e)
				}
				if point == "content.register" && mode == sqlite.CrashBeforeCommit {
					if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
						t.Fatalf("uncommitted source: %v", q)
					}
				} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
					t.Fatalf("source lost: %v", q)
				}
				r, e := h.Content.Register(ctx, actor, contentOriginal())
				requireAccepted(t, r, e)
				if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
					t.Fatal(e)
				}
				reg, e := h.Content.QueryRegistration(ctx, actor, r.ResultRef)
				if e != nil || reg.State != "PUBLISHED" {
					t.Fatalf("not published %v %v", reg, e)
				}
				held, e := h.Bodies.QueryReceipt(ctx, actor, reg.Deposit)
				if e != nil || !proto.Equal(held, reg.BodyReceipt) {
					t.Fatalf("receipt mismatch %v %v", held, e)
				}
				body, e := h.Content.Read(ctx, actor, r.ResultRef)
				if e != nil || !bytes.Equal(command.ContentBytes(body), []byte{0, 255, 65}) {
					t.Fatalf("body %v %v", body, e)
				}
				repeat, e := h.Content.Register(ctx, actor, contentOriginal())
				requireAccepted(t, repeat, e)
				if !proto.Equal(repeat, r) {
					t.Fatal("duplicate source")
				}
			})
		}
	}
}

// 规则：G3、V4
func TestContentRegistrationChild(t *testing.T) {
	path := os.Getenv("LERNA_CONTENT_DB")
	if path == "" {
		t.Skip("child only")
	}
	h, e := assembly.Open(path, "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_CONTENT_POINT"), sqlite.FaultMode(os.Getenv("LERNA_CONTENT_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	_, e = h.Content.Register(ctx, actor, contentOriginal())
	if e == nil {
		e = h.Content.ProcessRegistrations(ctx, actor)
	}
	if e == nil {
		t.Fatal("fault did not fire")
	}
}

func contentDerivationTask(t *testing.T, h *assembly.Harness) *v1.GlobalName {
	t.Helper()
	ctx := context.Background()
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	c := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "host", TargetDomainId: "local", CommandId: "derive-task"}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.SubmitGoal", Goal: "source"}
	if _, e := h.Sessions.SubmitGoal(ctx, actor, c); e != nil {
		t.Fatal(e)
	}
	if e := h.Sessions.ProcessPending(ctx, actor); e != nil {
		t.Fatal(e)
	}
	q, e := h.Durable.QueryReceipt(ctx, actor, c.Identity)
	if e != nil {
		t.Fatal(e)
	}
	return q.Receipt.TaskRef.Name
}
func runContentDerivation(ctx context.Context, h *assembly.Harness, task *v1.GlobalName) (*v1.Ref, error) {
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	r, e := h.Content.PrepareDerivation(ctx, actor, &v1.PrepareDerivationCommand{Header: contentHeader("prepare"), TaskId: task, GeneratorVersion: "fault-v1", OutputKind: "CONTEXT", MediaType: "text/plain"})
	if e != nil {
		return nil, e
	}
	if r.Decision != v1.Decision_DECISION_ACCEPTED {
		return nil, &command.Failure{Detail: r.Error}
	}
	d, e := h.Content.QueryDerivation(ctx, actor, r.ResultRef)
	if e != nil {
		return nil, e
	}
	if d.State == "COMMITTED" {
		return d.OutputRef, nil
	}
	if d.State == "STAGED" {
		return d.OutputRef, h.Content.ProcessRegistrations(ctx, actor)
	}
	original, e := h.Tasks.QueryTask(ctx, actor, task)
	if e != nil {
		return nil, e
	}
	if d.Generation == 1 {
		_, e = h.Content.ReadDerivationInput(ctx, actor, &v1.ReadDerivationInputCommand{Header: contentHeader("input"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, InputRef: original.GoalRef})
		if e != nil {
			return nil, e
		}
		r, e = h.Content.TakeoverDerivation(ctx, actor, &v1.TakeoverDerivationCommand{Header: contentHeader("takeover"), DerivationRef: d.Ref, ExpectedGeneration: 1})
		if e != nil {
			return nil, e
		}
		if r.Decision != v1.Decision_DECISION_ACCEPTED {
			return nil, &command.Failure{Detail: r.Error}
		}
		d, e = h.Content.QueryDerivation(ctx, actor, d.Ref)
		if e != nil {
			return nil, e
		}
	}
	if d.State != "SEALED" {
		r, e = h.Content.SealDerivation(ctx, actor, &v1.SealDerivationCommand{Header: contentHeader("seal"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, InputRefs: []*v1.Ref{original.GoalRef}})
		if e != nil {
			return nil, e
		}
		if r.Decision != v1.Decision_DECISION_ACCEPTED {
			return nil, &command.Failure{Detail: r.Error}
		}
	}
	r, e = h.Content.CommitDerivation(ctx, actor, &v1.CommitDerivationCommand{Header: contentHeader("commit"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, Body: []byte("derived source")})
	if e != nil {
		return nil, e
	}
	if r.Decision != v1.Decision_DECISION_ACCEPTED {
		return nil, &command.Failure{Detail: r.Error}
	}
	return r.ResultRef, h.Content.ProcessRegistrations(ctx, actor)
}

// 规则：G3、G11、R7、V4
func TestContentDerivationCrashMatrix(t *testing.T) {
	for _, point := range []string{"content.derivation", "content.derivation_input", "content.derivation_takeover", "content.derivation_seal", "content.derivation_commit", "body.accept", "content.publish"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "derived.db")
				h, e := assembly.Open(path, "alice", "local")
				if e != nil {
					t.Fatal(e)
				}
				task := contentDerivationTask(t, h)
				h.Close()
				child := exec.Command(os.Args[0], "-test.run=^TestContentDerivationChild$")
				child.Env = append(os.Environ(), "LERNA_DERIVATION_DB="+path, "LERNA_CONTENT_POINT="+point, "LERNA_CONTENT_MODE="+string(mode))
				out, e := child.CombinedOutput()
				if mode == sqlite.LoseReceipt {
					if e != nil {
						t.Fatalf("child %v %s", e, out)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("crash %v %s", e, out)
					}
				}
				h, e = assembly.Open(path, "alice", "local")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				ctx := context.Background()
				actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
				ref, e := runContentDerivation(ctx, h, task)
				if e != nil {
					t.Fatal(e)
				}
				body, e := h.Content.Read(ctx, actor, ref)
				if e != nil || string(command.ContentBytes(body)) != "derived source" || len(body.DerivedFrom) != 1 {
					t.Fatalf("derived body %v %v", body, e)
				}
				d, e := h.Content.QueryDerivation(ctx, actor, body.ProducerRef)
				if e != nil || d.State != "COMMITTED" || d.Generation != 2 || len(d.ActualInputRefs) != 1 || !proto.Equal(d.OutputRef, ref) {
					t.Fatalf("responsibility %v %v", d, e)
				}
				reg, e := h.Content.QueryRegistration(ctx, actor, ref)
				if e != nil {
					t.Fatal(e)
				}
				held, e := h.Bodies.QueryReceipt(ctx, actor, reg.Deposit)
				if e != nil || !proto.Equal(held, reg.BodyReceipt) {
					t.Fatalf("holder receipt %v %v", held, e)
				}
				again, e := runContentDerivation(ctx, h, task)
				if e != nil || !proto.Equal(again, ref) {
					t.Fatalf("duplicate output %v %v", again, e)
				}
			})
		}
	}
}

// 规则：G3、V4
func TestContentDerivationChild(t *testing.T) {
	path := os.Getenv("LERNA_DERIVATION_DB")
	if path == "" {
		t.Skip("child only")
	}
	h, e := assembly.Open(path, "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	task := contentDerivationTask(t, h)
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_CONTENT_POINT"), sqlite.FaultMode(os.Getenv("LERNA_CONTENT_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = runContentDerivation(ctx, h, task); e == nil {
		t.Fatal("fault did not fire")
	}
}
