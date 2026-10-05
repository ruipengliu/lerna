//go:build fault

package fault_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/defaults/scripted"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G2、G3、G11、完成-7
func TestCompletionSupersessionAndInputShareOneCommit(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "supersession.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			seedConfigGoal(t, h)
			ctx := context.Background()
			caller := &v1.Caller{UserId: "u", IssuerId: "host"}
			r, e := applyConfig(t, h, ctx, "tasks.planning")
			requireAccepted(t, r, e)
			goal, e := h.Durable.QueryReceipt(ctx, caller, admissionHeader("goal").Identity)
			if e != nil {
				t.Fatal(e)
			}
			snap, e := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: admissionHeader("supersede-request"), TaskId: goal.Receipt.TaskRef.Name})
			if e != nil {
				t.Fatal(e)
			}
			proposal, e := (&scripted.Reasoner{CompletionEvidence: []*v1.CompletionEvidence{}}).Propose(ctx, snap)
			if e != nil {
				t.Fatal(e)
			}
			r, e = h.Tasks.ReceiveProposal(ctx, caller, &v1.ReceiveProposalCommand{Header: admissionHeader("supersede-proposal"), Proposal: proposal})
			requireAccepted(t, r, e)
			r, e = h.Tasks.BeginCompletion(ctx, caller, &v1.BeginCompletionCommand{Header: admissionHeader("supersede-begin"), TaskId: goal.Receipt.TaskRef.Name, ProposalRef: r.ResultRef})
			requireAccepted(t, r, e)
			input := &v1.SubmitInputCommand{Header: admissionHeader("atomic-supersede-input"), SessionId: goal.Receipt.SessionRef.Name, TaskId: goal.Receipt.TaskRef.Name, InputKind: "MODIFY", ContentRef: goal.Receipt.InputRef, ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1}
			b, e := proto.Marshal(input)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path+".input", b, 0600); e != nil {
				t.Fatal(e)
			}
			h.Close()
			child := exec.Command(os.Args[0], "-test.run=^TestCompletionSupersessionChild$")
			child.Env = append(os.Environ(), "LERNA_SUPERSESSION_DB="+path, "LERNA_SUPERSESSION_MODE="+string(mode))
			out, e := child.CombinedOutput()
			if mode == sqlite.LoseReceipt {
				if e != nil {
					t.Fatalf("child %v %s", e, out)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
					t.Fatalf("fault not reached %v %s", e, out)
				}
			}
			h, e = assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			task, e := h.Tasks.QueryTask(ctx, caller, input.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			planning, e := h.Tasks.QueryPlanning(ctx, caller, input.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			v, e := h.Tasks.QueryVerification(ctx, caller, planning.VerificationRef)
			if e != nil {
				t.Fatal(e)
			}
			if mode == sqlite.CrashBeforeCommit {
				if task.InputVersion != 1 || planning.VerificationFreeze != 1 || v.Status != "VERIFYING" {
					t.Fatalf("partial supersession %v %v %v", task, planning, v)
				}
			} else {
				if task.InputVersion != 2 || planning.VerificationFreeze != 0 || v.Status != "SUPERSEDED" {
					t.Fatalf("committed supersession lost %v %v %v", task, planning, v)
				}
			}
			r, e = h.Sessions.SubmitInput(ctx, caller, input)
			requireAccepted(t, r, e)
			again, e := h.Sessions.SubmitInput(ctx, caller, input)
			requireAccepted(t, again, e)
			if !proto.Equal(r, again) {
				t.Fatal("replayed input changed original receipt")
			}
			planning, e = h.Tasks.QueryPlanning(ctx, caller, input.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			v, e = h.Tasks.QueryVerification(ctx, caller, planning.VerificationRef)
			if e != nil || v.Status != "SUPERSEDED" || planning.VerificationFreeze != 0 {
				t.Fatalf("no lawful continuation %v %v", v, e)
			}
		})
	}
}

// 规则：G3、G11
func TestCompletionSupersessionChild(t *testing.T) {
	path := os.Getenv("LERNA_SUPERSESSION_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	b, e := os.ReadFile(path + ".input")
	if e != nil {
		t.Fatal(e)
	}
	c := new(v1.SubmitInputCommand)
	if e = proto.Unmarshal(b, c); e != nil {
		t.Fatal(e)
	}
	ctx, e := sqlite.WithFault(context.Background(), "sessions.input", sqlite.FaultMode(os.Getenv("LERNA_SUPERSESSION_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	_, e = h.Sessions.SubmitInput(ctx, &v1.Caller{UserId: "u", IssuerId: "host"}, c)
	if e == nil {
		t.Fatal("fault not reached")
	}
}
