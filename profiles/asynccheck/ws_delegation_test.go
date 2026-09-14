package asynccheck

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/wsbinding"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func childLimits() tasks.DelegationLimits {
	return tasks.DelegationLimits{MaxChildren: 4, MaxDepth: 3, MaxConcurrent: 2, MaxChecks: 16, IOTimeout: time.Second}
}
func wsChild(t *testing.T, h *harness, p authorization.GrantPresentation) *tasks.ChildPort {
	t.Helper()
	policy := tasks.SignedChildPolicy(h.grants, p, &wire.AuthorizationAction{Resource: "root", Action: "task.execute", Purpose: "task", Location: "local"}, "owner")
	c, e := h.core.Children(h.token, "parent-owner", policy, func(_ context.Context, r tasks.RunSnapshot) (tasks.ChildEvidence, error) {
		return tasks.ChildEvidence{Source: "cloud-artifact", Evidence: "counter-value-0", Coverage: "synthetic independent observation", Effect: "NONE"}, nil
	}, h.operation, childLimits(), controlLimits())
	mustGRPC(t, e)
	c, e = c.BindPeer(p)
	mustGRPC(t, e)
	return c
}
func TestWSDelegationProcess(t *testing.T) {
	dir := os.Getenv("HARNESS_CHILD_DIR")
	if dir == "" {
		t.Skip("child only")
	}
	h, host, _ := openWS(t, dir)
	host.Required = append(host.Required, "task.delegation.v1")
	defer h.close()
	raw, e := os.ReadFile(filepath.Join(dir, "delegation.json"))
	mustGRPC(t, e)
	var in tasks.ChildIntent
	mustGRPC(t, json.Unmarshal(raw, &in))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	peer, e := host.Dial(ctx, os.Getenv("HARNESS_CHILD_ADDRESS"), "cloud")
	mustGRPC(t, e)
	accepted, e := peer.Accept(ctx, in)
	mustGRPC(t, e)
	// Exit without Close after peer acknowledgment; the next incarnation must
	// recover the same child from a fresh message containing the original intent.
	t.Logf("accepted child=%s owner=%s", accepted.Ref.TaskID, accepted.Owner)
	if os.Getenv("HARNESS_CHILD_CRASH") == "1" {
		os.Exit(78)
	}
	got, e := peer.Lookup(ctx, tasks.ChildReferenceOf(in))
	mustGRPC(t, e)
	if got.Ref != accepted.Ref {
		t.Fatal("duplicate child after restart")
	}
	report, e := peer.Report(ctx, tasks.ChildReferenceOf(in))
	mustGRPC(t, e)
	if report.Child != got.Ref || report.Parent != in.Parent || report.OperationID != in.Spec.OperationID {
		t.Fatal("lost association")
	}
	changed := in
	changed.Spec.Goal = "changed after signed narrowing"
	if _, e = peer.Accept(ctx, changed); !authorization.Is(e, authorization.Denied) {
		t.Fatal("changed signed semantics accepted", e)
	}
	peer.Close()
}
func TestWSDelegationSignedRestart(t *testing.T) {
	edge, cloud := prepareWS(t)
	h, host, disk := openWS(t, cloud)
	host.Required = append(host.Required, "task.delegation.v1")
	defer h.close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	p := authorization.GrantPresentation{Namespace: "local", Subject: "operator", Audience: "cloud", Presenter: "edge", CertificateSHA256: disk.Binding.CertificateSHA256}
	c := wsChild(t, h, p)
	resolve := host.Resolve
	host.Resolve = func(ctx context.Context, p authorization.GrantPresentation) (*wsbinding.Binding, error) {
		b, e := resolve(ctx, p)
		if e != nil {
			return nil, e
		}
		copy := *b
		copy.Children = c
		return &copy, nil
	}
	server, e := wsbinding.NewServer(host)
	mustGRPC(t, e)
	defer server.Close()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go server.Serve(listener)
	schema := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:child:value","type":"object"}`)
	in := tasks.ChildIntent{Parent: tasks.Ref{Namespace: "local", TaskID: "parent"}, ParentOwner: "parent-owner", ParentEpoch: 1, Depth: 1, Ancestors: []tasks.Ref{{Namespace: "local", TaskID: "parent"}}, Spec: tasks.ChildSpec{Key: "a", OperationID: "parent-operation", Agent: "owner", Goal: "read synthetic counter", Acceptance: "independent state check", Input: []byte(`{}`), InputSchema: schema, ResultSchema: schema, Required: true, Budget: tasks.Constraints{MaxSteps: 2, DeadlineUnix: h.now().Add(time.Minute).Unix()}}}
	// Both derivation and its reply are stable operations in the existing grant
	// ledger; runtime budgets are a separate child-authority ledger.
	snap, e := h.db.Load(ctx)
	mustGRPC(t, e)
	op, e := h.operation(ctx)
	mustGRPC(t, e)
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.execute"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: in.Spec.Budget.DeadlineUnix}
	spec := &wire.SignedGrantSpec{Subject: "operator", Audience: "cloud", Presenter: "edge", CertificateSha256: p.CertificateSHA256, Scope: scope, NotBefore: h.now().Unix(), Mode: "continuous", Units: 2, DelegationDepth: 1}
	root, e := h.grants.Mutate(ctx, h.token, &wire.GrantMutation{OperationId: op, ExpectedRevision: snap.State.Revision, Kind: "ISSUE", Spec: spec})
	mustGRPC(t, e)
	op, e = h.operation(ctx)
	mustGRPC(t, e)
	leaf := proto.Clone(spec).(*wire.SignedGrantSpec)
	leaf.Mode = "single"
	leaf.Units = 1
	leaf.DelegationDepth = 0
	leaf.OperationBinding = in.Spec.OperationID
	leaf.SemanticSha256 = tasks.DelegationDigest(in)
	mutation := &wire.GrantMutation{OperationId: op, ExpectedRevision: root.Revision, Kind: "DERIVE", GrantId: root.GrantId, ExpectedGrantRevision: root.Revision, Spec: leaf}
	receipt, e := h.grants.Mutate(ctx, h.token, mutation)
	mustGRPC(t, e)
	in.GrantMaterial = receipt.Material
	replay, e := h.grants.Mutate(ctx, h.token, mutation)
	mustGRPC(t, e)
	if replay.GrantId != receipt.GrantId {
		t.Fatal("second quota on retry")
	}
	privateJSON(t, filepath.Join(edge, "delegation.json"), in)
	for _, crash := range []bool{true, false} {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWSDelegationProcess$", "-test.v")
		cmd.Env = append(os.Environ(), "HARNESS_CHILD_DIR="+edge, "HARNESS_CHILD_ADDRESS=wss://"+listener.Addr().String()+"/harness")
		if crash {
			cmd.Env = append(cmd.Env, "HARNESS_CHILD_CRASH=1")
		}
		out, e := cmd.CombinedOutput()
		t.Log(string(out))
		if crash {
			exit, ok := e.(*exec.ExitError)
			if !ok || exit.ExitCode() != 78 {
				t.Fatalf("missing abrupt exit: %v", e)
			}
		} else {
			mustGRPC(t, e)
		}
	}
	record, e := h.grants.Get(ctx, h.token, root.GrantId)
	mustGRPC(t, e)
	if record.Allocated != 1 {
		t.Fatal("quota duplicated", record.Allocated)
	}
	// Current revocation also blocks a previously admitted child from starting.
	task, e := c.Lookup(ctx, tasks.ChildReferenceOf(in))
	mustGRPC(t, e)
	r, e := h.core.Load(ctx, task.Ref)
	mustGRPC(t, e)
	worker, e := c.BindWorker(tasks.WorkerBinding{Token: h.token, Subject: "operator", WorkerID: "child-worker", AllowEffectEvidence: true}, limits())
	mustGRPC(t, e)

	// Admit a separately authorized tool while delegation is valid, then revoke
	// only delegation authority. No optional StartGuard is installed.
	for _, kind := range []string{"claim", "start"} {
		r, e = worker.Commit(ctx, tasks.WorkChange{ChangeID: "child-" + kind, Kind: kind, Qualification: tasks.QualificationOf(r)})
		mustGRPC(t, e)
	}

	descendant, e := worker.Delegations(childLimits(), func(context.Context, tasks.Task, tasks.ChildSpec, *tasks.ChildReport) error { return nil })
	mustGRPC(t, e)
	if _, e = descendant.Admit(ctx, tasks.QualificationOf(r), tasks.DelegationProposal{OperationID: "descendant", Children: []tasks.ChildSpec{in.Spec}}); !authorization.Is(e, authorization.Denied) {
		t.Fatal("leaf grant delegated onward", e)
	}
	input, e := h.put(ctx, []byte(`{"delta":3}`))
	mustGRPC(t, e)
	toolOp, e := h.operation(ctx)
	mustGRPC(t, e)
	toolRequest := execution.Request{OperationID: toolOp, Qualification: tasks.QualificationOf(r), Capability: h.cap.Name, Version: h.cap.Version, Implementation: h.cap.Implementation, ImplementationVersion: h.cap.ImplementationVersion, DescriptorSHA256: h.cap.Digest(), InputRef: input, ResourceVersion: 1}
	toolMaterial, e := h.issue(ctx, toolRequest)
	mustGRPC(t, e)
	executor, e := execution.New(h.grants, worker, h.access, h.target, h.binding, h.cap, config(), h.operation)
	mustGRPC(t, e)
	_, e = executor.Invoke(ctx, toolRequest, toolMaterial)
	mustGRPC(t, e)
	state, e := h.db.Load(ctx)
	mustGRPC(t, e)
	op, e = h.operation(ctx)
	mustGRPC(t, e)
	_, e = h.grants.Mutate(ctx, h.token, &wire.GrantMutation{OperationId: op, ExpectedRevision: state.State.Revision, Kind: "REVOKE", GrantId: root.GrantId, ExpectedGrantRevision: record.Revision})
	mustGRPC(t, e)
	if _, e = worker.Commit(ctx, tasks.WorkChange{ChangeID: "revoked-start", Kind: "claim", Qualification: tasks.QualificationOf(r)}); !authorization.Is(e, authorization.Denied) {
		t.Fatal("revoked parent grant reused", e)
	}

	if _, e = executor.Run(ctx, toolOp); !authorization.Is(e, authorization.Denied) {
		t.Fatal("revoked child started an already admitted tool", e)
	}
	stored, e := executor.GetInvocation(ctx, toolOp)
	mustGRPC(t, e)
	if stored.Started {
		t.Fatal("effect start boundary crossed")
	}
	t.Log("mTLS child handoff; abrupt client exit 78; same original child after restart; narrowed allocation=1; current revocation denied start")
}
