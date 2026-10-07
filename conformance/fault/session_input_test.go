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
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G11
func TestSessionInputAndProcessingCommitBoundaries(t *testing.T) {
	for _, point := range []string{"sessions.input", "tasks.input"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "input.db")
				h, e := assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				seedConfigGoal(t, h)
				r, e := applyConfig(t, h, context.Background(), "tasks.planning")
				requireAccepted(t, r, e)
				caller := &v1.Caller{UserId: "u", IssuerId: "host"}
				q, e := h.Durable.QueryReceipt(context.Background(), caller, admissionHeader("goal").Identity)
				if e != nil {
					t.Fatal(e)
				}
				input := &v1.SubmitInputCommand{Header: admissionHeader("input-commit"), SessionId: q.Receipt.SessionRef.Name, TaskId: q.Receipt.TaskRef.Name, InputKind: "MODIFY", ContentRef: q.Receipt.InputRef, ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1}
				var cmd proto.Message = input
				if point == "tasks.input" {
					input.Header = admissionHeader("seed-change")
					r, e = h.Sessions.SubmitInput(context.Background(), caller, input)
					requireAccepted(t, r, e)
					task, e := h.Tasks.QueryTask(context.Background(), caller, q.Receipt.TaskRef.Name)
					if e != nil {
						t.Fatal(e)
					}
					cmd = &v1.ProcessInputCommand{Header: admissionHeader("input-commit"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: 2, Outcome: "UNCHANGED", Source: "TRUSTED_TEMPLATE"}
				}
				payload, e := protojson.Marshal(cmd)
				if e != nil {
					t.Fatal(e)
				}
				commandFile := path + ".json"
				if e = os.WriteFile(commandFile, payload, 0600); e != nil {
					t.Fatal(e)
				}
				h.Close()
				if mode == sqlite.LoseReceipt {
					h, e = assembly.Open(path, "u", "d")
					if e != nil {
						t.Fatal(e)
					}
					fault, e := sqlite.WithFault(context.Background(), point, mode)
					if e != nil {
						t.Fatal(e)
					}
					r, e = applySessionInput(t, h, fault, point, commandFile)
					if e == nil || r != nil {
						t.Fatalf("lost receipt leaked: %v %v", r, e)
					}
					h.Close()
				} else {
					child := exec.Command(os.Args[0], "-test.run=^TestSessionInputCrashChild$")
					child.Env = append(os.Environ(), "LERNA_INPUT_DB="+path, "LERNA_INPUT_POINT="+point, "LERNA_INPUT_MODE="+string(mode), "LERNA_INPUT_COMMAND="+commandFile)
					out, e := child.CombinedOutput()
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("fault not reached: %v %s", e, out)
					}
				}
				h, e = assembly.Open(path, "u", "d")
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				receipt, e := h.Durable.QueryReceipt(context.Background(), caller, admissionHeader("input-commit").Identity)
				if e != nil {
					t.Fatal(e)
				}
				task, e := h.Tasks.QueryTask(context.Background(), caller, q.Receipt.TaskRef.Name)
				if e != nil {
					t.Fatal(e)
				}
				session, e := h.Sessions.QuerySession(context.Background(), caller, q.Receipt.SessionRef.Name)
				if e != nil {
					t.Fatal(e)
				}
				if mode == sqlite.CrashBeforeCommit {
					if receipt.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
						t.Fatal("uncommitted receipt survived")
					}
					if point == "sessions.input" && (task.InputVersion != 1 || len(session.Inputs) != 1) {
						t.Fatal("partial input survived")
					}
					if point == "tasks.input" && task.BoundInputVersion != 1 {
						t.Fatal("partial processing survived")
					}
				} else if receipt.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
					t.Fatal("committed receipt lost")
				}
				r, e = applySessionInput(t, h, context.Background(), point, commandFile)
				requireAccepted(t, r, e)
				if mode != sqlite.CrashBeforeCommit && !proto.Equal(r, receipt.Receipt) {
					t.Fatal("replay changed original receipt")
				}
				r2, e := applySessionInput(t, h, context.Background(), point, commandFile)
				requireAccepted(t, r2, e)
				if !proto.Equal(r, r2) {
					t.Fatal("second replay changed receipt")
				}
				task, e = h.Tasks.QueryTask(context.Background(), caller, q.Receipt.TaskRef.Name)
				if e != nil {
					t.Fatal(e)
				}
				session, e = h.Sessions.QuerySession(context.Background(), caller, q.Receipt.SessionRef.Name)
				if e != nil {
					t.Fatal(e)
				}
				history, e := h.Tasks.QueryInputs(context.Background(), caller, task.TaskId)
				if e != nil {
					t.Fatal(e)
				}
				if task.InputVersion != 2 || len(session.Inputs) != 2 || session.LastCommittedSeq != 2 || len(history.Inputs) != 2 {
					t.Fatalf("duplicate/lost input: %v %v %v", task, session, history)
				}
				if point == "tasks.input" && (task.BoundInputVersion != 2 || history.Inputs[1].ProcessingStatus != "PROCESSED") {
					t.Fatal("processing evidence lost")
				}
			})
		}
	}
}

// 规则：G2、G3、G4、G11、R7
func TestInitialGoalCreationCommitBoundaries(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			kind := "draft"
			if explicit {
				kind = "explicit"
			}
			t.Run(kind+"/"+string(mode), func(t *testing.T) {
				ctx := context.Background()
				caller := &v1.Caller{UserId: "u", IssuerId: "host"}
				path := filepath.Join(t.TempDir(), "creation.db")
				h, err := assembly.Open(path, "u", "d")
				if err != nil {
					t.Fatal(err)
				}
				r, err := h.Sessions.CreateSession(ctx, caller, &v1.CreateSessionCommand{Header: admissionHeader("creation-session")})
				requireAccepted(t, r, err)
				id := r.ResultRef.Name
				contentRef, err := h.Content.Stage(ctx, caller, &v1.SubmitGoalCommand{Identity: admissionHeader("creation-body").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "original initial goal"})
				if err != nil {
					t.Fatal(err)
				}
				c := &v1.SubmitInputCommand{Header: admissionHeader("initial-creation"), SessionId: id, InputKind: "GOAL", ContentRef: contentRef}
				if explicit {
					c.ExplicitConditions = []*v1.Requirement{{ConditionId: "original-condition", DescriptionRef: contentRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}
				}
				body, err := protojson.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}
				commandFile := path + ".json"
				if err = os.WriteFile(commandFile, body, 0600); err != nil {
					t.Fatal(err)
				}
				h.Close()
				if mode == sqlite.LoseReceipt {
					h, err = assembly.Open(path, "u", "d")
					if err != nil {
						t.Fatal(err)
					}
					fault, err := sqlite.WithFault(ctx, "sessions.input", mode)
					if err != nil {
						t.Fatal(err)
					}
					r, err = h.Sessions.SubmitInput(fault, caller, c)
					if err == nil || r != nil {
						t.Fatalf("lost creation receipt: %v %v", r, err)
					}
					h.Close()
				} else {
					child := exec.Command(os.Args[0], "-test.run=^TestSessionInputCrashChild$")
					child.Env = append(os.Environ(), "LERNA_INPUT_DB="+path, "LERNA_INPUT_POINT=sessions.input", "LERNA_INPUT_MODE="+string(mode), "LERNA_INPUT_COMMAND="+commandFile)
					out, err := child.CombinedOutput()
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("creation fault not reached: %v %s", err, out)
					}
				}
				h, err = assembly.Open(path, "u", "d")
				if err != nil {
					t.Fatal(err)
				}
				defer h.Close()
				q, err := h.Durable.QueryReceipt(ctx, caller, c.Header.Identity)
				if err != nil {
					t.Fatal(err)
				}
				before, err := h.Sessions.QuerySession(ctx, caller, id)
				if err != nil {
					t.Fatal(err)
				}
				if mode == sqlite.CrashBeforeCommit {
					if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND || before.LastCommittedSeq != 0 || len(before.TaskRefs) != 0 || len(before.Inputs) != 0 {
						t.Fatalf("partial creation: %v %v", q, before)
					}
					sources, err := h.Trace.QuerySources(ctx, caller)
					if err != nil {
						t.Fatal(err)
					}
					for _, source := range sources {
						if source.Command.Event.TaskId != nil {
							t.Fatalf("partial task source: %v", source)
						}
					}
				} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
					t.Fatalf("committed creation lost: %v", q)
				}
				r, err = h.Sessions.SubmitInput(ctx, caller, c)
				requireAccepted(t, r, err)
				if mode != sqlite.CrashBeforeCommit && !proto.Equal(r, q.Receipt) {
					t.Fatal("original creation receipt changed")
				}
				first := r
				r, err = h.Sessions.SubmitInput(ctx, caller, c)
				if err != nil || !proto.Equal(first, r) {
					t.Fatalf("creation replay: %v %v", r, err)
				}
				saved, err := h.Sessions.QuerySession(ctx, caller, id)
				if err != nil || len(saved.TaskRefs) != 1 || len(saved.Inputs) != 1 || saved.LastCommittedSeq != 1 {
					t.Fatalf("one original creation: %v %v", saved, err)
				}
				task, err := h.Tasks.QueryTask(ctx, caller, saved.TaskRefs[0].Name)
				if err != nil || task.InputVersion != 1 || !proto.Equal(task.GoalRef, contentRef) {
					t.Fatalf("original created task: %v %v", task, err)
				}
				history, err := h.Tasks.QueryInputs(ctx, caller, task.TaskId)
				if err != nil || len(history.Inputs) != 1 || history.Inputs[0].TaskInputSeq != 1 || !proto.Equal(history.Inputs[0].InputRef, first.ResultRef) {
					t.Fatalf("original initial history: %v %v", history, err)
				}
				planning, err := h.Tasks.QueryPlanning(ctx, caller, task.TaskId)
				if err != nil {
					t.Fatal(err)
				}
				if explicit {
					if task.BoundInputVersion != 1 || history.Inputs[0].ProcessingStatus != "PROCESSED" || planning.Requirements.GetSource() != "USER_EXPLICIT" || !proto.Equal(planning.Requirements.AcceptedBy, c.Header.Identity) || !proto.Equal(planning.Requirements.SourceInputRef, first.ResultRef) {
						t.Fatalf("explicit creation lost: %v %v %v", task, history, planning)
					}
				} else if task.BoundInputVersion != 0 || task.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_DRAFT || history.Inputs[0].ProcessingStatus != "ACCEPTED" || planning.Requirements != nil {
					t.Fatalf("draft creation changed: %v %v %v", task, history, planning)
				}
			})
		}
	}
}

// 规则：G3
func TestSessionInputCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_INPUT_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_INPUT_POINT"), sqlite.FaultMode(os.Getenv("LERNA_INPUT_MODE")))
	if e != nil {
		t.Fatal(e)
	}
	r, e := applySessionInput(t, h, ctx, os.Getenv("LERNA_INPUT_POINT"), os.Getenv("LERNA_INPUT_COMMAND"))
	t.Fatalf("fault not reached: %v %v", r, e)
}
func applySessionInput(t *testing.T, h *assembly.Harness, ctx context.Context, point, path string) (*v1.CommandReceipt, error) {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	if point == "sessions.input" {
		c := new(v1.SubmitInputCommand)
		if e = protojson.Unmarshal(b, c); e != nil {
			t.Fatal(e)
		}
		return h.Sessions.SubmitInput(ctx, caller, c)
	}
	c := new(v1.ProcessInputCommand)
	if e = protojson.Unmarshal(b, c); e != nil {
		t.Fatal(e)
	}
	return h.Tasks.ProcessInput(ctx, caller, c)
}
