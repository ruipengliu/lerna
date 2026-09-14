package tasks_test

import (
	"context"
	"encoding/json"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type delegationProbe struct {
	Path, Token, Mode string
	Now               time.Time
	Qualification     tasks.Qualification
	Proposal          tasks.DelegationProposal
	Intent            tasks.ChildIntent
}

func TestDelegationProcessProbe(t *testing.T) {
	path := os.Getenv("HARNESS_DELEGATION_PROBE")
	if path == "" {
		t.Skip("child only")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var in delegationProbe
	if e = json.Unmarshal(raw, &in); e != nil {
		t.Fatal(e)
	}
	db, e := sqliteauth.Open(in.Path)
	if e != nil {
		t.Fatal(e)
	}
	a, e := authorization.New(db, &clock{in.Now}, authConfig())
	if e != nil {
		t.Fatal(e)
	}
	owner := "local-owner"
	if in.Mode == "accept" {
		owner = "child-owner"
	}
	s, e := tasks.New(a, tasks.Config{Namespace: "local", Resource: "root", Owner: owner, MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	l := tasks.DelegationLimits{MaxChildren: 4, MaxDepth: 3, MaxConcurrent: 2, MaxChecks: 16, IOTimeout: time.Second}
	ctx := context.Background()
	if in.Mode == "admit" {
		w := workerPort(t, s, in.Token, "parent-worker")
		p, e := w.Delegations(l, publicDelegationData)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = p.Admit(ctx, in.Qualification, in.Proposal); e != nil {
			t.Fatal(e)
		}
	} else {
		p, e := s.Children(in.Token, "local-owner", func(context.Context, tasks.ChildIntent, string) error { return nil }, func(context.Context, tasks.RunSnapshot) (tasks.ChildEvidence, error) {
			return tasks.ChildEvidence{Source: "probe", Evidence: "no-effects", Coverage: "all", Effect: "NONE"}, nil
		}, func(ctx context.Context) (string, error) { return a.NewOperation(ctx, in.Token) }, l, controlLimits())
		if e != nil {
			t.Fatal(e)
		}
		got, e := p.Accept(ctx, in.Intent)
		if e != nil {
			t.Fatal(e)
		}
		t.Logf("durable child %s", got.Ref.TaskID)
	}
	// No orderly cleanup or application state flushing.
	os.Exit(79)
}
func crashDelegation(t *testing.T, in delegationProbe) {
	t.Helper()
	raw, e := json.Marshal(in)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "probe.json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDelegationProcessProbe$", "-test.v")
	cmd.Env = append(os.Environ(), "HARNESS_DELEGATION_PROBE="+path)
	out, e := cmd.CombinedOutput()
	t.Log(string(out))
	exit, ok := e.(*exec.ExitError)
	if !ok || exit.ExitCode() != 79 {
		t.Fatalf("expected abrupt process exit 79: %v", e)
	}
}
func TestDelegationParentAndChildCommitSurviveProcessExit(t *testing.T) {
	s, _, token, c, _, p, r, proposal, path := delegationParent(t)
	q := tasks.QualificationOf(r)
	ctx := context.Background()
	probe := delegationProbe{Path: path, Token: token, Mode: "admit", Now: c.now, Qualification: q, Proposal: proposal}
	crashDelegation(t, probe)
	crashDelegation(t, probe)
	r, e := s.Load(ctx, r.Task.Ref)
	if e != nil || r.Task.DelegatedSteps != 2 {
		t.Fatal("reservation lost or duplicated", e)
	}
	// Persist the original outbound intent before an unavailable transport.
	_, e = p.Advance(ctx, tasks.QualificationOf(r), func(string) (tasks.ChildRemote, error) { return nil, context.DeadlineExceeded }, func(context.Context, tasks.ChildIntent) (string, error) { return "probe", nil }, func(context.Context, tasks.ChildSpec, tasks.ChildReport) (string, error) { return "ACCEPTED", nil })
	if e != context.DeadlineExceeded {
		t.Fatal(e)
	}
	r, e = s.Load(ctx, r.Task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	intent := *r.Delegations.Children[0].Intent
	intent.GrantMaterial = "probe"
	_, a, ct, cc, childPath := setup(t)
	enableControls(t, a, ct, cc)
	childProbe := delegationProbe{Path: childPath, Token: ct, Mode: "accept", Now: cc.now, Intent: intent}
	crashDelegation(t, childProbe)
	crashDelegation(t, childProbe)
	cs, e := tasks.New(a, tasks.Config{Namespace: "local", Resource: "root", Owner: "child-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	page, e := cs.ListRecoverable(ctx, tasks.RecoveryQuery{Namespace: "local", Limit: 10})
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Runs) != 1 {
		t.Fatalf("expected one durable child, got %d", len(page.Runs))
	}
	t.Logf("parent admission and child acceptance each survived two abrupt exits; child=%s; reservation=2", page.Runs[0].Task.Ref.TaskID)
}
