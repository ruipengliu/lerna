package takeover

import (
	"context"
	"fmt"
	"lerna/authorization"
	"lerna/execution"
	"time"
)

var concurrencyNames = []string{"atomic-control-failure", "atomic-control-unknown", "atomic-start-failure", "atomic-start-unknown", "simultaneous-starts", "synchronous-inflight", "late-control-result"}

type faultyAuthority struct {
	execution.Authority
	commit bool
	skip   int
}

func (a *faultyAuthority) UpdateExecution(c context.Context, f func(authorization.ExecutionTransaction) error) error {
	if a.skip > 0 {
		a.skip--
		return a.Authority.UpdateExecution(c, f)
	}
	if a.commit {
		if e := a.Authority.UpdateExecution(c, f); e != nil {
			return e
		}
		return &authorization.Error{Code: authorization.OutcomeUnknown}
	}
	return &authorization.Error{Code: authorization.Unavailable}
}

type syncWrap struct {
	execution.Driver
	start func(context.Context, execution.Call) error
}

func (w syncWrap) Start(c context.Context, r execution.Call) error { return w.start(c, r) }
func concurrency(ctx context.Context, name string) error {
	h, e := fresh(ctx)
	if e != nil {
		return e
	}
	defer h.destroy()
	if name == "atomic-control-failure" || name == "atomic-control-unknown" || name == "atomic-start-failure" || name == "atomic-start-unknown" {
		isStart := name == "atomic-start-failure" || name == "atomic-start-unknown"
		committed := name == "atomic-control-unknown" || name == "atomic-start-unknown"
		var r execution.Request
		if isStart {
			r, e = h.invocation(ctx, 1, 1, false)
			if e != nil {
				return e
			}
		}
		op, e := h.operation(ctx)
		if e != nil {
			return e
		}
		skip := 0
		if isStart {
			skip = 1
		}
		broken, e := execution.New(&faultyAuthority{h.grants, committed, skip}, h.work, h.access, h.target, h.binding, h.cap, config(), h.operation)
		if e != nil {
			return e
		}
		broken, e = broken.WithResourceControl(scope(), h.target)
		if e != nil {
			return e
		}
		if isStart {
			_, e = broken.Run(ctx, r.OperationID)
		} else {
			_, e = broken.RequestResourceControl(ctx, execution.ResourceControlRequest{OperationID: op, Resource: scope().Ref, Intent: "TAKEOVER", ExpectedVersion: 1})
		}
		if e == nil {
			return fmt.Errorf("fault not propagated")
		}
		target, e := h.target.Snapshot(ctx)
		if e != nil || target.Jobs != 0 {
			return fmt.Errorf("external action on failed or unknown commit")
		}
		if isStart {
			record, e := h.client.GetInvocation(ctx, r.OperationID)
			if e != nil {
				return e
			}
			if record.Started != committed {
				return fmt.Errorf("partial start")
			}
			if committed {
				if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
					return e
				}
				target, e = h.target.Snapshot(ctx)
				return require(e == nil && target.Jobs == 0, "unknown commit resent Start")
			}
			return nil
		}
		v, e := h.client.GetResourceControl(ctx, scope().Ref)
		if e != nil {
			return e
		}
		want := uint64(1)
		if committed {
			want = 2
			receipt, e := h.client.LookupResourceControl(ctx, op)
			if e != nil || receipt.Version != 2 {
				return fmt.Errorf("unknown commit receipt lost: %v", e)
			}
		}
		return require(v.Version == want, "partial resource commit")
	}
	other, e := h.other(ctx)
	if e != nil {
		return e
	}
	defer other.close()
	switch name {
	case "simultaneous-starts":
		r, e := h.invocation(ctx, 1, 1, false)
		if e != nil {
			return e
		}
		r2, e := other.invocation(ctx, 1, 1, false)
		if e != nil {
			return e
		}
		gate := make(chan struct{})
		results := make(chan error, 2)
		for i, p := range []*harness{h, other} {
			req := r
			if i == 1 {
				req = r2
			}
			go func(p *harness, req execution.Request) {
				<-gate
				_, e := p.exec.Run(ctx, req.OperationID)
				results <- e
			}(p, req)
		}
		close(gate)
		a, b := <-results, <-results
		if (a == nil) == (b == nil) {
			return fmt.Errorf("start ordering: %v %v", a, b)
		}
		target, e := h.target.Snapshot(ctx)
		return require(e == nil && target.Jobs == 1, "multiple concurrent targets")
	case "synchronous-inflight":
		syn, e := openEntry(ctx, h.root, h.token, "sync")
		if e != nil {
			return e
		}
		defer syn.close()
		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		if e = syn.replace(syncWrap{Driver: syn.target, start: func(c context.Context, r execution.Call) error {
			close(entered)
			select {
			case <-release:
			case <-c.Done():
				return c.Err()
			}
			if e := syn.target.Start(c, r); e != nil {
				return e
			}
			return syn.target.Complete(c, r.Request.OperationID)
		}}, syn.target); e != nil {
			return e
		}
		r, e := syn.invocation(ctx, 1, 1, false)
		if e != nil {
			return e
		}
		go func() { _, e := syn.exec.Run(ctx, r.OperationID); done <- e }()
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			return fmt.Errorf("start not reached")
		}
		if _, e = h.control(ctx, "TAKEOVER"); e != nil {
			close(release)
			<-done
			return e
		}
		v, e := h.exec.AdvanceResourceControl(ctx, scope().Ref)
		close(release)
		<-done
		if e != nil || v.Progress != "ACCEPTED" {
			return fmt.Errorf("inflight omitted: %+v %v", v, e)
		}
		syn.clock.advance(4 * time.Second)
		if e = syn.exec.ReconcileResource(ctx, scope().Ref, 16); e != nil {
			return e
		}
		h.clock.advance(4 * time.Second)
		v, e = h.exec.AdvanceResourceControl(ctx, scope().Ref)
		target, err := h.target.Snapshot(ctx)
		return require(e == nil && err == nil && v.Progress == "APPLIED" && target.Changes == 0, "sync path escaped fencing")
	case "late-control-result":
		if _, e = h.control(ctx, "TAKEOVER"); e != nil {
			return e
		}
		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		if e = h.replace(h.target, controlWrap{ResourceDriver: h.target, apply: func(c context.Context, r execution.ResourceCommand) (execution.ResourceObservation, error) {
			o, e := h.target.ApplyResourceControl(c, r)
			close(entered)
			select {
			case <-release:
			case <-c.Done():
				return o, c.Err()
			}
			return o, e
		}}); e != nil {
			return e
		}
		go func() { _, e := h.exec.AdvanceResourceControl(ctx, scope().Ref); done <- e }()
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			return fmt.Errorf("apply not reached")
		}
		_, e = other.control(ctx, "TAKEOVER")
		close(release)
		<-done
		if e != nil {
			return e
		}
		v, e := other.exec.AdvanceResourceControl(ctx, scope().Ref)
		return require(e == nil && v.Version == 3 && v.Progress == "APPLIED", "old control result rewrote new version")
	}
	return fmt.Errorf("unknown concurrency %s", name)
}
