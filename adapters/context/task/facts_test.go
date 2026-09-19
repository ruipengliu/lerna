package task_test

import (
	"context"
	taskcontext "lerna/adapters/context/task"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/tasks"
	"testing"
	"time"
)

type core struct {
	current tasks.RunSnapshot
	denied  bool
}

func (c *core) Current(context.Context, tasks.Ref) (tasks.RunSnapshot, error) { return c.current, nil }

func (c *core) CheckDecision(context.Context, tasks.Qualification) error {
	if c.denied {
		return contextassembly.Denied
	}
	return nil
}

type content struct{}

func (content) Assemble(_ context.Context, t tasks.Task, _ string, _ int) (brain.Input, error) {
	return brain.Input{Goal: t.Goal, Constraints: "Preserve user facts"}, nil
}
func (content) Validate(context.Context, tasks.Task, string) error { return nil }

type clock struct{}

func (clock) Now() (time.Time, error) { return time.Unix(1900000000, 0), nil }
func TestFactsStayValidAcrossAccountingButInvalidateOnInputOrControl(t *testing.T) {
	base := tasks.RunSnapshot{Task: tasks.Task{Ref: tasks.Ref{Namespace: "local", TaskID: "task"}, Subject: "alice", Goal: "Answer", Owner: "node", OwnerEpoch: 1, Version: 10, State: "RUNNING", Constraints: tasks.Constraints{DeadlineUnix: 1900000600}}, Work: []tasks.Work{{ID: "work", Worker: "worker", Generation: 1, InFlight: true, LeaseUntil: 1900000060000000000}}}
	state := &core{current: base}
	facts, e := taskcontext.NewFacts(state, content{}, clock{}, base, taskcontext.Scope{Purpose: "assist", Location: "model", Storage: "device", PolicyVersion: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	r := contextassembly.Request{Key: contextassembly.Key{Namespace: "local", TaskID: "task", Decision: 1}, Subject: "alice", Purpose: "assist", Location: "model", Storage: "device", FactsVersion: 1, PolicyVersion: "v1", MaxBytes: 8192}
	state.current.Task.Version++
	state.current.Task.ModelReservedTokens = 1000
	if _, e = facts.Load(context.Background(), r); e != nil {
		t.Fatalf("accounting invalidated facts %v", e)
	}
	state.denied = true
	if e = facts.Validate(context.Background(), r); e != contextassembly.Denied {
		t.Fatalf("revoked task execute %v", e)
	}
	state.denied = false
	state.current.Task.Goal = "Changed"
	if e = facts.Validate(context.Background(), r); e != contextassembly.Invalidated {
		t.Fatalf("changed facts %v", e)
	}
	state.current = base
	state.current.Task.Control.Intent = "PAUSE"
	if e = facts.Validate(context.Background(), r); e != contextassembly.Invalidated {
		t.Fatalf("paused %v", e)
	}
	state.current = base
	state.current.Task.OwnerEpoch = 2
	if e = facts.Validate(context.Background(), r); e != contextassembly.Invalidated {
		t.Fatalf("owner changed %v", e)
	}
}
