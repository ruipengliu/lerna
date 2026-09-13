package taskcontext_test

import (
	"context"
	"lerna/adapters/sqlitecontext"
	"lerna/adapters/taskcontext"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/execution"
	"lerna/tasks"
	"path/filepath"
	"testing"
	"time"
)

type memories struct{ revoked bool }

func (m *memories) Load(context.Context, contextassembly.Request, contextassembly.Reference) (contextassembly.Source, error) {
	return contextassembly.Source{Block: brain.Block{Ref: "memory:style", Text: "concise", Subject: "alice", Role: "memory"}, Applicable: true}, nil
}
func (m *memories) Validate(context.Context, contextassembly.Request, contextassembly.Reference, bool) error {
	if m.revoked {
		return contextassembly.Denied
	}
	return nil
}
func TestBrainSessionKeepsFactsAndChecksCurrentMemory(t *testing.T) {
	ctx := context.Background()
	base := tasks.RunSnapshot{Task: tasks.Task{Ref: tasks.Ref{Namespace: "local", TaskID: "task"}, Subject: "alice", Goal: "Answer", Owner: "node", OwnerEpoch: 1, Version: 10, State: "RUNNING", Constraints: tasks.Constraints{DeadlineUnix: 1900000600}}, Work: []tasks.Work{{ID: "work", Worker: "worker", Generation: 1, InFlight: true, LeaseUntil: 1900000060000000000}}}
	state := &core{current: base}
	facts, e := taskcontext.NewFacts(state, content{}, clock{}, base, taskcontext.Scope{Purpose: "assist", Location: "model", Storage: "device", PolicyVersion: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	store, e := sqlitecontext.Open(filepath.Join(t.TempDir(), "context.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	source := &memories{}
	assembler, e := contextassembly.New(store, facts, source)
	if e != nil {
		t.Fatal(e)
	}
	req := contextassembly.Request{Key: contextassembly.Key{Namespace: "local", TaskID: "task", Decision: 1}, Subject: "alice", Purpose: "assist", Location: "model", Storage: "device", FactsVersion: 1, PolicyVersion: "v1", MaxBytes: 8192, Candidates: []contextassembly.Candidate{{Reference: contextassembly.Reference{Namespace: "local", Collection: "personal", Key: "style", Revision: 1}}}}
	session, e := taskcontext.NewSession(assembler, req, base.Task)
	if e != nil {
		t.Fatal(e)
	}
	var port brain.Context = session
	in, e := port.Assemble(ctx, base.Task, "model", 8192)
	if e != nil || len(in.Blocks) != 1 || in.Blocks[0].Text != "concise" {
		t.Fatalf("input %v %v", in, e)
	}
	changed := base.Task
	changed.Goal = "Other"
	if _, e = port.Assemble(ctx, changed, "model", 8192); e != contextassembly.Invalidated {
		t.Fatalf("changed decision facts %v", e)
	}
	if _, e = port.Assemble(ctx, base.Task, "other-model", 8192); e != contextassembly.Denied {
		t.Fatalf("other location %v", e)
	}
	invocation := execution.Request{OperationID: "action-one", InputRef: "input", Qualification: tasks.QualificationOf(base)}
	guard, e := session.StartGuard(invocation, base.Task)
	if e != nil {
		t.Fatal(e)
	}
	if e = guard.ValidateStart(ctx, invocation); e != nil {
		t.Fatal(e)
	}
	other := invocation
	other.OperationID = "action-two"
	if e = guard.ValidateStart(ctx, other); e != contextassembly.Denied {
		t.Fatalf("different operation reused context binding: %v", e)
	}
	other = invocation
	other.InputRef = "changed-input"
	if e = guard.ValidateStart(ctx, other); e != contextassembly.Denied {
		t.Fatalf("changed operation reused context binding: %v", e)
	}
	source.revoked = true
	if e = guard.ValidateStart(ctx, invocation); e != contextassembly.Denied {
		t.Fatalf("revoked Memory allowed invocation start: %v", e)
	}
	source.revoked = false
	t.Run("model-return-revocation", func(t *testing.T) {
		saved := &output{}
		model, e := brain.NewAnswer(revokingModel{source}, session, saved, accounting{}, brain.Config{MaxInputBytes: 8192, MaxOutputBytes: 1024, SettlementTimeout: time.Second})
		if e != nil {
			t.Fatal(e)
		}
		decision := tasks.DecisionInput{Task: base.Task, Work: base.Work[0], Generation: tasks.GenerationReservation{Qualification: tasks.QualificationOf(base), OutputOperation: "output", Limits: tasks.GenerationLimits{Requests: 1, InputTokens: 1024, OutputTokens: 512}}}
		if _, e = model.Decide(ctx, decision); e != contextassembly.Denied || saved.saved {
			t.Fatalf("revoked model output saved=%v error=%v", saved.saved, e)
		}
	})
	source.revoked = true
	if e = port.Validate(ctx, base.Task, "model"); e != contextassembly.Denied {
		t.Fatalf("revoked dependency %v", e)
	}
}

type revokingModel struct{ source *memories }

func (revokingModel) Capabilities() brain.Capabilities {
	return brain.Capabilities{Model: "test", Location: "model", Version: "1", Text: true, Structured: true, HardBounds: true, InputUpper: 1, ContextTokens: 2048}
}
func (m revokingModel) Generate(context.Context, brain.Request) (brain.Result, error) {
	m.source.revoked = true
	return brain.Result{Content: []byte(`{"answer":"Hi","sources":["memory:style"]}`), Finish: "stop", Usage: brain.Usage{Known: true, Input: 100, Output: 20}}, nil
}

type accounting struct{}

func (accounting) BeginRequest(context.Context, tasks.Qualification, uint32) error { return nil }
func (accounting) CheckDecision(context.Context, tasks.Qualification) error        { return nil }
func (accounting) Settle(context.Context, tasks.Qualification, tasks.GenerationUsage) error {
	return nil
}

type output struct{ saved bool }

func (o *output) Save(context.Context, tasks.DecisionInput, []byte) (string, error) {
	o.saved = true
	return "answer", nil
}
