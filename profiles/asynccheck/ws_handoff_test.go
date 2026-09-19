package asynccheck

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"google.golang.org/protobuf/proto"
	wsbinding "lerna/adapters/transport/ws"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"net"
	"testing"
	"time"
)

func allowHandoff(t *testing.T, h *harness) {
	ctx := context.Background()
	scope := wsScope(h, true)
	scope.Actions = append(scope.Actions, "task.handoff", "task.cancel")
	for _, cmd := range []*wire.AuthorizationCommand{{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "handoff", Scope: scope}}}}}, {Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "handoff", Subject: "operator", Scope: scope, Mode: "continuous"}}}} {
		snap, e := h.db.Load(ctx)
		mustGRPC(t, e)
		cmd.ExpectedRevision = snap.State.Revision
		op, e := h.operation(ctx)
		mustGRPC(t, e)
		_, e = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: cmd})
		mustGRPC(t, e)
	}
}
func TestWSHandoffExecutionFenceAndLateFacts(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "not-started", true: "late-result"}[started], func(t *testing.T) {
			edge, cloud := prepareWS(t)
			source, client, disk := openWS(t, edge)
			defer source.close()
			allowHandoff(t, source)
			serverHarness, host, _ := openWS(t, cloud)
			defer serverHarness.close()
			target, e := open(context.Background(), t.TempDir(), "")
			mustGRPC(t, e)
			defer target.close()
			allowHandoff(t, target)
			target.core, e = tasks.New(target.auth, tasks.Config{Namespace: "local", Resource: "root", Owner: "target-owner", MaxTasks: 100, MaxPage: 10})
			mustGRPC(t, e)
			target.work, e = target.core.BindWorker(tasks.WorkerBinding{Token: target.token, Subject: "operator", WorkerID: "worker", AllowEffectEvidence: true}, limits())
			mustGRPC(t, e)
			sp, sk, e := ed25519.GenerateKey(rand.Reader)
			mustGRPC(t, e)
			tp, tk, e := ed25519.GenerateKey(rand.Reader)
			mustGRPC(t, e)
			cfg := tasks.HandoffConfig{PrivateKey: sk, Peers: map[string]ed25519.PublicKey{"target-owner": tp}, IOTimeout: time.Second, Policy: func(context.Context, string, tasks.RunSnapshot, string) error { return nil }, Isolated: func(ctx context.Context, r tasks.RunSnapshot) error {
				e := source.auth.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error {
					return source.work.GuardExecution(tx, disk.Request.Qualification, disk.Request.OperationID, false)
				})
				if !authorization.Is(e, authorization.Unavailable) {
					return &authorization.Error{Code: authorization.Unavailable}
				}
				return nil
			}}
			sh, e := source.core.Handoffs(source.token, cfg)
			mustGRPC(t, e)
			cfg.PrivateKey = tk
			cfg.Peers = map[string]ed25519.PublicKey{"owner": sp}
			th, e := target.core.Handoffs(target.token, cfg)
			mustGRPC(t, e)
			host.Required = append(host.Required, "task.handoff.v1")
			client.Required = append(client.Required, "task.handoff.v1")
			resolve := host.Resolve
			host.Resolve = func(ctx context.Context, p authorization.GrantPresentation) (*wsbinding.Binding, error) {
				b, e := resolve(ctx, p)
				if e != nil {
					return nil, e
				}
				copy := *b
				copy.Handoffs = th
				copy.HandoffWorker = target.work
				return &copy, nil
			}
			server, e := wsbinding.NewServer(host)
			mustGRPC(t, e)
			defer server.Close()
			listener, e := net.Listen("tcp", "127.0.0.1:0")
			mustGRPC(t, e)
			go server.Serve(listener)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			peer, e := client.Dial(ctx, "wss://"+listener.Addr().String()+"/harness", "cloud")
			mustGRPC(t, e)
			defer peer.Close()
			if started {
				_, e = source.exec.Run(ctx, disk.Request.OperationID)
				mustGRPC(t, e)
				mustGRPC(t, source.exec.Drain(ctx, 16))
			}
			r, e := source.core.Load(ctx, disk.Request.Qualification.Ref)
			mustGRPC(t, e)
			op, e := source.operation(ctx)
			mustGRPC(t, e)
			_, e = sh.Begin(ctx, tasks.HandoffRequest{OperationID: op, Ref: r.Task.Ref, Target: "target-owner", ExpectedVersion: r.Task.Version})
			mustGRPC(t, e)
			offer, e := sh.Export(ctx, r.Task.Ref, op)
			mustGRPC(t, e)
			ack, e := peer.PrepareHandoff(ctx, offer)
			mustGRPC(t, e)
			activation, e := sh.Seal(ctx, ack)
			mustGRPC(t, e)
			mustGRPC(t, peer.ActivateHandoff(ctx, activation))
			mustGRPC(t, peer.ActivateHandoff(ctx, activation))
			if !started {
				_, e = source.exec.Run(ctx, disk.Request.OperationID)
				if !authorization.Is(e, authorization.Unavailable) {
					t.Fatal("sealed endpoint started", e)
				}
				truth, e := source.target.Snapshot(ctx)
				mustGRPC(t, e)
				if truth.Jobs != 0 || truth.Changes != 0 {
					t.Fatal(truth)
				}
			} else {
				mustGRPC(t, source.target.Complete(ctx, disk.Request.OperationID))
				source.clock.advance(time.Second)
				_, e = source.exec.Reconcile(ctx, disk.Request.OperationID)
				mustGRPC(t, e)
				mustGRPC(t, source.exec.Drain(ctx, 16))
				facts, e := sh.Facts(ctx, r.Task.Ref, op)
				mustGRPC(t, e)
				mustGRPC(t, peer.ForwardHandoffFacts(ctx, facts))
				mustGRPC(t, peer.ForwardHandoffFacts(ctx, facts))
				got, e := target.core.Load(ctx, r.Task.Ref)
				mustGRPC(t, e)
				if len(got.ExecutionReports) != 1 || got.Work[0].InFlight {
					t.Fatal("late effect not reconciled", got)
				}
				truth, e := source.target.Snapshot(ctx)
				mustGRPC(t, e)
				if truth.Jobs != 1 || truth.Changes != 1 {
					t.Fatal(truth)
				}
				// Synthetic report revisions exercise queue capacity, not extra effects.
				state, e := sh.Status(ctx, r.Task.Ref, op)
				mustGRPC(t, e)
				report := state.Reports[0]
				seen := map[uint64]bool{}
				for _, existing := range state.Reports {
					seen[existing.Revision] = true
				}
				for revision := uint64(1); revision <= 32; revision++ {
					if seen[revision] {
						continue
					}
					report.Revision = revision
					mustGRPC(t, source.work.ConsumeExecution(ctx, report))
					state, e = sh.Status(ctx, r.Task.Ref, op)
					mustGRPC(t, e)
				}
				report.Revision = 33
				if e = source.work.ConsumeExecution(ctx, report); !authorization.Is(e, authorization.Invalid) {
					t.Fatal("report revision overflow accepted", e)
				}
				after, e := sh.Status(ctx, r.Task.Ref, op)
				mustGRPC(t, e)
				if len(after.Reports) != 32 || after.Digest != state.Digest || after.Phase != "SEALED" {
					t.Fatal("overflow lost obligation", after)
				}
				restricted := cfg
				restricted.PrivateKey = sk
				restricted.Peers = map[string]ed25519.PublicKey{"target-owner": tp}
				restricted.Policy = func(_ context.Context, phase string, view tasks.RunSnapshot, _ string) error {
					if phase == "facts" {
						for _, report := range view.ExecutionReports {
							if report.Revision == 1 {
								return &authorization.Error{Code: authorization.Denied}
							}
						}
					}
					return nil
				}
				guarded, e := source.core.Handoffs(source.token, restricted)
				mustGRPC(t, e)
				if _, e = guarded.Facts(ctx, r.Task.Ref, op); !authorization.Is(e, authorization.Denied) {
					t.Fatal("superseded report escaped disclosure policy", e)
				}
				restricted.Policy = func(_ context.Context, phase string, view tasks.RunSnapshot, _ string) error {
					if phase == "status" {
						for _, report := range view.ExecutionReports {
							if report.Revision == 1 {
								return &authorization.Error{Code: authorization.Denied}
							}
						}
					}
					return nil
				}
				guarded, e = source.core.Handoffs(source.token, restricted)
				mustGRPC(t, e)
				if got, e := guarded.Status(ctx, r.Task.Ref, op); !authorization.Is(e, authorization.Denied) || len(got.Reports) != 0 || len(got.Checkpoint) != 0 {
					t.Fatal("status exposed restricted history", e)
				}
			}
			t.Logf("real mTLS handoff; started-before-freeze=%t; target epoch=2; original operation retained", started)
		})
	}
}

func TestWSHandoffNewParentUsesExistingChild(t *testing.T) {
	edge, cloud := prepareWS(t)
	source, oldClient, edgeDisk := openWS(t, edge)
	defer source.close()
	allowHandoff(t, source)
	child, host, cloudDisk := openWS(t, cloud)
	defer child.close()
	allowHandoff(t, child)
	next, e := open(context.Background(), t.TempDir(), "")
	mustGRPC(t, e)
	defer next.close()
	allowHandoff(t, next)
	next.core, e = tasks.New(next.auth, tasks.Config{Namespace: "local", Resource: "root", Owner: "target-owner", MaxTasks: 100, MaxPage: 10})
	mustGRPC(t, e)
	sp, sk, _ := ed25519.GenerateKey(rand.Reader)
	tp, tk, _ := ed25519.GenerateKey(rand.Reader)
	oldIdentity := authorization.GrantPresentation{Namespace: "local", Subject: "operator", Audience: "cloud", Presenter: "edge", CertificateSHA256: cloudDisk.Binding.CertificateSHA256}
	policy := tasks.SignedChildPolicy(child.grants, oldIdentity, &wire.AuthorizationAction{Resource: "root", Action: "task.execute", Purpose: "task", Location: "local"}, "owner")
	cp, e := child.core.Children(child.token, "owner", policy, func(context.Context, tasks.RunSnapshot) (tasks.ChildEvidence, error) {
		return tasks.ChildEvidence{Source: "child-store", Evidence: "no-effects", Coverage: "all", Effect: "NONE"}, nil
	}, child.operation, childLimits(), controlLimits())
	mustGRPC(t, e)
	cp, e = cp.BindPeer(oldIdentity)
	mustGRPC(t, e)
	cp, e = cp.WithParentRoutes(map[string]ed25519.PublicKey{"owner": sp, "target-owner": tp}, func(_ context.Context, _ tasks.ChildIntent, p authorization.GrantPresentation) error {
		if p.Presenter != "cloud" {
			return &authorization.Error{Code: authorization.Denied}
		}
		return nil
	})
	mustGRPC(t, e)
	host.Required = []string{"chunks.v1", "task.delegation.v1"}
	oldClient.Required = host.Required
	host.Resolve = func(ctx context.Context, p authorization.GrantPresentation) (*wsbinding.Binding, error) {
		return &wsbinding.Binding{Peer: p, Children: cp, Disclose: func(context.Context, authorization.GrantPresentation, *wire.CapabilityRequest) error { return nil }}, nil
	}
	server, e := wsbinding.NewServer(host)
	mustGRPC(t, e)
	defer server.Close()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go server.Serve(listener)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	address := "wss://" + listener.Addr().String() + "/harness"
	oldPeer, e := oldClient.Dial(ctx, address, "cloud")
	mustGRPC(t, e)
	defer oldPeer.Close()
	r, e := source.core.Load(ctx, edgeDisk.Request.Qualification.Ref)
	mustGRPC(t, e)
	dp, e := source.work.Delegations(childLimits(), func(context.Context, tasks.Task, tasks.ChildSpec, *tasks.ChildReport) error { return nil })
	mustGRPC(t, e)
	batch, e := source.operation(ctx)
	mustGRPC(t, e)
	cop, e := source.operation(ctx)
	mustGRPC(t, e)
	schema := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:handoff:child","type":"object"}`)
	spec := tasks.ChildSpec{Key: "child", OperationID: cop, Agent: "owner", Goal: "observe bounded state", Acceptance: "independent evidence", Input: []byte(`{}`), InputSchema: schema, ResultSchema: schema, Required: true, Budget: tasks.Constraints{MaxSteps: 2, DeadlineUnix: r.Task.Constraints.DeadlineUnix}}
	r, e = dp.Admit(ctx, tasks.QualificationOf(r), tasks.DelegationProposal{OperationID: batch, Children: []tasks.ChildSpec{spec}})
	mustGRPC(t, e)
	allocations := 0
	grant := func(ctx context.Context, in tasks.ChildIntent) (string, error) {
		snap, e := child.db.Load(ctx)
		if e != nil {
			return "", e
		}
		op, e := child.operation(ctx)
		if e != nil {
			return "", e
		}
		scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.execute"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: in.Spec.Budget.DeadlineUnix}
		gs := &wire.SignedGrantSpec{Subject: "operator", Audience: "cloud", Presenter: "edge", CertificateSha256: oldIdentity.CertificateSHA256, Scope: scope, NotBefore: child.now().Unix(), Mode: "continuous", Units: 2, DelegationDepth: 1}
		root, e := child.grants.Mutate(ctx, child.token, &wire.GrantMutation{OperationId: op, ExpectedRevision: snap.State.Revision, Kind: "ISSUE", Spec: gs})
		if e != nil {
			return "", e
		}
		leaf := proto.Clone(gs).(*wire.SignedGrantSpec)
		leaf.Mode = "single"
		leaf.Units = 1
		leaf.DelegationDepth = 0
		leaf.OperationBinding = in.Spec.OperationID
		leaf.SemanticSha256 = tasks.DelegationDigest(in)
		op, e = child.operation(ctx)
		if e != nil {
			return "", e
		}
		receipt, e := child.grants.Mutate(ctx, child.token, &wire.GrantMutation{OperationId: op, ExpectedRevision: root.Revision, Kind: "DERIVE", GrantId: root.GrantId, ExpectedGrantRevision: root.Revision, Spec: leaf})
		if e != nil {
			return "", e
		}
		allocations++
		return receipt.Material, nil
	}
	r, e = dp.Advance(ctx, tasks.QualificationOf(r), func(string) (tasks.ChildRemote, error) { return oldPeer, nil }, grant, func(context.Context, tasks.ChildSpec, tasks.ChildReport) (string, error) { return "ACCEPTED", nil })
	mustGRPC(t, e)
	original := r.Delegations.Children[0]
	_, e = oldPeer.Accept(ctx, *original.Intent)
	mustGRPC(t, e)
	ref := tasks.ChildReferenceOf(*original.Intent)
	cfg := tasks.HandoffConfig{PrivateKey: sk, Peers: map[string]ed25519.PublicKey{"target-owner": tp}, IOTimeout: time.Second, Policy: func(context.Context, string, tasks.RunSnapshot, string) error { return nil }, Isolated: func(ctx context.Context, r tasks.RunSnapshot) error {
		e := source.auth.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error {
			return source.work.GuardExecution(tx, edgeDisk.Request.Qualification, edgeDisk.Request.OperationID, false)
		})
		if !authorization.Is(e, authorization.Unavailable) {
			return &authorization.Error{Code: authorization.Unavailable}
		}
		return nil
	}}
	sh, e := source.core.Handoffs(source.token, cfg)
	mustGRPC(t, e)
	cfg.PrivateKey = tk
	cfg.Peers = map[string]ed25519.PublicKey{"owner": sp}
	th, e := next.core.Handoffs(next.token, cfg)
	mustGRPC(t, e)
	op, e := source.operation(ctx)
	mustGRPC(t, e)
	_, e = sh.Begin(ctx, tasks.HandoffRequest{OperationID: op, Ref: r.Task.Ref, Target: "target-owner", ExpectedVersion: r.Task.Version})
	mustGRPC(t, e)
	offer, e := sh.Export(ctx, r.Task.Ref, op)
	mustGRPC(t, e)
	ack, e := th.Prepare(ctx, offer)
	mustGRPC(t, e)
	activation, e := sh.Seal(ctx, ack)
	mustGRPC(t, e)
	_, e = th.Activate(ctx, activation)
	mustGRPC(t, e)
	newIdentity := authorization.GrantPresentation{Namespace: "local", Subject: "operator", Audience: "cloud", Presenter: "cloud", CertificateSHA256: edgeDisk.Binding.CertificateSHA256}
	route, e := th.Route(ctx, r.Task.Ref, op, newIdentity)
	mustGRPC(t, e)
	// A different actual mTLS presenter now contacts the same child server.
	newPeer, e := host.Dial(ctx, address, "cloud")
	mustGRPC(t, e)
	defer newPeer.Close()
	if _, e = newPeer.Lookup(ctx, ref); !authorization.Is(e, authorization.Denied) {
		t.Fatal("new parent without proof", e)
	}
	routed := newPeer.WithParentRoute(route)
	got, e := routed.Lookup(ctx, ref)
	mustGRPC(t, e)
	if got.Ref != original.Child {
		t.Fatal("new child")
	}
	if _, e = oldPeer.Accept(ctx, *original.Intent); !authorization.Is(e, authorization.Denied) {
		t.Fatal("former parent replay exposed child", e)
	}
	_, e = routed.Cancel(ctx, ref)
	mustGRPC(t, e)
	report, e := routed.Report(ctx, ref)
	mustGRPC(t, e)
	if report.Child != original.Child || report.State != "CANCELLED" || allocations != 1 {
		t.Fatal("lost child responsibility", report, allocations)
	}
	if _, e = oldPeer.Cancel(ctx, ref); !authorization.Is(e, authorization.Denied) {
		t.Fatal("former parent still controls child", e)
	}
	t.Log("edge parent -> cloud parent; exact activated-owner route; same signed leaf and child; cancellation confirmed; allocations=1")
}
