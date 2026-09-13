package tasks_test

import (
	"context"
	"fmt"
	"lerna/authorization"
	"lerna/tasks"
	"testing"
	"time"
)

type actionBacking struct {
	*authorization.Service
	token, path string
	clock       *clock
}

func TestExpiredActionLeaseFencesOldHostAndPreservesInvocation(t *testing.T) {
	s, w, p, r, back := actionRun(t)
	ctx := context.Background()
	q := tasks.QualificationOf(r)
	d, e := p.Reserve(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	if e = p.Record(ctx, q, d.Number, tasks.DecisionRecord{Evidence: "raw", Usage: tasks.GenerationUsage{Requests: 1, Tokens: 10}, Proposal: tasks.ActionProposal{Kind: "actions", Actions: []tasks.Action{{Key: "a", OperationID: "stable", Descriptor: "d", InputRef: "i", ResourceVersion: 1, ControlVersion: 1}}}}); e != nil {
		t.Fatal(e)
	}
	r, e = p.Admit(ctx, q, d.Number)
	if e != nil {
		t.Fatal(e)
	}
	a, e := p.Next(ctx, tasks.QualificationOf(r))
	if e != nil {
		t.Fatal(e)
	}
	oldHost := w.WithActionRecovery(tasks.QualificationOf(r))
	back.clock.now = back.clock.now.Add(2 * time.Second)
	r, e = p.EnsureLease(ctx, tasks.QualificationOf(r))
	if e != nil {
		t.Fatal(e)
	}
	for _, old := range []*tasks.WorkPort{w, oldHost} {
		if e = back.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error {
			return old.GuardExecution(tx, a.Qualification, a.OperationID, false)
		}); e == nil {
			t.Fatal("old host revived by replacement lease")
		}
	}
	current := w.WithActionRecovery(tasks.QualificationOf(r))
	if e = back.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error {
		return current.GuardExecution(tx, a.Qualification, a.OperationID, true)
	}); e != nil {
		t.Fatal(e)
	}
	if e = current.ConsumeExecution(ctx, tasks.ExecutionReport{OperationID: a.OperationID, Qualification: a.Qualification, Revision: 1, Phase: "FINISHED", Result: "SUCCESS", Effect: "CONFIRMED", Reference: "fact"}); e != nil {
		t.Fatal(e)
	}
	r, e = s.Load(ctx, r.Task.Ref)
	if e != nil || r.Task.State != "RUNNING" || r.Work[0].Generation != 2 || r.Actions.Actions[0].Qualification != a.Qualification || r.Actions.Actions[0].OperationID != "stable" {
		t.Fatalf("recovery replaced identity: %+v %v", r, e)
	}
}

func actionRun(t *testing.T) (*tasks.Service, *tasks.WorkPort, *tasks.ActionPort, tasks.RunSnapshot, *actionBacking) {
	t.Helper()
	s, a, token, c, path := setup(t)
	enableControls(t, a, token, c)
	in := submission(t, a, token, c)
	in.Constraints.ModelRequests = 4
	in.Constraints.ModelTokens = 65536
	in.Constraints.MaxSteps = 3
	task, e := s.Submit(context.Background(), token, in)
	if e != nil {
		t.Fatal(e)
	}
	w, e := s.BindWorker(tasks.WorkerBinding{Token: token, Subject: "admin", WorkerID: "action-worker", AllowEffectEvidence: true}, limits())
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Load(context.Background(), task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	for _, k := range []string{"claim", "start"} {
		r, e = w.Commit(context.Background(), tasks.WorkChange{ChangeID: k, Kind: k, Qualification: tasks.QualificationOf(r)})
		if e != nil {
			t.Fatal(e)
		}
	}
	p, e := w.Actions(tasks.ActionLimits{MaxOperations: 3, MaxQueries: 32, MaxCorrections: 2, InputTokens: 8192, OutputTokens: 1024})
	if e != nil {
		t.Fatal(e)
	}
	r, e = p.Initialize(context.Background(), tasks.QualificationOf(r))
	if e != nil {
		t.Fatal(e)
	}
	return s, w, p, r, &actionBacking{a, token, path, c}
}

func TestBatchAdmissionRejectsWholeGraphAndPreservesFirstRecord(t *testing.T) {
	for _, kind := range []string{"cycle", "oversized", "unknown-kind", "over-budget", "duplicate-operation", "recovery-start"} {
		t.Run(kind, func(t *testing.T) {
			s, _, p, r, _ := actionRun(t)
			ctx := context.Background()
			q := tasks.QualificationOf(r)
			d, e := p.Reserve(ctx, q)
			if e != nil {
				t.Fatal(e)
			}
			x := tasks.Action{Key: "a", OperationID: "op-a", Descriptor: "d", InputRef: "i", ResourceVersion: 1, ControlVersion: 1}
			proposal := tasks.ActionProposal{Kind: "actions", Actions: []tasks.Action{x}}
			switch kind {
			case "cycle":
				proposal.Actions[0].DependsOn = []string{"a"}
			case "unknown-kind":
				proposal.Kind = "execute-anything"
			case "recovery-start":
				proposal.Actions[0].RecoveryStart = q
			case "oversized", "over-budget":
				n := 9
				if kind == "over-budget" {
					n = 4
				}
				proposal.Actions = nil
				for i := 0; i < n; i++ {
					v := x
					v.Key = fmt.Sprint(i)
					v.OperationID = "op" + v.Key
					proposal.Actions = append(proposal.Actions, v)
				}
			case "duplicate-operation":
				v := x
				v.Key = "b"
				proposal.Actions = append(proposal.Actions, v)
			}
			record := tasks.DecisionRecord{Evidence: "first-invalid", Usage: tasks.GenerationUsage{Requests: 1, Tokens: 10}, Proposal: proposal}
			if e = p.Record(ctx, q, d.Number, record); e != nil {
				t.Fatal(e)
			}
			if _, e = p.Admit(ctx, q, d.Number); e == nil {
				t.Fatal("invalid batch admitted")
			}
			record.Evidence = "hidden-repair"
			if e = p.Record(ctx, q, d.Number, record); e == nil {
				t.Fatal("overwrote first proposal")
			}
			r, e = s.Load(ctx, r.Task.Ref)
			if e != nil || len(r.Actions.Actions) != 0 || r.Actions.Decisions[0].Record.Evidence != "first-invalid" {
				t.Fatalf("partial admission: %+v %v", r.Actions, e)
			}
		})
	}
}

type uncertainActionStore struct {
	underlying tasks.RuntimeStore
	armed      bool
}

func (s *uncertainActionStore) UpdateRuntime(ctx context.Context, fn func(authorization.RuntimeTransaction) error) error {
	e := s.underlying.UpdateRuntime(ctx, fn)
	if e == nil && s.armed {
		s.armed = false
		return &authorization.Error{Code: authorization.Unavailable}
	}
	return e
}

func TestUnknownAdmissionReplaysOriginalOperationMap(t *testing.T) {
	s, _, p, r, a := actionRun(t)
	ctx := context.Background()
	q := tasks.QualificationOf(r)
	d, e := p.Reserve(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	record := tasks.DecisionRecord{Evidence: "first", Usage: tasks.GenerationUsage{Requests: 1, Tokens: 10}, Proposal: tasks.ActionProposal{Kind: "actions", Actions: []tasks.Action{{Key: "a", OperationID: "stable-op", Descriptor: "d", InputRef: "i", ResourceVersion: 1, ControlVersion: 1}}}}
	if e = p.Record(ctx, q, d.Number, record); e != nil {
		t.Fatal(e)
	}
	lost := &uncertainActionStore{underlying: a}
	second, e := tasks.New(lost, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	w, e := second.BindWorker(tasks.WorkerBinding{Token: a.token, Subject: "admin", WorkerID: "action-worker", AllowEffectEvidence: true}, limits())
	if e != nil {
		t.Fatal(e)
	}
	p, e = w.Actions(r.Actions.Limits)
	if e != nil {
		t.Fatal(e)
	}
	lost.armed = true
	if _, e = p.Admit(ctx, q, d.Number); e == nil {
		t.Fatal("fault did not surface")
	}
	r, e = p.Admit(ctx, q, d.Number)
	if e != nil {
		t.Fatal(e)
	}
	again, e := p.Admit(ctx, q, d.Number)
	if e != nil || again.Task.Version != r.Task.Version || len(again.Actions.Actions) != 1 || again.Actions.Actions[0].OperationID != "stable-op" {
		t.Fatalf("replay: %+v %v", again.Actions, e)
	}
	loaded, e := s.Load(ctx, r.Task.Ref)
	if e != nil || loaded.Task.ModelUsedRequests != 1 {
		t.Fatal("generation repeated")
	}
}

func TestActionBatchKeepsTaskOpenBetweenDependentEffects(t *testing.T) {
	s, w, p, r, _ := actionRun(t)
	ctx := context.Background()
	q := tasks.QualificationOf(r)
	d, e := p.Reserve(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	// Operation strings here are Core identities; execution admission separately
	// verifies issuance and single-use grants in its own authority transaction.
	proposal := tasks.ActionProposal{Kind: "actions", Actions: []tasks.Action{{Key: "create", OperationID: "operation-a", Descriptor: "descriptor-a", InputRef: "input-a", ResourceVersion: 1, ControlVersion: 1, Write: true}, {Key: "submit", DependsOn: []string{"create"}, OperationID: "operation-b", Descriptor: "descriptor-b", InputRef: "input-b", ResourceVersion: 2, ControlVersion: 1, Write: true}}}
	if e = p.Record(ctx, q, d.Number, tasks.DecisionRecord{Evidence: "proposal-ref", Usage: tasks.GenerationUsage{Requests: 1, Tokens: 30}, Proposal: proposal}); e != nil {
		t.Fatal(e)
	}
	r, e = p.Admit(ctx, q, d.Number)
	if e != nil {
		t.Fatal(e)
	}
	first, e := p.Next(ctx, tasks.QualificationOf(r))
	if e != nil || first.Key != "create" {
		t.Fatalf("first: %+v %v", first, e)
	}
	if e = w.ConsumeExecution(ctx, tasks.ExecutionReport{OperationID: first.OperationID, Qualification: first.Qualification, Revision: 1, Phase: "FINISHED", Result: "SUCCESS", Effect: "CONFIRMED", Reference: "created-ref"}); e != nil {
		t.Fatal(e)
	}
	r, e = s.Load(ctx, r.Task.Ref)
	if e != nil || r.Task.State != "RUNNING" || r.Task.Result != "" {
		t.Fatalf("individual API completed goal: %+v %v", r.Task, e)
	}
	second, e := p.Next(ctx, tasks.QualificationOf(r))
	if e != nil || second.Key != "submit" || second.OperationID != "operation-b" {
		t.Fatalf("dependency: %+v %v", second, e)
	}
}

func TestWrongWriteCannotFinishWithPendingCompensation(t *testing.T) {
	_, _, p, r, _ := actionRun(t)
	ctx := context.Background()
	q := tasks.QualificationOf(r)
	d, e := p.Reserve(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	v := tasks.ActionProposal{Kind: "actions", Actions: []tasks.Action{{Key: "wrong", OperationID: "wrong-op", Descriptor: "d", InputRef: "i", ResourceVersion: 1, ControlVersion: 1, Write: true}}}
	if e = p.Record(ctx, q, d.Number, tasks.DecisionRecord{Evidence: "first", Usage: tasks.GenerationUsage{Requests: 1, Tokens: 5}, Proposal: v}); e != nil {
		t.Fatal(e)
	}
	r, e = p.Admit(ctx, q, d.Number)
	if e != nil {
		t.Fatal(e)
	}
	r, e = p.Finish(ctx, tasks.QualificationOf(r), "wrong-write-evidence", true)
	if e != nil || r.Task.State != "FAILED" || !r.Actions.WrongWrite {
		t.Fatalf("wrong write escaped: %+v %v", r.Task, e)
	}
}

func TestUnknownActionWaitAllowsBudgetedOriginalQueries(t *testing.T) {
	s, w, p, r, _ := actionRun(t)
	ctx := context.Background()
	q := tasks.QualificationOf(r)
	d, e := p.Reserve(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	proposal := tasks.ActionProposal{Kind: "actions", Actions: []tasks.Action{{Key: "a", OperationID: "op", Descriptor: "d", InputRef: "i", ResourceVersion: 1, ControlVersion: 1}}}
	if e = p.Record(ctx, q, d.Number, tasks.DecisionRecord{Evidence: "raw", Usage: tasks.GenerationUsage{Requests: 1, UnknownRequests: 1}, Proposal: proposal}); e != nil {
		t.Fatal(e)
	}
	r, e = p.Admit(ctx, q, d.Number)
	if e != nil {
		t.Fatal(e)
	}
	a, e := p.Next(ctx, tasks.QualificationOf(r))
	if e != nil {
		t.Fatal(e)
	}
	fact := tasks.ExecutionReport{OperationID: "op", Qualification: a.Qualification, Revision: 1, Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}
	if e = w.ConsumeExecution(ctx, fact); e != nil {
		t.Fatal(e)
	}
	r, e = s.Load(ctx, r.Task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Reserve(ctx, tasks.QualificationOf(r)); e == nil {
		t.Fatal("unknown action allowed replacement decision")
	}
	if e = p.ChargeQuery(ctx, tasks.QualificationOf(r), "reconcile-original"); e != nil {
		t.Fatal(e)
	}
	fact.Revision = 2
	fact.Phase = "FINISHED"
	fact.Result = "SUCCESS"
	fact.Effect = "CONFIRMED"
	fact.Reference = "evidence"
	if e = w.ConsumeExecution(ctx, fact); e != nil {
		t.Fatal(e)
	}
	r, e = s.Load(ctx, r.Task.Ref)
	if e != nil || r.Task.State != "RUNNING" || r.Task.ModelReservedRequests != 1 || r.Actions.Actions[0].OperationID != "op" {
		t.Fatalf("lost original action/reservation: %+v %v", r.Task, e)
	}
}

func TestActionAdmissionFencesControlAndWorkerChanges(t *testing.T) {
	for _, kind := range []string{"pause", "version", "owner", "epoch", "worker"} {
		t.Run(kind, func(t *testing.T) {
			s, _, p, r, back := actionRun(t)
			ctx := context.Background()
			q := tasks.QualificationOf(r)
			d, e := p.Reserve(ctx, q)
			if e != nil {
				t.Fatal(e)
			}
			record := tasks.DecisionRecord{Evidence: "first", Usage: tasks.GenerationUsage{Requests: 1, Tokens: 10}, Proposal: tasks.ActionProposal{Kind: "actions", Actions: []tasks.Action{{Key: "a", OperationID: "op", Descriptor: "d", InputRef: "i", ResourceVersion: 1, ControlVersion: 1}}}}
			if e = p.Record(ctx, q, d.Number, record); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "pause":
				controls, e := s.Controls(controlLimits())
				if e != nil {
					t.Fatal(e)
				}
				if _, e = controls.Request(ctx, back.token, controlRequest(t, back.Service, back.token, r.Task, "PAUSE")); e != nil {
					t.Fatal(e)
				}
			case "version":
				q.Version++
			case "owner":
				q.Owner = "other"
			case "epoch":
				q.Epoch++
			case "worker":
				w, e := s.BindWorker(tasks.WorkerBinding{Token: back.token, Subject: "admin", WorkerID: "other-worker", AllowEffectEvidence: true}, limits())
				if e != nil {
					t.Fatal(e)
				}
				p, e = w.Actions(r.Actions.Limits)
				if e != nil {
					t.Fatal(e)
				}
			}
			if _, e = p.Admit(ctx, q, d.Number); e == nil {
				t.Fatal("stale authority admitted actions")
			}
			r, e = s.Load(ctx, r.Task.Ref)
			if e != nil || len(r.Actions.Actions) != 0 {
				t.Fatal("partial admission after fencing")
			}
		})
	}
}
