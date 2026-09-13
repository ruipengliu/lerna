package takeover

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/execution"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var crashPoints = []string{"takeover-admitted", "start-registered", "control-dispatched", "target-stopped", "target-completed", "takeover-applied", "resume-observed", "resume-dispatched", "resume-committed", "resume-applied"}

type checkpoint struct {
	Token            string
	Request          execution.Request
	Takeover, Resume string
}

func RunProbe(ctx context.Context, root, point string) error {
	h, e := open(ctx, root, "")
	if e != nil {
		return e
	}
	defer h.close()
	r, _, e := h.request(ctx)
	if e != nil {
		return e
	}
	m, e := h.issue(ctx, r)
	if e != nil {
		return e
	}
	op, e := h.operation(ctx)
	if e != nil {
		return e
	}
	resume, e := h.operation(ctx)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(checkpoint{h.token, r, op, resume})
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(root, "checkpoint.json"), raw, 0600); e != nil {
		return e
	}
	request := func() error {
		_, e := h.client.RequestResourceControl(ctx, execution.ResourceControlRequest{OperationID: op, Resource: scope().Ref, Intent: "TAKEOVER", ExpectedVersion: 1})
		return e
	}
	if point == "start-registered" {
		if e = h.replace(driverWrap{AsyncDriver: h.target, CancelDriver: h.target, start: func(context.Context, execution.Call) (execution.Fact, error) {
			if e := request(); e != nil {
				return execution.Fact{}, e
			}
			os.Exit(73)
			return execution.Fact{}, nil
		}}, h.target); e != nil {
			return e
		}
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		return e
	}
	if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
		return e
	}
	if e = request(); e != nil {
		return e
	}
	if point == "takeover-admitted" {
		os.Exit(73)
	}
	if point == "target-completed" {
		if e = h.target.Complete(ctx, r.OperationID); e != nil {
			return e
		}
	}
	if point == "control-dispatched" || point == "target-stopped" || point == "target-completed" {
		if e = h.replace(h.target, controlWrap{ResourceDriver: h.target, apply: func(c context.Context, in execution.ResourceCommand) (execution.ResourceObservation, error) {
			if point != "control-dispatched" {
				if _, e := h.target.ApplyResourceControl(c, in); e != nil {
					return execution.ResourceObservation{}, e
				}
			}
			os.Exit(73)
			return execution.ResourceObservation{}, nil
		}}); e != nil {
			return e
		}
	}
	if _, e = h.exec.AdvanceResourceControl(ctx, scope().Ref); e != nil {
		return e
	}
	v, e := h.settle(ctx)
	if e != nil || v.Progress != "APPLIED" {
		return fmt.Errorf("takeover not settled %v", e)
	}
	if point == "takeover-applied" {
		os.Exit(73)
	}
	if e = h.target.UserChange(ctx, 20); e != nil {
		return e
	}
	if _, e = h.client.RequestResourceControl(ctx, execution.ResourceControlRequest{OperationID: resume, Resource: scope().Ref, Intent: "RESUME", ExpectedVersion: 2}); e != nil {
		return e
	}
	w := controlWrap{ResourceDriver: h.target}
	if point == "resume-observed" {
		w.observe = func(c context.Context, r execution.ResourceRef) (execution.ResourceObservation, error) {
			if _, e := h.target.ObserveResource(c, r); e != nil {
				return execution.ResourceObservation{}, e
			}
			os.Exit(73)
			return execution.ResourceObservation{}, nil
		}
	}
	if point == "resume-dispatched" || point == "resume-committed" {
		w.apply = func(c context.Context, r execution.ResourceCommand) (execution.ResourceObservation, error) {
			if point == "resume-committed" {
				if _, e := h.target.ApplyResourceControl(c, r); e != nil {
					return execution.ResourceObservation{}, e
				}
			}
			os.Exit(73)
			return execution.ResourceObservation{}, nil
		}
	}
	if e = h.replace(h.target, w); e != nil {
		return e
	}
	v, e = h.exec.AdvanceResourceControl(ctx, scope().Ref)
	if e != nil {
		return e
	}
	if point == "resume-applied" && v.Progress == "APPLIED" {
		os.Exit(73)
	}
	return fmt.Errorf("unknown crash point %s", point)
}
func recovery(ctx context.Context, exe, point string) error {
	root, e := os.MkdirTemp("", "resource-crash-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(root)
	cmd := exec.CommandContext(ctx, exe, "resource-crash-probe", root, point)
	output, e := cmd.CombinedOutput()
	if code, ok := e.(*exec.ExitError); !ok || code.ExitCode() != 73 {
		return fmt.Errorf("probe %s: %v %s", point, e, output)
	}
	return RecoverProbe(ctx, root, point)
}

// RecoverProbe is a local recovery entry for an existing probe directory. It
// never resends a possibly sent control command or the original business Start.
func RecoverProbe(ctx context.Context, root, point string) error {
	raw, e := os.ReadFile(filepath.Join(root, "checkpoint.json"))
	if e != nil {
		return e
	}
	var cp checkpoint
	if e = json.Unmarshal(raw, &cp); e != nil {
		return e
	}
	h, e := open(ctx, root, cp.Token)
	if e != nil {
		return e
	}
	defer h.close()
	h.clock.advance(5 * time.Second)
	receipt, e := h.client.LookupResourceControl(ctx, cp.Takeover)
	if e != nil || receipt.Version != 2 {
		return fmt.Errorf("takeover receipt lost: %v", e)
	}
	before, e := h.exec.GetInvocation(ctx, cp.Request.OperationID)
	if e != nil || !before.Started {
		return fmt.Errorf("original invocation lost: %v", e)
	}
	if _, e = h.client.Invoke(ctx, cp.Request, ""); e != nil {
		return e
	}
	if _, e = h.exec.Run(ctx, cp.Request.OperationID); e != nil {
		return e
	}
	if point == "control-dispatched" {
		if e = h.target.Complete(ctx, cp.Request.OperationID); e != nil {
			return e
		}
	}
	if _, e = h.exec.AdvanceResourceControl(ctx, scope().Ref); e != nil {
		return e
	}
	v, e := h.settle(ctx)
	if e != nil {
		return e
	}
	pending := point == "control-dispatched" || point == "resume-dispatched"
	if (v.Progress == "APPLIED") == pending {
		return fmt.Errorf("wrong recovered progress: %+v", v)
	}
	record, e := h.exec.GetInvocation(ctx, cp.Request.OperationID)
	if e != nil {
		return e
	}
	if record.Permit != before.Permit || record.Checks < before.Checks || record.Applied != record.Revision || record.Effect == "UNKNOWN" {
		return fmt.Errorf("original permit, effect or outbox lost")
	}
	target, e := h.target.Snapshot(ctx)
	if e != nil {
		return e
	}
	want := 0
	if point == "target-completed" || point == "control-dispatched" {
		want = 1
	}
	jobs := 1
	if point == "start-registered" {
		jobs = 0
	}
	if target.Changes != want || target.Jobs != jobs {
		return fmt.Errorf("duplicate or lost target effect: %+v", target)
	}
	if v.Intent == "RESUME" {
		if target.Value != 20 {
			return fmt.Errorf("human mutation lost")
		}
		rr, e := h.client.LookupResourceControl(ctx, cp.Resume)
		if e != nil || rr.Version != 3 {
			return fmt.Errorf("resume receipt lost")
		}
	}
	return nil
}
