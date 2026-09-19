package tasks_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	sqliteauth "lerna/adapters/authorization/sqlite"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestHandoffPreparingStopsSource(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableHandoff(t, a, token, c)
	ctx := context.Background()
	task, e := s.Submit(ctx, token, submission(t, a, token, c))
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Load(ctx, task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	h, e := s.Handoffs(token, publicHandoffConfig())
	if e != nil {
		t.Fatal(e)
	}
	op, e := a.NewOperation(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	request := tasks.HandoffRequest{OperationID: op, Ref: task.Ref, Target: "target-owner", ExpectedVersion: r.Task.Version}
	state, e := h.Begin(ctx, request)
	if e != nil {
		t.Fatal(e)
	}
	if state.Phase != "PREPARING" {
		t.Fatal(state)
	}
	w := workerPort(t, s, token, "worker")
	if _, e = w.Commit(ctx, tasks.WorkChange{ChangeID: "old-claim", Kind: "claim", Qualification: tasks.QualificationOf(r)}); !authorization.Is(e, authorization.Unavailable) {
		t.Fatal("preparing source claimed", e)
	}
	unrelated, e := s.Submit(ctx, token, submission(t, a, token, c))
	if e != nil {
		t.Fatal("unrelated admission frozen", e)
	}
	other, e := s.Load(ctx, unrelated.Ref)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = w.Commit(ctx, tasks.WorkChange{ChangeID: "unrelated-claim", Kind: "claim", Qualification: tasks.QualificationOf(other)}); e != nil {
		t.Fatal("unrelated task frozen", e)
	}
	if _, e = h.Begin(ctx, request); e != nil {
		t.Fatal("retry", e)
	}
	if e = h.Abort(ctx, task.Ref, op); e != nil {
		t.Fatal(e)
	}
	if _, e = w.Commit(ctx, tasks.WorkChange{ChangeID: "new-claim", Kind: "claim", Qualification: tasks.QualificationOf(r)}); e != nil {
		t.Fatal("safe abort", e)
	}
}

func TestHandoffSealedActivationAndOriginalAdmission(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableHandoff(t, a, token, c)
	_, ta, tt, tc, _ := setup(t)
	enableHandoff(t, ta, tt, tc)
	target, e := tasks.New(ta, tasks.Config{Namespace: "local", Resource: "root", Owner: "target-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	sp, sk, _ := ed25519.GenerateKey(rand.Reader)
	tp, tk, _ := ed25519.GenerateKey(rand.Reader)
	isolated := false
	cfg := tasks.HandoffConfig{PrivateKey: sk, Peers: map[string]ed25519.PublicKey{"target-owner": tp}, IOTimeout: time.Second, Policy: func(context.Context, string, tasks.RunSnapshot, string) error { return nil }, Isolated: func(context.Context, tasks.RunSnapshot) error {
		if !isolated {
			return &authorization.Error{Code: authorization.Unavailable}
		}
		return nil
	}}
	source, _ := s.Handoffs(token, cfg)
	cfg.PrivateKey = tk
	cfg.Peers = map[string]ed25519.PublicKey{"local-owner": sp}
	dest, _ := target.Handoffs(tt, cfg)
	ctx := context.Background()
	sub := submission(t, a, token, c)
	task, e := s.Submit(ctx, token, sub)
	if e != nil {
		t.Fatal(e)
	}
	op, _ := a.NewOperation(ctx, token)
	_, e = source.Begin(ctx, tasks.HandoffRequest{OperationID: op, Ref: task.Ref, Target: "target-owner", ExpectedVersion: task.Version})
	if e != nil {
		t.Fatal(e)
	}
	offer, e := source.Export(ctx, task.Ref, op)
	if e != nil {
		t.Fatal(e)
	}
	ack, e := dest.Prepare(ctx, offer)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = target.Load(ctx, task.Ref); !authorization.Is(e, authorization.NotFound) {
		t.Fatal("prepared task visible as runnable", e)
	}
	if e = source.Abort(ctx, task.Ref, op); e != nil {
		t.Fatal(e)
	}
	cancelled, e := source.Aborted(ctx, task.Ref, op)
	if e != nil {
		t.Fatal(e)
	}
	if e = dest.DiscardPrepared(ctx, cancelled); e != nil {
		t.Fatal(e)
	}
	op, _ = a.NewOperation(ctx, token)
	if _, e = source.Begin(ctx, tasks.HandoffRequest{OperationID: op, Ref: task.Ref, Target: "target-owner", ExpectedVersion: task.Version}); e != nil {
		t.Fatal(e)
	}
	offer, e = source.Export(ctx, task.Ref, op)
	if e != nil {
		t.Fatal(e)
	}
	ack, e = dest.Prepare(ctx, offer)
	if e != nil {
		t.Fatal("cannot retry aborted target", e)
	}
	if _, e = source.Seal(ctx, ack); !authorization.Is(e, authorization.Unavailable) {
		t.Fatal("unproven isolation", e)
	}
	isolated = true
	activation, e := source.Seal(ctx, ack)
	if e != nil {
		t.Fatal(e)
	}
	if e = source.Abort(ctx, task.Ref, op); !authorization.Is(e, authorization.Conflict) {
		t.Fatal("sealed source aborted", e)
	}
	for i := 0; i < 2; i++ {
		h, e := dest.Activate(ctx, activation)
		if e != nil || h.Phase != "ACTIVE" {
			t.Fatal(h, e)
		}
	}
	r, e := target.Load(ctx, task.Ref)
	if e != nil || r.Task.OwnerEpoch != 2 || r.Task.Owner != "target-owner" {
		t.Fatal(r, e)
	}
	if got, e := target.Submit(ctx, tt, sub); e != nil || got.Ref != task.Ref {
		t.Fatal("original admission lost", got, e)
	}
	if _, e = workerPort(t, target, tt, "new-worker").Commit(ctx, tasks.WorkChange{ChangeID: "target-claim", Kind: "claim", Qualification: tasks.QualificationOf(r)}); e != nil {
		t.Fatal("target cannot claim", e)
	}
	if _, e = source.Seal(ctx, ack); e != nil {
		t.Fatal("lost activation reply recovery", e)
	}
	if e = dest.DiscardPrepared(ctx, cancelled); e == nil {
		t.Fatal("old abort discarded active decision")
	}
	original, _ := a.NewOperation(ctx, token)
	request, e := dest.OriginalRequest(ctx, task.Ref, op, original)
	if e != nil {
		t.Fatal(e)
	}
	admission, e := source.ValidateOriginal(ctx, request)
	if e != nil {
		t.Fatal("open original window", e)
	}
	if e = dest.ImportOriginal(ctx, admission); e != nil {
		t.Fatal(e)
	}
	other, e := target.Submit(ctx, tt, submission(t, ta, tt, tc))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = dest.Begin(ctx, tasks.HandoffRequest{OperationID: original, Ref: other.Ref, Target: "third-owner", ExpectedVersion: other.Version}); !authorization.Is(e, authorization.Denied) {
		t.Fatal("forwarded identity escaped to another handoff", e)
	}
	r, e = target.Load(ctx, task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	controls, e := target.Controls(controlLimits())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = controls.Request(ctx, tt, tasks.ControlRequest{OperationID: original, Ref: task.Ref, ExpectedVersion: r.Task.Version, Intent: "PAUSE"}); e != nil {
		t.Fatal("open-window forwarded control", e)
	}
	bad := sub
	bad.OperationID = original
	if _, e = target.Submit(ctx, tt, bad); !authorization.Is(e, authorization.Denied) {
		t.Fatal("forwarded identity escaped task scope", e)
	}
	unopened, _ := a.NewOperation(ctx, token)
	c.now = c.now.Add(2 * time.Minute)
	if _, e = a.NewOperation(ctx, token); e != nil {
		t.Fatal(e)
	}
	request, e = dest.OriginalRequest(ctx, task.Ref, op, unopened)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = source.ValidateOriginal(ctx, request); !authorization.Is(e, authorization.Expired) {
		t.Fatal("closed original window reopened", e)
	}
	tampered := activation
	tampered.Body = append([]byte(nil), activation.Body...)
	tampered.Body[5] ^= 1
	if _, e = dest.Activate(ctx, tampered); e == nil {
		t.Fatal("tampered activation")
	}
}

func publicHandoffConfig() tasks.HandoffConfig {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	return tasks.HandoffConfig{PrivateKey: key, IOTimeout: time.Second, Policy: func(context.Context, string, tasks.RunSnapshot, string) error { return nil }, Isolated: func(context.Context, tasks.RunSnapshot) error { return nil }}
}
func enableHandoff(t *testing.T, a *authorization.Service, token string, c *clock) {
	enableExecution(t, a, token, c)
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read", "task.execute", "task.handoff", "task.reconcile", "task.pause"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: c.now.Add(time.Hour).Unix()}
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: 4, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "tasks", Scope: scope}}}}})
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: 5, Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "handoff", Subject: "admin", Scope: scope, Mode: "continuous"}}})
}

func TestHandoffQuarantinedBackupCannotStart(t *testing.T) {
	s, a, token, c, path := setup(t)
	enableHandoff(t, a, token, c)
	ctx := context.Background()
	task, e := s.Submit(ctx, token, submission(t, a, token, c))
	if e != nil {
		t.Fatal(e)
	}
	db, e := sqliteauth.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	before, e := db.Load(ctx)
	if e != nil {
		t.Fatal(e)
	}
	h, e := s.Handoffs(token, publicHandoffConfig())
	if e != nil {
		t.Fatal(e)
	}
	op, _ := a.NewOperation(ctx, token)
	_, e = h.Begin(ctx, tasks.HandoffRequest{OperationID: op, Ref: task.Ref, Target: "target-owner", ExpectedVersion: task.Version})
	if e != nil {
		t.Fatal(e)
	}
	cloned, e := sqliteauth.Open(filepath.Join(t.TempDir(), "restored.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer cloned.Close()
	if e = cloned.Commit(ctx, 0, before.State); e != nil {
		t.Fatal(e)
	}
	ca, e := authorization.New(cloned, c, authConfig())
	if e != nil {
		t.Fatal(e)
	}
	cs, e := tasks.New(ca, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	isolated := cs.Quarantine()
	r, e := isolated.Load(ctx, task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = workerPort(t, isolated, token, "clone").Commit(ctx, tasks.WorkChange{Kind: "claim", ChangeID: "clone", Qualification: tasks.QualificationOf(r)}); !authorization.Is(e, authorization.Unavailable) {
		t.Fatal("backup started", e)
	}
	_, e = isolated.VerifyRecovery(ctx, token, func(ctx context.Context, r tasks.RunSnapshot) error {
		h, e := h.Status(ctx, r.Task.Ref, op)
		if e != nil {
			return e
		}
		if h.Phase != "ACTIVE" {
			return &authorization.Error{Code: authorization.Denied}
		}
		return nil
	})
	if !authorization.Is(e, authorization.Denied) {
		t.Fatal("old epoch self-certified", e)
	}
}

func addHandoffPermission(t *testing.T, a *authorization.Service, token string, c *clock) {
	v, e := a.GetPolicy(context.Background(), token)
	if e != nil {
		t.Fatal(e)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.handoff"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: c.now.Add(time.Hour).Unix()}
	rules := append(v.Rules, &wire.PolicyRule{Id: "handoff-permission", Scope: scope})
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: v.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: rules}}})
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: v.Revision + 1, Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "handoff-permission", Subject: "admin", Scope: scope, Mode: "continuous"}}})
}
func TestHandoffParentKeepsChildAndBudget(t *testing.T) {
	s, a, token, c, _, dp, r, proposal, _ := delegationParent(t)
	addHandoffPermission(t, a, token, c)
	ctx := context.Background()
	r, e := dp.Admit(ctx, tasks.QualificationOf(r), proposal)
	if e != nil {
		t.Fatal(e)
	}
	_, ca, ct, cc, _ := setup(t)
	enableControls(t, ca, ct, cc)
	child, e := tasks.New(ca, tasks.Config{Namespace: "local", Resource: "root", Owner: "child-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	cp, e := child.Children(ct, "local-owner", func(context.Context, tasks.ChildIntent, string) error { return nil }, func(context.Context, tasks.RunSnapshot) (tasks.ChildEvidence, error) {
		return tasks.ChildEvidence{Source: "child", Evidence: "independent", Coverage: "all", Effect: "NONE"}, nil
	}, func(ctx context.Context) (string, error) { return ca.NewOperation(ctx, ct) }, tasks.DelegationLimits{MaxChildren: 4, MaxDepth: 3, MaxConcurrent: 2, MaxChecks: 16, IOTimeout: time.Second}, controlLimits())
	if e != nil {
		t.Fatal(e)
	}
	resolve := func(string) (tasks.ChildRemote, error) { return cp, nil }
	grant := func(context.Context, tasks.ChildIntent) (string, error) { return "public-fixture", nil }
	assess := func(context.Context, tasks.ChildSpec, tasks.ChildReport) (string, error) { return "ACTIVE", nil }
	r, e = dp.Advance(ctx, tasks.QualificationOf(r), resolve, grant, assess)
	if e != nil {
		t.Fatal(e)
	}
	original := r.Delegations.Children[0]
	_, ta, tt, tc, _ := setup(t)
	enableHandoff(t, ta, tt, tc)
	target, e := tasks.New(ta, tasks.Config{Namespace: "local", Resource: "root", Owner: "target-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	sp, sk, _ := ed25519.GenerateKey(rand.Reader)
	tp, tk, _ := ed25519.GenerateKey(rand.Reader)
	cfg := publicHandoffConfig()
	cfg.PrivateKey = sk
	cfg.Peers = map[string]ed25519.PublicKey{"target-owner": tp}
	sh, _ := s.Handoffs(token, cfg)
	cfg.PrivateKey = tk
	cfg.Peers = map[string]ed25519.PublicKey{"local-owner": sp}
	th, _ := target.Handoffs(tt, cfg)
	op, _ := a.NewOperation(ctx, token)
	_, e = sh.Begin(ctx, tasks.HandoffRequest{OperationID: op, Ref: r.Task.Ref, Target: "target-owner", ExpectedVersion: r.Task.Version})
	if e != nil {
		t.Fatal(e)
	}
	offer, e := sh.Export(ctx, r.Task.Ref, op)
	if e != nil {
		t.Fatal(e)
	}
	ack, e := th.Prepare(ctx, offer)
	if e != nil {
		t.Fatal(e)
	}
	active, e := sh.Seal(ctx, ack)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = th.Activate(ctx, active); e != nil {
		t.Fatal(e)
	}
	next, e := target.Load(ctx, r.Task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(next.Delegations, r.Delegations) || next.Task.DelegatedSteps != 2 {
		t.Fatal("delegation snapshot or reservation changed")
	}
	nd, e := workerPort(t, target, tt, "parent-worker").Delegations(r.Delegations.Limits, publicDelegationData)
	if e != nil {
		t.Fatal(e)
	}
	next, e = nd.Advance(ctx, tasks.QualificationOf(next), resolve, grant, assess)
	if e != nil {
		t.Fatal(e)
	}
	if next.Delegations.Children[0].Child != original.Child || next.Task.DelegatedSteps != 2 {
		t.Fatal("new child or budget allocation")
	}
	page, e := child.ListRecoverable(ctx, tasks.RecoveryQuery{Namespace: "local", Limit: 10})
	if e != nil || len(page.Runs) != 1 {
		t.Fatal("duplicate child", e)
	}
}

func TestHandoffContinuesWithPauseAndBounds(t *testing.T) {
	ctx := context.Background()
	owners := []string{"local-owner", "second-owner", "third-owner"}
	cores := make([]*tasks.Service, 3)
	auths := make([]*authorization.Service, 3)
	tokens := make([]string, 3)
	ports := make([]*tasks.HandoffPort, 3)
	keys := make([]ed25519.PrivateKey, 3)
	peers := map[string]ed25519.PublicKey{}
	var initial tasks.Submission
	for i, owner := range owners {
		_, a, token, c, _ := setup(t)
		enableHandoff(t, a, token, c)
		core, e := tasks.New(a, tasks.Config{Namespace: "local", Resource: "root", Owner: owner, MaxTasks: 100, MaxPage: 10})
		if e != nil {
			t.Fatal(e)
		}
		cores[i], auths[i], tokens[i] = core, a, token
		pub, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		peers[owner], keys[i] = pub, key
		if i == 0 {
			initial = submission(t, a, token, c)
		}
	}
	denied := false
	for i := range cores {
		cfg := publicHandoffConfig()
		cfg.PrivateKey, cfg.Peers = keys[i], peers
		cfg.Policy = func(ctx context.Context, phase string, _ tasks.RunSnapshot, _ string) error {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("unbounded policy")
			}
			if denied && phase == "activate" {
				return &authorization.Error{Code: authorization.Denied}
			}
			return nil
		}
		var e error
		ports[i], e = cores[i].Handoffs(tokens[i], cfg)
		if e != nil {
			t.Fatal(e)
		}
		cfg.IOTimeout = 6 * time.Second
		if _, e = cores[i].Handoffs(tokens[i], cfg); !authorization.Is(e, authorization.Invalid) {
			t.Fatal("unbounded I/O", e)
		}
	}
	task, e := cores[0].Submit(ctx, tokens[0], initial)
	if e != nil {
		t.Fatal(e)
	}
	cp, e := cores[0].Controls(controlLimits())
	if e != nil {
		t.Fatal(e)
	}
	pause := controlRequest(t, auths[0], tokens[0], task, "PAUSE")
	if _, e = cp.Request(ctx, tokens[0], pause); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		r, e := cores[i].Load(ctx, task.Ref)
		if e != nil {
			t.Fatal(e)
		}
		op, e := auths[i].NewOperation(ctx, tokens[i])
		if e != nil {
			t.Fatal(e)
		}
		_, e = ports[i].Begin(ctx, tasks.HandoffRequest{OperationID: op, Ref: task.Ref, Target: owners[i+1], ExpectedVersion: r.Task.Version})
		if e != nil {
			t.Fatal(e)
		}
		offer, e := ports[i].Export(ctx, task.Ref, op)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = ports[i+1].Prepare(ctx, tasks.HandoffEnvelope{Body: make([]byte, (3<<20)+1)}); !authorization.Is(e, authorization.Invalid) {
			t.Fatal("oversized envelope", e)
		}
		replayed, e := ports[i].Export(ctx, task.Ref, op)
		if e != nil || !reflect.DeepEqual(offer, replayed) {
			t.Fatal("checkpoint bytes changed on replay", e)
		}
		ack, e := ports[i+1].Prepare(ctx, offer)
		if e != nil {
			t.Fatal(e)
		}
		activation, e := ports[i].Seal(ctx, ack)
		if e != nil {
			t.Fatal(e)
		}
		denied = true
		if _, e = ports[i+1].Activate(ctx, activation); !authorization.Is(e, authorization.Denied) {
			t.Fatal("current disclosure policy ignored", e)
		}
		denied = false
		if _, e = ports[i+1].Activate(ctx, activation); e != nil {
			t.Fatal(e)
		}
		got, e := cores[i+1].Load(ctx, task.Ref)
		if e != nil || got.Task.OwnerEpoch != uint64(i+2) || got.Task.Control.Intent != "PAUSE" || !reflect.DeepEqual(r.Task.Constraints, got.Task.Constraints) || !reflect.DeepEqual(r.Records, got.Records) {
			t.Fatal("lost recovered state", got, e)
		}
		controls, e := cores[i+1].Controls(controlLimits())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = controls.Request(ctx, tokens[i+1], pause); e != nil {
			t.Fatal("original control lost", e)
		}
		if got, e := cores[i+1].Submit(ctx, tokens[i+1], initial); e != nil || got.Ref != task.Ref {
			t.Fatal("original submission lost", got, e)
		}
	}
}
