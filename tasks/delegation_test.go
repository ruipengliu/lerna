package tasks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"lerna/authorization"
	"lerna/brain"
	"lerna/tasks"
	"testing"
	"time"
)

func TestDelegationAdmissionAndParentCompletion(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableExecution(t, a, token, c)
	ctx := context.Background()
	in := submission(t, a, token, c)
	in.Constraints.MaxSteps = 8
	parent, e := s.Submit(ctx, token, in)
	if e != nil {
		t.Fatal(e)
	}
	worker := workerPort(t, s, token, "parent-worker")
	run, e := s.Load(ctx, parent.Ref)
	if e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"claim", "start"} {
		run, e = worker.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(run)})
		if e != nil {
			t.Fatal(e)
		}
	}
	port, e := worker.Delegations(tasks.DelegationLimits{MaxChildren: 4, MaxDepth: 3, MaxConcurrent: 2, MaxChecks: 8, IOTimeout: time.Second}, publicDelegationData)
	if e != nil {
		t.Fatal(e)
	}
	op, e := a.NewOperation(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	childOp, e := a.NewOperation(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	proposal := tasks.DelegationProposal{OperationID: op, Children: []tasks.ChildSpec{{Key: "child", OperationID: childOp, Agent: "child-owner", Goal: "answer independently", Input: []byte(`{"question":"value"}`), InputSchema: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://harness.test/child","type":"object"}`), ResultSchema: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://harness.test/child","type":"object"}`), Required: true, Budget: tasks.Constraints{MaxSteps: 2, DeadlineUnix: c.now.Add(time.Minute).Unix()}, Acceptance: "verify source evidence"}}}
	q := tasks.QualificationOf(run)
	run, e = port.Admit(ctx, q, proposal)
	if e != nil {
		t.Fatal(e)
	}
	if run.Task.DelegatedSteps != 2 || len(run.Delegations.Children) != 1 {
		t.Fatal(run)
	}
	again, e := port.Admit(ctx, q, proposal)
	if e != nil || again.Task.DelegatedSteps != 2 {
		t.Fatal(again, e)
	}
	_, e = worker.Commit(ctx, tasks.WorkChange{ChangeID: "premature", Kind: "complete", Qualification: tasks.QualificationOf(run), Proposal: tasks.Proposal{Kind: "result", BaseVersion: run.Task.Version, Complete: true, Result: "done"}})
	if !authorization.Is(e, authorization.Conflict) {
		t.Fatalf("parent completed with unresolved child: %v", e)
	}
}

type lostChildReply struct {
	tasks.ChildRemote
	lost bool
}

func (c *lostChildReply) Accept(ctx context.Context, in tasks.ChildIntent) (tasks.Task, error) {
	t, e := c.ChildRemote.Accept(ctx, in)
	if e == nil && !c.lost {
		c.lost = true
		return tasks.Task{}, context.DeadlineExceeded
	}
	return t, e
}

func TestDelegationLostAcceptanceAndIndependentVerification(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableUpdates(t, a, token, c)
	_, ca, ct, cc, _ := setup(t)
	enableControls(t, ca, ct, cc)
	childService, e := tasks.New(ca, tasks.Config{Namespace: "local", Resource: "root", Owner: "child-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	l := tasks.DelegationLimits{MaxChildren: 4, MaxDepth: 3, MaxConcurrent: 2, MaxChecks: 32, IOTimeout: time.Second}
	revoked := false
	child, e := childService.Children(ct, "local-owner", func(context.Context, tasks.ChildIntent, string) error {
		if revoked {
			return &authorization.Error{Code: authorization.Denied}
		}
		return nil
	}, func(_ context.Context, r tasks.RunSnapshot) (tasks.ChildEvidence, error) {
		return tasks.ChildEvidence{Evidence: "artifact:42", Source: "child-owner", Coverage: "value", Effect: "NONE"}, nil
	}, func(ctx context.Context) (string, error) { return ca.NewOperation(ctx, ct) }, l, controlLimits())
	if e != nil {
		t.Fatal(e)
	}
	input := submission(t, a, token, c)
	input.Constraints.MaxSteps = 8
	input.Constraints.ModelRequests = 1
	input.Constraints.ModelTokens = 2048
	parent, e := s.Submit(ctx, token, input)
	if e != nil {
		t.Fatal(e)
	}
	w := workerPort(t, s, token, "parent-worker")
	r, _ := s.Load(ctx, parent.Ref)
	for _, kind := range []string{"claim", "start"} {
		r, e = w.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(r)})
		if e != nil {
			t.Fatal(e)
		}
	}
	denyReports := false
	p, e := w.Delegations(l, func(_ context.Context, _ tasks.Task, _ tasks.ChildSpec, report *tasks.ChildReport) error {
		if denyReports && report != nil {
			return &authorization.Error{Code: authorization.Denied}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	schema := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://harness.test/child","type":"object","required":["value"],"properties":{"value":{"const":42}},"additionalProperties":false}`)
	op, _ := a.NewOperation(ctx, token)
	proposal := tasks.DelegationProposal{OperationID: op}
	for _, key := range []string{"a", "b", "after"} {
		op, _ := a.NewOperation(ctx, token)
		spec := tasks.ChildSpec{Key: key, OperationID: op, Agent: "child-owner", Goal: "return independently checked value", Acceptance: "value equals 42", Input: []byte(`{"value":42}`), InputSchema: schema, ResultSchema: schema, Required: true, Budget: tasks.Constraints{MaxSteps: 2, DeadlineUnix: c.now.Add(time.Minute).Unix()}}
		if key == "after" {
			spec.DependsOn = []string{"a", "b"}
		}
		proposal.Children = append(proposal.Children, spec)
	}

	// Drive the complete graph through the actual finite Brain contract and the
	// existing durable generation accounting before Core admits any children.
	proposed := append([]tasks.ChildSpec(nil), proposal.Children...)
	for i := range proposed {
		proposed[i].OperationID = ""
	}
	raw, _ := json.Marshal(brain.DelegationOutput{Children: proposed})
	model := &delegationModel{body: raw}
	environment := &delegationContext{}
	g, e := w.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 1024, OutputTokens: 1024})
	if e != nil {
		t.Fatal(e)
	}
	g = g.WithOutputIdentities(func(ctx context.Context) (string, error) { return a.NewOperation(ctx, token) })
	r, e = g.ReserveDecision(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	decider, e := brain.NewDelegations(model, environment, environment, g, brain.Config{MaxInputBytes: 8192, MaxOutputBytes: 8192, SettlementTimeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	decision, e := decider.Decide(ctx, tasks.DecisionInput{Task: r.Task, Work: r.Work[0], Generation: r.Generations[0]})
	if e != nil || decision.Kind != "delegate" {
		t.Fatal(decision, e)
	}
	contextInput, e := environment.Assemble(ctx, r.Task, "local", 8192)
	if e != nil {
		t.Fatal(e)
	}
	proposal, e = brain.PrepareDelegation(ctx, environment.data, contextInput, func(ctx context.Context) (string, error) { return a.NewOperation(ctx, token) })
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.Load(ctx, parent.Ref)
	if e != nil {
		t.Fatal(e)
	}
	r, e = p.Admit(ctx, tasks.QualificationOf(r), proposal)
	if e != nil {
		t.Fatal(e)
	}
	remote := &lostChildReply{ChildRemote: child}
	resolve := func(string) (tasks.ChildRemote, error) { return remote, nil }
	grant := func(context.Context, tasks.ChildIntent) (string, error) { return "test-policy-material", nil }
	assessments := 0
	assess := func(_ context.Context, _ tasks.ChildSpec, r tasks.ChildReport) (string, error) {
		assessments++
		if string(r.Result) != `{"value":42}` {
			return "CONFLICT", nil
		}
		return "ACCEPTED", nil
	}
	r, e = p.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
	if e != context.DeadlineExceeded {
		t.Fatalf("expected lost reply: %v", e)
	}
	original := *r.Delegations.Children[0].Intent
	first, e := child.Lookup(ctx, tasks.ChildReferenceOf(original))
	if e != nil {
		t.Fatal(e)
	}
	replay, e := child.Accept(ctx, original)
	if e != nil || replay.Ref != first.Ref {
		t.Fatal("duplicate admission", e)
	}
	changed := original
	changed.Spec.Goal = "altered"
	if _, e = child.Accept(ctx, changed); !authorization.Is(e, authorization.IdentityConflict) {
		t.Fatal("changed intent", e)
	}
	changed = original
	changed.ParentEpoch++
	if _, e = child.Accept(ctx, changed); !authorization.Is(e, authorization.IdentityConflict) {
		t.Fatal("original operation reused across parent epochs", e)
	}
	// The first two branches can be active while their dependent remains queued.
	r, e = p.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
	if e != nil {
		t.Fatal(e)
	}
	if r.Delegations.Children[2].Status != "QUEUED" {
		t.Fatal("dependent dispatched early")
	}
	cw, e := child.BindWorker(tasks.WorkerBinding{Token: ct, Subject: "admin", WorkerID: "child-worker"}, limits())
	if e != nil {
		t.Fatal(e)
	}
	completed := map[tasks.Ref]bool{}
	for i := 0; i < 12; i++ {
		if i == 1 {
			denyReports = true
			_, e = p.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
			if !authorization.Is(e, authorization.Denied) || assessments != 0 {
				t.Fatal("denied report reached assessment", e, assessments)
			}
			denyReports = false
			r, e = s.Load(ctx, parent.Ref)
			if e != nil {
				t.Fatal(e)
			}
		}

		r, e = p.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
		if e != nil {
			t.Fatal(e)
		}
		for _, link := range r.Delegations.Children {
			if link.Intent == nil {
				continue
			}
			task, err := child.Lookup(ctx, tasks.ChildReferenceOf(*link.Intent))
			if authorization.Is(err, authorization.NotFound) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			if completed[task.Ref] {
				continue
			}
			cr, _ := childService.Load(ctx, task.Ref)
			revoked = true
			if _, err = cw.Commit(ctx, tasks.WorkChange{ChangeID: "revoked", Kind: "claim", Qualification: tasks.QualificationOf(cr)}); !authorization.Is(err, authorization.Denied) {
				t.Fatal("revoked child started", err)
			}
			revoked = false
			for _, kind := range []string{"claim", "start", "complete"} {
				change := tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(cr)}
				if kind == "complete" {
					change.Proposal = tasks.Proposal{Kind: "result", Complete: true, BaseVersion: cr.Task.Version, Result: `{"value":42}`}
				}
				cr, err = cw.Commit(ctx, change)
				if err != nil {
					t.Fatal(err)
				}
			}
			completed[task.Ref] = true
		}
		all := true
		for _, link := range r.Delegations.Children {
			all = all && link.Status == "ACCEPTED"
		}
		if all {
			break
		}
	}
	if len(completed) != 3 || r.Task.DelegatedSteps != 3 {
		t.Fatalf("ledger: completed %d, reserved/used %d", len(completed), r.Task.DelegatedSteps)
	}
	_, e = p.Complete(ctx, tasks.QualificationOf(r))
	if !authorization.Is(e, authorization.Conflict) {
		t.Fatal("child labels completed parent", e)
	}

	updates, e := s.Updates(inputFixture{}, updateLimits())
	if e != nil {
		t.Fatal(e)
	}

	denyReports = true
	_, e = p.PublishResults(ctx, tasks.QualificationOf(r), updates, func(context.Context, tasks.Task, []tasks.ChildReport) (string, error) {
		t.Fatal("denied report reached save")
		return "", nil
	}, func(ctx context.Context) (string, error) { return a.NewOperation(ctx, token) })
	if !authorization.Is(e, authorization.Denied) {
		t.Fatal("revoked report saved", e)
	}
	denyReports = false
	facts := []byte{}
	_, e = p.PublishResults(ctx, tasks.QualificationOf(r), updates, func(_ context.Context, _ tasks.Task, reports []tasks.ChildReport) (string, error) {
		facts, _ = json.Marshal(reports)
		return "child-reports", nil
	}, func(ctx context.Context) (string, error) { return a.NewOperation(ctx, token) })
	if e != nil {
		t.Fatal(e)
	}
	r, _ = s.Load(ctx, parent.Ref)
	if len(facts) == 0 || len(r.Task.InputFacts) != 1 || r.Task.InputFacts[0].Subject != "admin" || r.Task.InputFacts[0].Kind != "append" {
		t.Fatal("missing trusted collaboration facts")
	}
	stopped := tasks.QualificationOf(r)
	stopped.Version = r.Work[0].DecisionVersion
	r, e = w.Observe(ctx, tasks.Observation{ChangeID: "proposal-returned", Qualification: stopped, Status: "STOPPED"})
	if e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"claim", "start"} {
		r, e = w.Commit(ctx, tasks.WorkChange{ChangeID: "fresh-" + kind, Kind: kind, Qualification: tasks.QualificationOf(r)})
		if e != nil {
			t.Fatal(e)
		}
	}
	r, e = p.Approve(ctx, tasks.QualificationOf(r), "independently checked 42", func(_ context.Context, r tasks.RunSnapshot, _ string) error {
		for _, link := range r.Delegations.Children {
			if string(link.Report.Result) != `{"value":42}` {
				return context.Canceled
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	r, e = p.Complete(ctx, tasks.QualificationOf(r))
	if e != nil || r.Task.State != "COMPLETED" {
		t.Fatal(r.Task.State, e)
	}
	t.Logf("three unique children; lost reply recovered; parallel/dependent branches verified; parent retained %d consumed child steps", r.Task.DelegatedSteps)
}

func delegationParent(t *testing.T) (*tasks.Service, *authorization.Service, string, *clock, *tasks.WorkPort, *tasks.DelegationPort, tasks.RunSnapshot, tasks.DelegationProposal, string) {
	t.Helper()
	s, a, token, c, path := setup(t)
	enableUpdates(t, a, token, c)
	ctx := context.Background()
	in := submission(t, a, token, c)
	in.Constraints.MaxSteps = 8
	task, e := s.Submit(ctx, token, in)
	if e != nil {
		t.Fatal(e)
	}
	w := workerPort(t, s, token, "parent-worker")
	r, _ := s.Load(ctx, task.Ref)
	for _, kind := range []string{"claim", "start"} {
		r, e = w.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(r)})
		if e != nil {
			t.Fatal(e)
		}
	}
	p, e := w.Delegations(tasks.DelegationLimits{MaxChildren: 4, MaxDepth: 3, MaxConcurrent: 2, MaxChecks: 16, IOTimeout: time.Second}, publicDelegationData)
	if e != nil {
		t.Fatal(e)
	}
	op, _ := a.NewOperation(ctx, token)
	cop, _ := a.NewOperation(ctx, token)
	schema := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://harness.test/value","type":"object"}`)
	proposal := tasks.DelegationProposal{OperationID: op, Children: []tasks.ChildSpec{{Key: "a", Agent: "child-owner", OperationID: cop, Goal: "value", Acceptance: "verify", Input: []byte(`{}`), InputSchema: schema, ResultSchema: schema, Required: true, Budget: tasks.Constraints{MaxSteps: 2, DeadlineUnix: c.now.Add(time.Minute).Unix()}}}}
	return s, a, token, c, w, p, r, proposal, path
}
func TestDelegationRejectsCyclesAndBudgetRaces(t *testing.T) {
	_, _, _, _, _, p, r, proposal, _ := delegationParent(t)
	ctx := context.Background()
	q := tasks.QualificationOf(r)
	cyclic := proposal
	cyclic.Children = append([]tasks.ChildSpec(nil), proposal.Children...)
	cyclic.Children[0].DependsOn = []string{"a"}
	if _, e := p.Admit(ctx, q, cyclic); !authorization.Is(e, authorization.Invalid) {
		t.Fatal("cycle admitted", e)
	}
	large := proposal
	large.Children = append([]tasks.ChildSpec(nil), proposal.Children...)
	large.Children[0].Budget.MaxSteps = 8
	if _, e := p.Admit(ctx, q, large); !authorization.Is(e, authorization.Unavailable) {
		t.Fatal("over-budget admitted", e)
	}

	for _, test := range []struct {
		name   string
		change func(*tasks.ChildSpec)
		code   authorization.Code
	}{
		{"input-limit", func(c *tasks.ChildSpec) { c.Input = bytes.Repeat([]byte("x"), 16385) }, authorization.Invalid},
		{"schema-limit", func(c *tasks.ChildSpec) { c.InputSchema = bytes.Repeat([]byte(" "), 8193) }, authorization.Invalid},
		{"empty-input", func(c *tasks.ChildSpec) { c.Input = nil }, authorization.Invalid},
		{"deadline-widening", func(c *tasks.ChildSpec) { c.Budget.DeadlineUnix = r.Task.Constraints.DeadlineUnix + 1 }, authorization.Denied},
		{"expired", func(c *tasks.ChildSpec) { c.Budget.DeadlineUnix = r.Task.Constraints.DeadlineUnix - 120 }, authorization.Denied},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := proposal
			bad.Children = append([]tasks.ChildSpec(nil), proposal.Children...)
			test.change(&bad.Children[0])
			if _, e := p.Admit(ctx, q, bad); !authorization.Is(e, test.code) {
				t.Fatal(e)
			}
		})
	}
	tooMany := proposal
	tooMany.Children = make([]tasks.ChildSpec, 5)
	if _, e := p.Admit(ctx, q, tooMany); !authorization.Is(e, authorization.Invalid) {
		t.Fatal("unbounded batch", e)
	}
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() { _, e := p.Admit(ctx, q, proposal); results <- e }()
	}
	for i := 0; i < 8; i++ {
		if e := <-results; e != nil {
			t.Fatal(e)
		}
	}
	got, e := p.Admit(ctx, q, proposal)
	if e != nil || got.Task.DelegatedSteps != 2 {
		t.Fatal("duplicate budget", e)
	}
}
func TestDelegationCancelTombstoneAndUnknownEffects(t *testing.T) {
	s, a, token, _, w, p, r, proposal, _ := delegationParent(t)
	ctx := context.Background()
	_, ca, ct, cc, _ := setup(t)
	enableControls(t, ca, ct, cc)
	cs, e := tasks.New(ca, tasks.Config{Namespace: "local", Resource: "root", Owner: "child-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	unknown := true
	cp, e := cs.Children(ct, "local-owner", func(context.Context, tasks.ChildIntent, string) error { return nil }, func(context.Context, tasks.RunSnapshot) (tasks.ChildEvidence, error) {
		effect := "NONE"
		if unknown {
			effect = "UNKNOWN"
		}
		return tasks.ChildEvidence{Source: "child-owner", Evidence: "observed", Coverage: "whole child", Effect: effect}, nil
	}, func(ctx context.Context) (string, error) { return ca.NewOperation(ctx, ct) }, tasks.DelegationLimits{MaxChildren: 4, MaxDepth: 3, MaxConcurrent: 2, MaxChecks: 16, IOTimeout: time.Second}, controlLimits())
	if e != nil {
		t.Fatal(e)
	}
	r, e = p.Admit(ctx, tasks.QualificationOf(r), proposal)
	if e != nil {
		t.Fatal(e)
	}
	resolve := func(string) (tasks.ChildRemote, error) { return cp, nil }
	grant := func(context.Context, tasks.ChildIntent) (string, error) { return "test-policy", nil }
	assess := func(context.Context, tasks.ChildSpec, tasks.ChildReport) (string, error) { return "STOPPED", nil }
	r, e = p.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
	if e != nil {
		t.Fatal(e)
	}
	controls, e := s.Controls(controlLimits())
	if e != nil {
		t.Fatal(e)
	}
	_, e = controls.Request(ctx, token, controlRequest(t, a, token, r.Task, "CANCEL"))
	if e != nil {
		t.Fatal(e)
	}
	r, _ = s.Load(ctx, r.Task.Ref)
	r, e = p.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
	if e != nil {
		t.Fatal(e)
	}
	if r.Delegations.Children[0].Status != "CONFLICT" || r.Task.DelegatedSteps != 2 {
		t.Fatal("unknown effect treated as stopped")
	}
	q := tasks.QualificationOf(r)
	q.Version = r.Work[0].DecisionVersion
	r, e = w.Observe(ctx, tasks.Observation{ChangeID: "parent-stopped", Qualification: q, Status: "STOPPED", Evidence: "parent worker joined"})
	if e != nil {
		t.Fatal(e)
	}
	if r.Task.State != "WAITING" {
		t.Fatal("parent cancelled with unresolved child")
	}

	// New host observations acquire a durable report revision; the prior unknown
	// evidence remains in parent history and never settles budget by itself.
	unknown = false
	r, e = p.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
	if e != nil {
		t.Fatal(e)
	}
	if r.Task.State != "CANCELLED" || r.Task.DelegatedSteps != 0 || len(r.Delegations.Children[0].Reports) != 3 {
		t.Fatal("confirmed cancellation did not settle safely", r.Task.State, r.Task.DelegatedSteps, len(r.Delegations.Children[0].Reports))
	}

	intent := *r.Delegations.Children[0].Intent
	intent.Spec.OperationID = "never-submitted"
	intent.Spec.Key = "other"
	receipt, e := cp.Cancel(ctx, tasks.ChildReferenceOf(intent))
	if e != nil || receipt.Version != 0 || receipt.Outcome != "APPLIED" {
		t.Fatal(receipt, e)
	}
	if _, e = cp.Accept(ctx, intent); !authorization.Is(e, authorization.Denied) {
		t.Fatal("late acceptance escaped tombstone", e)
	}
}

func TestDelegationCancellationHasSeparateAllowance(t *testing.T) {
	s, a, token, _, w, p, r, proposal, _ := delegationParent(t)
	ctx := context.Background()
	_, ca, ct, cc, _ := setup(t)
	enableControls(t, ca, ct, cc)
	cs, e := tasks.New(ca, tasks.Config{Namespace: "local", Resource: "root", Owner: "child-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	cp, e := cs.Children(ct, "local-owner", func(context.Context, tasks.ChildIntent, string) error { return nil }, func(context.Context, tasks.RunSnapshot) (tasks.ChildEvidence, error) {
		return tasks.ChildEvidence{Source: "child-owner", Evidence: "observed", Coverage: "whole child", Effect: "NONE"}, nil
	}, func(ctx context.Context) (string, error) { return ca.NewOperation(ctx, ct) }, tasks.DelegationLimits{MaxChildren: 4, MaxDepth: 3, MaxConcurrent: 2, MaxChecks: 16, IOTimeout: time.Second}, controlLimits())
	if e != nil {
		t.Fatal(e)
	}
	r, e = p.Admit(ctx, tasks.QualificationOf(r), proposal)
	if e != nil {
		t.Fatal(e)
	}
	resolve := func(string) (tasks.ChildRemote, error) { return cp, nil }
	grant := func(context.Context, tasks.ChildIntent) (string, error) { return "test-policy", nil }
	assess := func(context.Context, tasks.ChildSpec, tasks.ChildReport) (string, error) { return "STOPPED", nil }
	for i := 0; i < 16; i++ {
		r, e = p.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
		if e != nil {
			t.Fatal(e)
		}
	}
	r, e = p.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
	if !authorization.Is(e, authorization.Unavailable) || r.Task.State != "WAITING" {
		t.Fatal("missing bounded wait", e)
	}
	controls, e := s.Controls(controlLimits())
	if e != nil {
		t.Fatal(e)
	}
	_, e = controls.Request(ctx, token, controlRequest(t, a, token, r.Task, "CANCEL"))
	if e != nil {
		t.Fatal(e)
	}
	r, _ = s.Load(ctx, r.Task.Ref)
	r, e = p.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
	if e != nil {
		t.Fatal("normal checks exhausted cancellation allowance", e)
	}
	if r.Delegations.DispositionChecks != 1 || r.Delegations.Children[0].Status != "STOPPED" {
		t.Fatal("cancel not reconciled")
	}
	q := tasks.QualificationOf(r)
	q.Version = r.Work[0].DecisionVersion
	r, e = w.Observe(ctx, tasks.Observation{ChangeID: "joined", Qualification: q, Status: "STOPPED", Evidence: "worker joined"})
	if e != nil || r.Task.State != "CANCELLED" {
		t.Fatal(r.Task.State, e)
	}
}

func TestDelegationRequiredFailureBlocksOnlyDependencies(t *testing.T) {
	s, a, token, _, _, p, r, proposal, _ := delegationParent(t)
	ctx := context.Background()
	first := proposal.Children[0]
	first.Budget.MaxSteps = 1
	proposal.Children = nil
	for _, key := range []string{"failed", "independent", "dependent", "healthy"} {
		v := first
		v.Key = key
		v.OperationID, _ = a.NewOperation(ctx, token)
		if key == "dependent" {
			v.DependsOn = []string{"failed"}
		}
		proposal.Children = append(proposal.Children, v)
	}
	_, ca, ct, cc, _ := setup(t)
	enableControls(t, ca, ct, cc)
	cs, e := tasks.New(ca, tasks.Config{Namespace: "local", Resource: "root", Owner: "child-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	cp, e := cs.Children(ct, "local-owner", func(context.Context, tasks.ChildIntent, string) error { return nil }, func(context.Context, tasks.RunSnapshot) (tasks.ChildEvidence, error) {
		return tasks.ChildEvidence{Source: "child-owner", Evidence: "observed", Coverage: "whole child", Effect: "NONE"}, nil
	}, func(ctx context.Context) (string, error) { return ca.NewOperation(ctx, ct) }, tasks.DelegationLimits{MaxChildren: 4, MaxDepth: 3, MaxConcurrent: 2, MaxChecks: 16, IOTimeout: time.Second}, controlLimits())
	if e != nil {
		t.Fatal(e)
	}
	worker, e := cp.BindWorker(tasks.WorkerBinding{Token: ct, Subject: "admin", WorkerID: "child-worker"}, limits())
	if e != nil {
		t.Fatal(e)
	}
	r, e = p.Admit(ctx, tasks.QualificationOf(r), proposal)
	if e != nil {
		t.Fatal(e)
	}
	resolve := func(string) (tasks.ChildRemote, error) { return cp, nil }
	grant := func(context.Context, tasks.ChildIntent) (string, error) { return "test-policy", nil }
	assess := func(_ context.Context, _ tasks.ChildSpec, r tasks.ChildReport) (string, error) {
		if r.State == "FAILED" {
			return "FAILED", nil
		}
		return "ACCEPTED", nil
	}
	for i := 0; i < 4; i++ {
		r, e = p.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
		if e != nil {
			t.Fatal(e)
		}
	}
	if r.Delegations.Children[3].Status != "QUEUED" {
		t.Fatal("parallel slot limit exceeded")
	}
	done := map[tasks.Ref]bool{}
	for i := 0; i < 10; i++ {
		for _, link := range r.Delegations.Children {
			if link.Intent == nil {
				continue
			}
			task, e := cp.Lookup(ctx, tasks.ChildReferenceOf(*link.Intent))
			if e != nil {
				t.Fatal(e)
			}
			if done[task.Ref] {
				continue
			}
			cr, e := cs.Load(ctx, task.Ref)
			if e != nil {
				t.Fatal(e)
			}
			for _, kind := range []string{"claim", "start"} {
				cr, e = worker.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(cr)})
				if e != nil {
					t.Fatal(e)
				}
			}
			change := tasks.WorkChange{ChangeID: "finish", Kind: "complete", Qualification: tasks.QualificationOf(cr), Proposal: tasks.Proposal{Kind: "result", BaseVersion: cr.Task.Version, Complete: true, Result: `{}`}}
			if link.Spec.Key == "failed" {
				change = tasks.WorkChange{ChangeID: "failed", Kind: "stop", Qualification: tasks.QualificationOf(cr), Finished: true, Reason: "brain_failure"}
			}
			_, e = worker.Commit(ctx, change)
			if e != nil {
				t.Fatal(e)
			}
			done[task.Ref] = true
		}
		r, e = p.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
		if e != nil {
			t.Fatal(e)
		}
		if r.Delegations.Children[0].Status == "FAILED" && r.Delegations.Children[1].Status == "ACCEPTED" && r.Delegations.Children[2].Status == "UNSENT" && r.Delegations.Children[3].Status == "ACCEPTED" {
			break
		}
	}
	if len(done) != 3 || r.Delegations.Children[2].Status != "UNSENT" || r.Delegations.Children[3].Status != "ACCEPTED" {
		t.Fatal("failure blocked independent branch", len(done))
	}
	got, e := s.Get(ctx, token, r.Task.Ref)
	if e != nil || got.State == "COMPLETED" {
		t.Fatal("required failure lost", e)
	}
}

func publicDelegationData(context.Context, tasks.Task, tasks.ChildSpec, *tasks.ChildReport) error {
	return nil
}
