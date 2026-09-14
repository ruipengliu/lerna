package asynccheck

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/wsbinding"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/internal/executionwire"
	"lerna/sdk"
	"lerna/tasks"
	"net"
	"testing"
	"time"
)

func reliableConfig() wsbinding.JournalConfig {
	return wsbinding.JournalConfig{Records: 64, Bytes: 262144, Reorder: 8, Attempts: 3, Retention: time.Minute}
}
func reliableHost(t *testing.T, h *harness, host wsbinding.Host) wsbinding.Host {
	return reliableHostConfig(t, h, host, reliableConfig())
}
func reliableHostConfig(t *testing.T, h *harness, host wsbinding.Host, config wsbinding.JournalConfig) wsbinding.Host {
	resolve := host.Resolve
	var journal *wsbinding.Journal
	host.Required = append(host.Required, "reliable.v1")
	host.Resolve = func(ctx context.Context, p authorization.GrantPresentation) (*wsbinding.Binding, error) {
		b, e := resolve(ctx, p)
		if e != nil {
			return nil, e
		}
		if journal == nil {
			journal, e = wsbinding.NewJournal(h.auth, p.Namespace+"/"+p.Subject+"/"+p.Presenter, config)
			if e != nil {
				return nil, e
			}
		}
		copy := *b
		copy.Journal = journal
		copy.Retain = func(ctx context.Context, p authorization.GrantPresentation, r *wire.CapabilityRequest) error {
			v, e := h.auth.ViewActions(ctx, h.token, []*wire.AuthorizationAction{{Resource: "root", Action: "content.store", Purpose: "task", Location: "local"}, {Resource: "root", Action: "content.retain", Purpose: "task", Location: "local"}})
			if e != nil {
				return e
			}
			if v.Identity.Subject != p.Subject || !v.Allowed[0] || !v.Allowed[1] {
				return &authorization.Error{Code: authorization.Denied}
			}
			return nil
		}
		return &copy, nil
	}
	return host
}
func TestWSReliableInvocation(t *testing.T) {
	edge, cloud := prepareWS(t)
	a, ah, _ := openWS(t, edge)
	defer a.close()
	b, bh, _ := openWS(t, cloud)
	defer b.close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, material, e := b.request(ctx)
	mustGRPC(t, e)
	ah = reliableHost(t, a, ah)
	bh = reliableHost(t, b, bh)
	s, e := wsbinding.NewServer(bh)
	mustGRPC(t, e)
	defer s.Close()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go s.Serve(l)
	p, e := ah.Dial(ctx, "wss://"+l.Addr().String()+"/harness", "cloud")
	mustGRPC(t, e)
	defer p.Close()
	receipt, e := sdk.NewCapabilityClient(p, "local").Invoke(ctx, req, material)
	mustGRPC(t, e)
	if receipt.OperationID != req.OperationID {
		t.Fatal("wrong receipt")
	}
	_, e = b.exec.Run(ctx, req.OperationID)
	mustGRPC(t, e)
	mustGRPC(t, b.target.Complete(ctx, req.OperationID))
	b.clock.advance(time.Second)
	_, e = b.exec.Reconcile(ctx, req.OperationID)
	mustGRPC(t, e)
	view, e := sdk.NewCapabilityClient(p, "local").GetInvocation(ctx, req.OperationID)
	mustGRPC(t, e)
	if view.Effect != "CONFIRMED" {
		t.Fatal(view)
	}
	truth, e := b.target.Snapshot(ctx)
	mustGRPC(t, e)
	if truth.Changes != 1 {
		t.Fatal(truth)
	}
}

func TestWSReliableReconnectFencesOldConnection(t *testing.T) {
	edge, cloud := prepareWS(t)
	a, ah, _ := openWS(t, edge)
	defer a.close()
	b, bh, _ := openWS(t, cloud)
	defer b.close()
	ah = reliableHost(t, a, ah)
	bh = reliableHost(t, b, bh)
	s, e := wsbinding.NewServer(bh)
	mustGRPC(t, e)
	defer s.Close()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go s.Serve(l)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, e := ah.Dial(ctx, "wss://"+l.Addr().String()+"/harness", "cloud")
	mustGRPC(t, e)
	defer first.Close()
	second, e := ah.Dial(ctx, "wss://"+l.Addr().String()+"/harness", "cloud")
	mustGRPC(t, e)
	defer second.Close()
	select {
	case <-first.Done():
	case <-ctx.Done():
		t.Fatal("old connection survived")
	}
	req, material, e := b.request(ctx)
	mustGRPC(t, e)
	_, e = sdk.NewCapabilityClient(second, "local").Invoke(ctx, req, material)
	mustGRPC(t, e)
}

func TestWSReliableRunStoreConsumption(t *testing.T) {
	ctx := context.Background()
	h, e := fresh(ctx)
	mustGRPC(t, e)
	defer h.destroy()
	req, material, e := h.request(ctx)
	mustGRPC(t, e)
	_, e = h.exec.Invoke(ctx, req, material)
	mustGRPC(t, e)
	_, e = h.exec.Run(ctx, req.OperationID)
	mustGRPC(t, e)
	mustGRPC(t, h.target.Complete(ctx, req.OperationID))
	h.clock.advance(time.Second)
	record, e := h.exec.Reconcile(ctx, req.OperationID)
	mustGRPC(t, e)
	j, e := wsbinding.NewJournal(h.auth, "trusted-execution", wsbinding.JournalConfig{Records: 16, Bytes: 65536, Reorder: 4, Retention: time.Minute, Attempts: 3})
	mustGRPC(t, e)
	message := &wire.WSReliable{Generation: 1, Position: 1, MessageId: "report", Request: &wire.CapabilityRequest{MessageId: "report", Namespace: "local", Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: req.OperationID}}}
	_, e = j.Receive(ctx, message)
	mustGRPC(t, e)
	before, e := h.core.Load(ctx, req.Qualification.Ref)
	mustGRPC(t, e)
	fact := record.Reports[len(record.Reports)-1]
	reply := &wire.CapabilityResponse{MessageId: "report-reply", ReplyTo: "report", Namespace: "local", Body: &wire.CapabilityResponse_Receipt{Receipt: &wire.InvocationReceipt{OperationId: req.OperationID, Revision: record.Revision}}}
	e = j.Complete(ctx, message, reply, func(tx authorization.RuntimeTransaction) error {
		if e := h.work.ConsumeExecutionIn(tx, fact); e != nil {
			return e
		}
		return &authorization.Error{Code: authorization.Unavailable}
	})
	if !authorization.Is(e, authorization.Unavailable) {
		t.Fatal(e)
	}
	after, e := h.core.Load(ctx, req.Qualification.Ref)
	mustGRPC(t, e)
	if after.Task.Version != before.Task.Version {
		t.Fatal("partial task commit")
	}
	e = j.Complete(ctx, message, reply, func(tx authorization.RuntimeTransaction) error { return h.work.ConsumeExecutionIn(tx, fact) })
	mustGRPC(t, e)
	after, e = h.core.Load(ctx, req.Qualification.Ref)
	mustGRPC(t, e)
	if after.Task.State != "COMPLETED" {
		t.Fatal(after.Task)
	}
	pending, e := j.Unconsumed(ctx)
	mustGRPC(t, e)
	if len(pending) != 0 {
		t.Fatal("not consumed")
	}
	batch, e := j.Replay(ctx, 1, 0)
	mustGRPC(t, e)
	if len(batch) != 1 || batch[0].Response == nil {
		t.Fatal("reply not committed")
	}
}

func TestWSReliableExpiredCursorUsesOriginalSnapshot(t *testing.T) {
	edge, cloud := prepareWS(t)
	a, ah, _ := openWS(t, edge)
	defer a.close()
	b, bh, _ := openWS(t, cloud)
	defer b.close()
	ah = reliableHost(t, a, ah)
	bh = reliableHost(t, b, bh)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, e := wsbinding.NewServer(bh)
	mustGRPC(t, e)
	defer s.Close()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go s.Serve(l)
	p, e := ah.Dial(ctx, "wss://"+l.Addr().String()+"/harness", "cloud")
	mustGRPC(t, e)
	defer p.Close()
	req, material, e := b.request(ctx)
	mustGRPC(t, e)
	_, e = sdk.NewCapabilityClient(p, "local").Invoke(ctx, req, material)
	mustGRPC(t, e)
	_, e = b.exec.Run(ctx, req.OperationID)
	mustGRPC(t, e)
	mustGRPC(t, b.target.Complete(ctx, req.OperationID))
	b.clock.advance(time.Second)
	_, e = b.exec.Reconcile(ctx, req.OperationID)
	mustGRPC(t, e)
	a.clock.advance(2 * time.Minute)
	b.clock.advance(2 * time.Minute)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		recovered, e := p.Recover(ctx)
		mustGRPC(t, e)
		if len(recovered) > 0 && recovered[0].State == "KNOWN" {
			if recovered[0].OperationID != req.OperationID {
				t.Fatal(recovered)
			}
			snapshot := new(wire.InvocationSnapshot)
			mustGRPC(t, proto.Unmarshal(recovered[0].Snapshot, snapshot))
			if snapshot.Effect != "CONFIRMED" {
				t.Fatal(snapshot)
			}
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("no snapshot recovery")
		}
	}
	// A cached snapshot is not an enduring grant to disclose it.
	admitted, e := b.exec.GetInvocation(ctx, req.OperationID)
	mustGRPC(t, e)
	revoke, e := b.operation(ctx)
	mustGRPC(t, e)
	state, e := b.db.Load(ctx)
	mustGRPC(t, e)
	_, e = b.grants.Mutate(ctx, b.token, &wire.GrantMutation{OperationId: revoke, Kind: "REVOKE", GrantId: admitted.Permit.GrantID, ExpectedRevision: state.State.Revision, ExpectedGrantRevision: state.State.Signed.Grants[admitted.Permit.GrantID].Record.Revision})
	mustGRPC(t, e)
	hidden, e := p.Recover(ctx)
	mustGRPC(t, e)
	if len(hidden) != 0 {
		t.Fatal("revoked cached snapshot disclosed", hidden)
	}
	// Temporary queries remain usable while reliable stream retirement settles.
	_, e = sdk.NewCapabilityClient(p, "local").GetInvocation(ctx, req.OperationID)
	if !authorization.Is(e, authorization.Denied) {
		t.Fatal(e)
	}
	truth, e := b.target.Snapshot(ctx)
	mustGRPC(t, e)
	if truth.Changes != 1 {
		t.Fatal(truth)
	}
}

func TestWSReliableWindowAndIdentityBoundaries(t *testing.T) {
	edge, cloud := prepareWS(t)
	a, ah, _ := openWS(t, edge)
	defer a.close()
	b, bh, _ := openWS(t, cloud)
	defer b.close()
	ah = reliableHost(t, a, ah)
	bh = reliableHost(t, b, bh)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, e := wsbinding.NewServer(bh)
	mustGRPC(t, e)
	defer s.Close()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go s.Serve(l)
	p, e := ah.Dial(ctx, "wss://"+l.Addr().String()+"/harness", "cloud")
	mustGRPC(t, e)
	defer p.Close()
	api := sdk.NewCapabilityClient(p, "local")
	accepted, material, e := b.request(ctx)
	mustGRPC(t, e)
	original, e := api.Invoke(ctx, accepted, material)
	mustGRPC(t, e)
	again, e := api.Invoke(ctx, accepted, material)
	mustGRPC(t, e)
	if again != original {
		t.Fatal("new message duplicated operation")
	}
	changed := accepted
	changed.ResourceVersion++
	_, e = api.Invoke(ctx, changed, material)
	if !authorization.Is(e, authorization.IdentityConflict) {
		t.Fatal("semantic conflict", e)
	}
	old, oldMaterial, e := b.request(ctx)
	mustGRPC(t, e)
	closeOp, e := b.operation(ctx)
	mustGRPC(t, e)
	snap, e := b.db.Load(ctx)
	mustGRPC(t, e)
	_, e = b.auth.Execute(ctx, b.token, authorization.Mutation{Namespace: "local", OperationID: closeOp, Command: &wire.AuthorizationCommand{ExpectedRevision: snap.State.Revision, Change: &wire.AuthorizationCommand_CloseWindows{CloseWindows: true}}})
	mustGRPC(t, e)
	for i := 0; i < 2; i++ {
		_, e = api.Invoke(ctx, old, oldMaterial)
		if !authorization.Is(e, authorization.Expired) {
			t.Fatal("closed window accepted", e)
		}
	}
	again, e = api.Invoke(ctx, accepted, material)
	mustGRPC(t, e)
	if again != original {
		t.Fatal("closed window forgot acceptance")
	}
	truth, e := b.target.Snapshot(ctx)
	mustGRPC(t, e)
	if truth.Changes != 0 {
		t.Fatal("admission was confused with execution", truth)
	}
	_, e = api.GetInvocation(ctx, accepted.OperationID)
	mustGRPC(t, e)
}

func TestWSReliableDeniedObjectDoesNotBlockHealthyInvocation(t *testing.T) {
	for _, stage := range []string{"before-persist", "after-persist"} {
		t.Run(stage, func(t *testing.T) {
			edge, cloud := prepareWS(t)
			a, ah, _ := openWS(t, edge)
			defer a.close()
			b, bh, _ := openWS(t, cloud)
			defer b.close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			denied, material, e := b.request(ctx)
			mustGRPC(t, e)
			healthy, goodMaterial, e := b.request(ctx)
			mustGRPC(t, e)
			ah = reliableHost(t, a, ah)
			bh = reliableHost(t, b, bh)
			resolve := bh.Resolve
			bh.Resolve = func(ctx context.Context, p authorization.GrantPresentation) (*wsbinding.Binding, error) {
				binding, e := resolve(ctx, p)
				if e != nil {
					return nil, e
				}
				retain := binding.Retain
				binding.Retain = func(ctx context.Context, p authorization.GrantPresentation, r *wire.CapabilityRequest) error {
					pending, e := binding.Journal.Unconsumed(ctx)
					if e != nil {
						return e
					}
					if (stage == "before-persist" || len(pending) > 0) && r.GetInvoke().GetInvocation().GetOperationId() == denied.OperationID {
						return &authorization.Error{Code: authorization.Denied}
					}
					return retain(ctx, p, r)
				}
				return binding, nil
			}
			s, e := wsbinding.NewServer(bh)
			mustGRPC(t, e)
			defer s.Close()
			l, e := net.Listen("tcp", "127.0.0.1:0")
			mustGRPC(t, e)
			go s.Serve(l)
			p, e := ah.Dial(ctx, "wss://"+l.Addr().String()+"/harness", "cloud")
			mustGRPC(t, e)
			defer p.Close()
			api := sdk.NewCapabilityClient(p, "local")
			_, e = api.Invoke(ctx, denied, material)
			if !authorization.Is(e, authorization.Denied) {
				t.Fatal(e)
			}
			_, e = api.Invoke(ctx, healthy, goodMaterial)
			mustGRPC(t, e)
			_, e = b.exec.Run(ctx, healthy.OperationID)
			mustGRPC(t, e)
			mustGRPC(t, b.target.Complete(ctx, healthy.OperationID))
			b.clock.advance(time.Second)
			_, e = b.exec.Reconcile(ctx, healthy.OperationID)
			mustGRPC(t, e)
			view, e := api.GetInvocation(ctx, healthy.OperationID)
			mustGRPC(t, e)
			if view.Effect != "CONFIRMED" {
				t.Fatal(view)
			}
			truth, e := b.target.Snapshot(ctx)
			mustGRPC(t, e)
			if truth.Changes != 1 {
				t.Fatal(truth)
			}

		})
	}
}

func TestWSReliableNonterminalRecoveryKeepsUpdating(t *testing.T) {
	edge, cloud := prepareWS(t)
	a, ah, _ := openWS(t, edge)
	defer a.close()
	b, bh, _ := openWS(t, cloud)
	defer b.close()
	c := reliableConfig()
	c.Retention = time.Second
	ah = reliableHostConfig(t, a, ah, c)
	bh = reliableHostConfig(t, b, bh, c)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, e := wsbinding.NewServer(bh)
	mustGRPC(t, e)
	defer s.Close()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go s.Serve(l)
	p, e := ah.Dial(ctx, "wss://"+l.Addr().String()+"/harness", "cloud")
	mustGRPC(t, e)
	defer p.Close()
	req, material, e := b.request(ctx)
	mustGRPC(t, e)
	_, e = sdk.NewCapabilityClient(p, "local").Invoke(ctx, req, material)
	mustGRPC(t, e)
	_, e = b.exec.Run(ctx, req.OperationID)
	mustGRPC(t, e)
	a.clock.advance(2 * time.Second)
	b.clock.advance(2 * time.Second)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		records, e := p.Recover(ctx)
		mustGRPC(t, e)
		if len(records) > 0 && records[0].State == "KNOWN" {
			snapshot := new(wire.InvocationSnapshot)
			mustGRPC(t, proto.Unmarshal(records[0].Snapshot, snapshot))
			if snapshot.Phase != "IN_PROGRESS" {
				t.Fatal(snapshot)
			}
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("no initial recovery snapshot")
		}
	}
	mustGRPC(t, b.target.Complete(ctx, req.OperationID))
	b.clock.advance(time.Second)
	_, e = b.exec.Reconcile(ctx, req.OperationID)
	mustGRPC(t, e)
	records, e := p.Recover(ctx)
	mustGRPC(t, e)
	if len(records) != 1 {
		t.Fatal(records)
	}
	snapshot := new(wire.InvocationSnapshot)
	mustGRPC(t, proto.Unmarshal(records[0].Snapshot, snapshot))
	if snapshot.Effect != "CONFIRMED" {
		t.Fatal("stale nonterminal snapshot", snapshot)
	}
	for i := 0; i < 4; i++ {
		records, e = p.Recover(ctx)
		mustGRPC(t, e)
	}
	if len(records) != 1 || len(records[0].Snapshot) != 0 || records[0].State != "UNKNOWN" {
		t.Fatal("recovery budget bypassed", records)
	}
	journal, e := wsbinding.NewJournal(a.auth, "local/operator/cloud", c)
	mustGRPC(t, e)
	mustGRPC(t, journal.Clean(ctx))
	status, e := journal.Status(ctx)
	mustGRPC(t, e)
	if status.Recovering != 0 || status.Generation < 2 {
		t.Fatal("cleanup lost boundary or kept completed obligation", status)
	}
}

func TestWSReliableQueuedDisclosureDenialKeepsHealthyMessage(t *testing.T) {
	edge, cloud := prepareWS(t)
	a, ah, _ := openWS(t, edge)
	defer a.close()
	b, bh, _ := openWS(t, cloud)
	defer b.close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bad, badMaterial, e := b.request(ctx)
	mustGRPC(t, e)
	good, goodMaterial, e := b.request(ctx)
	mustGRPC(t, e)
	ah = reliableHost(t, a, ah)
	bh = reliableHost(t, b, bh)
	source, e := wsbinding.NewJournal(a.auth, "local/operator/cloud", reliableConfig())
	mustGRPC(t, e)
	for i, in := range []struct {
		request  execution.Request
		material string
	}{{bad, badMaterial}, {good, goodMaterial}} {
		id := []string{"denied-before-send", "healthy-after-denied"}[i]
		_, e = source.Prepare(ctx, &wire.CapabilityRequest{MessageId: id, Namespace: "local", Body: &wire.CapabilityRequest_Invoke{Invoke: &wire.InvokeCapability{Invocation: executionwire.Encode(in.request), GrantMaterial: in.material}}})
		mustGRPC(t, e)
	}
	resolve := ah.Resolve
	ah.Resolve = func(ctx context.Context, p authorization.GrantPresentation) (*wsbinding.Binding, error) {
		binding, e := resolve(ctx, p)
		if e != nil {
			return nil, e
		}
		disclose := binding.Disclose
		binding.Disclose = func(ctx context.Context, p authorization.GrantPresentation, r *wire.CapabilityRequest) error {
			if r.GetInvoke().GetInvocation().GetOperationId() == bad.OperationID {
				return &authorization.Error{Code: authorization.Denied}
			}
			return disclose(ctx, p, r)
		}
		return binding, nil
	}
	s, e := wsbinding.NewServer(bh)
	mustGRPC(t, e)
	defer s.Close()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go s.Serve(l)
	p, e := ah.Dial(ctx, "wss://"+l.Addr().String()+"/harness", "cloud")
	mustGRPC(t, e)
	defer p.Close()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		reply, e := p.Result(ctx, "healthy-after-denied")
		if e == nil {
			if reply.GetReceipt().GetOperationId() != good.OperationID {
				t.Fatal(reply)
			}
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("healthy queued message blocked", e)
		}
	}
	if _, e = b.exec.GetInvocation(ctx, bad.OperationID); !authorization.Is(e, authorization.Denied) {
		t.Fatal("denied payload was handed off", e)
	}
	_, e = b.exec.Run(ctx, good.OperationID)
	mustGRPC(t, e)
	mustGRPC(t, b.target.Complete(ctx, good.OperationID))
	b.clock.advance(time.Second)
	_, e = b.exec.Reconcile(ctx, good.OperationID)
	mustGRPC(t, e)
	truth, e := b.target.Snapshot(ctx)
	mustGRPC(t, e)
	if truth.Changes != 1 {
		t.Fatal(truth)
	}
}

func TestWSReliableQueuedControlsAndExpiredLease(t *testing.T) {
	for _, kind := range []string{"PAUSE", "CANCEL", "EXPIRED_LEASE"} {
		t.Run(kind, func(t *testing.T) {
			edge, cloud := prepareWS(t)
			a, ah, _ := openWS(t, edge)
			defer a.close()
			b, bh, _ := openWS(t, cloud)
			defer b.close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			old, material, e := b.request(ctx)
			mustGRPC(t, e)
			source, e := wsbinding.NewJournal(a.auth, "local/operator/cloud", reliableConfig())
			mustGRPC(t, e)
			queued := func(id string, r execution.Request, m string) {
				_, e := source.Prepare(ctx, &wire.CapabilityRequest{MessageId: id, Namespace: "local", Body: &wire.CapabilityRequest_Invoke{Invoke: &wire.InvokeCapability{Invocation: executionwire.Encode(r), GrantMaterial: m}}})
				mustGRPC(t, e)
			}
			queued("old-work", old, material)
			if kind == "EXPIRED_LEASE" {
				b.clock.advance(11 * time.Second)
			}
			scope := wsScope(b, true)
			scope.Actions = append(scope.Actions, "task.pause", "task.cancel")
			for _, change := range []*wire.AuthorizationCommand{{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "reconnect-controls", Scope: scope}}}}}, {Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "reconnect-controls", Subject: "operator", Scope: scope, Mode: "continuous"}}}} {
				op, e := b.operation(ctx)
				mustGRPC(t, e)
				state, e := b.db.Load(ctx)
				mustGRPC(t, e)
				change.ExpectedRevision = state.State.Revision
				_, e = b.auth.Execute(ctx, b.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: change})
				mustGRPC(t, e)
			}
			if kind != "EXPIRED_LEASE" {
				controls, e := b.core.Controls(controlLimits())
				mustGRPC(t, e)
				state, e := b.core.Load(ctx, old.Qualification.Ref)
				mustGRPC(t, e)
				op, e := b.operation(ctx)
				mustGRPC(t, e)
				_, e = controls.Request(ctx, b.token, tasks.ControlRequest{OperationID: op, Ref: old.Qualification.Ref, ExpectedVersion: state.Task.Version, Intent: kind})
				mustGRPC(t, e)
			}
			healthy, goodMaterial, e := b.request(ctx)
			mustGRPC(t, e)
			queued("healthy-work", healthy, goodMaterial)
			ah = reliableHost(t, a, ah)
			bh = reliableHost(t, b, bh)
			s, e := wsbinding.NewServer(bh)
			mustGRPC(t, e)
			defer s.Close()
			l, e := net.Listen("tcp", "127.0.0.1:0")
			mustGRPC(t, e)
			go s.Serve(l)
			p, e := ah.Dial(ctx, "wss://"+l.Addr().String()+"/harness", "cloud")
			mustGRPC(t, e)
			defer p.Close()
			tick := time.NewTicker(20 * time.Millisecond)
			defer tick.Stop()
			for {
				bad, e1 := p.Result(ctx, "old-work")
				good, e2 := p.Result(ctx, "healthy-work")
				if e1 == nil && e2 == nil {
					if bad.GetFailure() == nil || good.GetReceipt().GetOperationId() != healthy.OperationID {
						t.Fatal(bad, good)
					}
					break
				}
				select {
				case <-tick.C:
				case <-ctx.Done():
					t.Fatal("queued control recovery", e1, e2)
				}
			}
			_, e = b.exec.Run(ctx, healthy.OperationID)
			mustGRPC(t, e)
			mustGRPC(t, b.target.Complete(ctx, healthy.OperationID))
			b.clock.advance(time.Second)
			_, e = b.exec.Reconcile(ctx, healthy.OperationID)
			mustGRPC(t, e)
			truth, e := b.target.Snapshot(ctx)
			mustGRPC(t, e)
			if truth.Changes != 1 {
				t.Fatal(truth)
			}
			if kind != "EXPIRED_LEASE" {
				state, e := b.core.Load(ctx, old.Qualification.Ref)
				mustGRPC(t, e)
				if state.Task.Control.Intent != kind {
					t.Fatal("persistent control lost", state.Task)
				}
			}
		})
	}
}
