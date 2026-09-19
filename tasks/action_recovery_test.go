package tasks_test

import (
	"context"
	"encoding/json"
	sqliteauth "lerna/adapters/authorization/sqlite"
	"lerna/authorization"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type actionProbe struct {
	Path, Token, Phase string
	Ref                tasks.Ref
	Now                time.Time
}

func TestActionProcessProbe(t *testing.T) {
	path := os.Getenv("HARNESS_ACTION_PROBE")
	if path == "" {
		t.Skip("child only")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var in actionProbe
	if e = json.Unmarshal(raw, &in); e != nil {
		t.Fatal(e)
	}
	db, e := sqliteauth.Open(in.Path)
	if e != nil {
		t.Fatal(e)
	}
	auth, e := authorization.New(db, &clock{now: in.Now}, authConfig())
	if e != nil {
		t.Fatal(e)
	}
	core, e := tasks.New(auth, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	w, e := core.BindWorker(tasks.WorkerBinding{Token: in.Token, Subject: "admin", WorkerID: "action-worker", AllowEffectEvidence: true}, limits())
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	r, e := core.Load(ctx, in.Ref)
	if e != nil {
		t.Fatal(e)
	}
	p, e := w.Actions(r.Actions.Limits)
	if e != nil {
		t.Fatal(e)
	}
	d := r.Actions.Decisions[0]
	r, e = p.Admit(ctx, d.Qualification, d.Number)
	if e != nil {
		t.Fatal(e)
	}
	if in.Phase != "admitted" {
		a, e := p.Next(ctx, tasks.QualificationOf(r))
		if e != nil {
			t.Fatal(e)
		}
		if in.Phase == "fact" {
			if e = w.ConsumeExecution(ctx, tasks.ExecutionReport{OperationID: a.OperationID, Qualification: a.Qualification, Revision: 1, Phase: "FINISHED", Result: "SUCCESS", Effect: "CONFIRMED", Reference: "effect-evidence"}); e != nil {
				t.Fatal(e)
			}
		}
	}
	// Deliberately skip close/defer: the parent's fresh reads must use committed
	// SQLite/WAL records, not in-process snapshots or orderly shutdown writes.
	os.Exit(73)
}
func TestActionRecordsSurviveAbruptProcessExit(t *testing.T) {
	for _, phase := range []string{"admitted", "dispatch", "fact"} {
		t.Run(phase, func(t *testing.T) {
			s, _, p, r, back := actionRun(t)
			ctx := context.Background()
			d, e := p.Reserve(ctx, tasks.QualificationOf(r))
			if e != nil {
				t.Fatal(e)
			}
			in := tasks.DecisionRecord{Evidence: "first-original-response", Usage: tasks.GenerationUsage{Requests: 1, Tokens: 37}, Proposal: tasks.ActionProposal{Kind: "actions", Actions: []tasks.Action{{Key: "one", OperationID: "original-op", Descriptor: "descriptor", InputRef: "input", ResourceVersion: 1, ControlVersion: 1}}}}
			if e = p.Record(ctx, d.Qualification, d.Number, in); e != nil {
				t.Fatal(e)
			}
			raw, e := json.Marshal(actionProbe{back.path, back.token, phase, r.Task.Ref, back.clock.now})
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(t.TempDir(), "probe.json")
			if e = os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
			exe, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			cmd := exec.Command(exe, "-test.run=^TestActionProcessProbe$")
			cmd.Env = append(os.Environ(), "HARNESS_ACTION_PROBE="+path)
			output, e := cmd.CombinedOutput()
			if exit, ok := e.(*exec.ExitError); !ok || exit.ExitCode() != 73 {
				t.Fatalf("child exit: %v %s", e, output)
			}
			r, e = s.Load(ctx, r.Task.Ref)
			if e != nil {
				t.Fatal(e)
			}
			want := "READY"
			if phase == "dispatch" {
				want = "DISPATCHED"
			}
			if phase == "fact" {
				want = "DONE"
			}
			if len(r.Actions.Actions) != 1 || r.Actions.Actions[0].OperationID != "original-op" || r.Actions.Actions[0].Status != want || r.Task.ModelUsedTokens != 37 || r.Actions.Decisions[0].Record.Evidence != "first-original-response" || r.Task.State != "RUNNING" {
				t.Fatalf("lost durable work: %+v", r)
			}
			again, e := p.Admit(ctx, d.Qualification, d.Number)
			if e != nil || again.Task.Version != r.Task.Version || len(again.Actions.Actions) != 1 {
				t.Fatal("replay changed original admission")
			}
		})
	}
}
