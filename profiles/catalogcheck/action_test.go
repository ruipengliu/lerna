package catalogcheck

import (
	"context"
	"encoding/json"
	"lerna/authorization"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"testing"
	"time"
)

func TestBrainCompletesOneTaskThroughTwoAPIResults(t *testing.T) {
	report, e := RunActionCase(context.Background(), "multistep", nil)
	if e != nil {
		t.Fatal(e)
	}
	if report.State != "COMPLETED" || report.Decisions != 2 || report.Operations != 2 || report.DirectorySize != 1008 || report.FinalState != "authorized" || report.Ledger != 997 {
		t.Fatalf("loop did not reach independently checked goal: %+v", report)
	}
}

func TestModelDisclosureRequiresCurrentCloudAuthorization(t *testing.T) {
	ctx := context.Background()
	f, e := newFixture(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer f.close()
	if _, e = f.manager.Resolve(ctx, f.defs[0].Kind); e != nil {
		t.Fatal(e)
	}
	a := &actionHost{f: f, h: f.manager.current, location: "ark-cn-beijing"}
	if e = a.allowPublic(ctx); e != nil {
		t.Fatal(e)
	}
	host := f.host
	snap, e := host.db.Load(ctx)
	if e != nil {
		t.Fatal(e)
	}
	op, e := host.operation(ctx)
	if e != nil {
		t.Fatal(e)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"catalog.search", "catalog.describe", "content.process", "content.disclose"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: host.now().Add(time.Hour).Unix()}
	_, e = host.auth.Execute(ctx, host.token, authorization.Mutation{Namespace: host.namespace, OperationID: op, Command: &wire.AuthorizationCommand{ExpectedRevision: snap.State.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "local-only", Scope: scope}}}}}})
	if e != nil {
		t.Fatal(e)
	}
	// Source rules still allow cloud, but the current principal policy does not.
	if e = a.Validate(ctx, tasks.Task{}, "ark-cn-beijing"); e == nil {
		t.Fatal("local catalog authority implicitly authorized cloud model disclosure")
	}
}

func TestBrainKeepsFailuresAndHonorsExplicitWaits(t *testing.T) {
	for _, tc := range []struct {
		mode, state, final    string
		decisions, operations int
		first                 bool
	}{
		{"single", "COMPLETED", "authorized", 1, 1, true},
		{"batch", "COMPLETED", "authorized", 1, 2, true},
		{"recover-dispatch", "COMPLETED", "authorized", 2, 2, true},
		{"recover-record", "COMPLETED", "authorized", 2, 2, true},
		{"revoked-record", "WAITING", "draft", 1, 0, true},
		{"missing-resume", "COMPLETED", "authorized", 3, 2, true},
		{"read-only-correction", "COMPLETED", "authorized", 3, 2, false},
		{"missing", "WAITING", "draft", 1, 0, true},
		{"invalid-then-correct", "COMPLETED", "authorized", 3, 2, false},
		{"wrong-write", "FAILED", "rejected", 1, 1, false},
		{"no-progress", "WAITING", "draft", 3, 0, false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			r, e := RunActionCase(context.Background(), tc.mode, nil)
			if e != nil {
				t.Fatal(e)
			}
			if r.State != tc.state || r.FinalState != tc.final || r.Decisions != tc.decisions || r.Operations != tc.operations || r.FirstCorrect != tc.first {
				t.Fatalf("outcome: %+v", r)
			}
		})
	}
}

type inputAwareModel struct{ actionScript }

func (m *inputAwareModel) Generate(ctx context.Context, req brain.Request) (brain.Result, error) {
	found := false
	for _, b := range req.Input.Blocks {
		if b.Role != "input" {
			continue
		}
		var v struct {
			Record          string
			Quantity, Limit int
			Verified        bool
		}
		if json.Unmarshal([]byte(b.Text), &v) == nil && v.Record == "item" && v.Quantity == 3 && v.Limit == 10 && v.Verified {
			found = true
		}
	}
	if !found {
		return brain.Result{}, brain.Error("TASK_INPUT_MISSING")
	}
	return m.actionScript.Generate(ctx, req)
}
func TestActionModelReceivesAuthorizedTaskInput(t *testing.T) {
	r, e := RunActionCase(context.Background(), "multistep", &inputAwareModel{actionScript{mode: "multistep"}})
	if e != nil || r.State != "COMPLETED" {
		t.Fatalf("model could not use submitted input: %+v %v", r, e)
	}
}
func TestUnnecessaryModelWaitIsNotFirstCorrect(t *testing.T) {
	r, e := RunActionCase(context.Background(), "multistep", &actionScript{mode: "missing"})
	if e != nil || r.State != "WAITING" || r.FirstCorrect {
		t.Fatalf("unnecessary wait counted correct: %+v %v", r, e)
	}
}

func TestInvalidatedAdmittedContextWaitsBeforeDispatch(t *testing.T) {
	r, e := RunActionCase(context.Background(), "revoked-admitted", nil)
	if e != nil {
		t.Fatal(e)
	}
	if r.State != "WAITING" || r.StopReason != "input_invalidated" || r.FinalState != "draft" || r.Decisions != 1 || r.Operations != 1 || r.ReadyOperations != 1 || r.DispatchedOperations != 0 {
		t.Fatalf("dispatch after invalidation: %+v", r)
	}
}
