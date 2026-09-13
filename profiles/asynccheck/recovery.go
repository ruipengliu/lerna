package asynccheck

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/execution"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var crashPoints = []string{"handle-lost", "handle-saved", "inspection-reserved", "cancel-accepted", "cancel-registered", "target-cancelled", "report-saved", "core-applied"}

type checkpoint struct {
	Token   string
	Request execution.Request
	Cancel  string
}
type crashCore struct{ execution.Core }

func (c crashCore) ConsumeExecution(ctx context.Context, r tasks.ExecutionReport) error {
	if e := c.Core.ConsumeExecution(ctx, r); e != nil {
		return e
	}
	os.Exit(73)
	return nil
}
func RunProbe(ctx context.Context, root, point string) error {
	h, e := open(ctx, root, "")
	if e != nil {
		return e
	}
	defer h.close()
	r, m, e := h.request(ctx)
	if e != nil {
		return e
	}
	cancel, e := h.operation(ctx)
	if e != nil {
		return e
	}
	raw, _ := json.Marshal(checkpoint{h.token, r, cancel})
	if e = os.WriteFile(filepath.Join(root, "checkpoint.json"), raw, 0600); e != nil {
		return e
	}
	if point == "handle-lost" {
		if e = h.replace(driverWrap{AsyncDriver: h.target, cancel: h.target, start: func(c context.Context, in execution.Call) (execution.Fact, error) {
			_, e := h.target.StartAsync(c, in)
			if e != nil {
				return execution.Fact{}, e
			}
			os.Exit(73)
			return execution.Fact{}, nil
		}}); e != nil {
			return e
		}
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		return e
	}
	if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
		return e
	}
	if point == "handle-saved" {
		os.Exit(73)
	}
	if point == "inspection-reserved" {
		if e = h.replace(driverWrap{AsyncDriver: h.target, cancel: h.target, inspect: func(context.Context, execution.Call, execution.Association) (execution.Fact, error) {
			os.Exit(73)
			return execution.Fact{}, nil
		}}); e != nil {
			return e
		}
		h.clock.advance(time.Second)
		_, e = h.client.Reconcile(ctx, r.OperationID)
		return e
	}
	if point == "report-saved" || point == "core-applied" {
		if e = h.target.Complete(ctx, r.OperationID); e != nil {
			return e
		}
		h.clock.advance(time.Second)
		if _, e = h.client.Reconcile(ctx, r.OperationID); e != nil {
			return e
		}
		if point == "report-saved" {
			os.Exit(73)
		}
		h.exec, e = execution.New(h.grants, crashCore{h.work}, h.access, h.target, h.binding, h.cap, config(), h.operation)
		if e != nil {
			return e
		}
		return h.exec.Drain(ctx, 16)
	}
	if _, e = h.client.RequestCancel(ctx, execution.CancelRequest{OperationID: cancel, Invocation: r.OperationID}); e != nil {
		return e
	}
	if point == "cancel-accepted" {
		os.Exit(73)
	}
	if e = h.replace(driverWrap{AsyncDriver: h.target, cancel: h.target, stop: func(c context.Context, in execution.Call, a execution.Association, op string) error {
		if point == "target-cancelled" {
			if e := h.target.Cancel(c, in, a, op); e != nil {
				return e
			}
		}
		os.Exit(73)
		return nil
	}}); e != nil {
		return e
	}
	_, e = h.exec.RunCancel(ctx, cancel)
	return e
}
func recovery(ctx context.Context, exe, point string) error {
	root, e := os.MkdirTemp("", "async-crash-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(root)
	cmd := exec.CommandContext(ctx, exe, "async-crash-probe", root, point)
	output, e := cmd.CombinedOutput()
	if code, ok := e.(*exec.ExitError); !ok || code.ExitCode() != 73 {
		return fmt.Errorf("probe %s: %v %s", point, e, output)
	}
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
	out, e := h.client.GetInvocation(ctx, cp.Request.OperationID)
	if e != nil {
		return e
	}
	initialChecks := out.Checks
	if _, e = h.client.Invoke(ctx, cp.Request, ""); e != nil {
		return e
	}
	if point == "cancel-accepted" {
		if _, e = h.exec.RunCancel(ctx, cp.Cancel); e != nil {
			return e
		}
	}
	if point == "cancel-registered" {
		c, e := h.client.GetCancel(ctx, cp.Cancel)
		if e != nil || !c.Sent || c.Progress != "UNKNOWN" {
			return fmt.Errorf("cancel registration lost: %v", e)
		}
		if _, e = h.exec.RunCancel(ctx, cp.Cancel); e != nil {
			return e
		} // Never resends unknown Cancel.
	}
	if point != "cancel-accepted" && point != "target-cancelled" {
		if e = h.target.Complete(ctx, cp.Request.OperationID); e != nil {
			return e
		}
	}
	if out.Effect == "UNKNOWN" {
		if _, e = h.client.Reconcile(ctx, cp.Request.OperationID); e != nil {
			return e
		}
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		return e
	}
	task, e := h.core.Get(ctx, h.token, cp.Request.Qualification.Ref)
	if e != nil {
		return e
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		return e
	}
	again, e := h.core.Get(ctx, h.token, cp.Request.Qualification.Ref)
	if e != nil || again.Version != task.Version {
		return fmt.Errorf("report replay advanced core")
	}
	target, e := h.target.Snapshot(ctx)
	if e != nil {
		return e
	}
	want := 1
	if point == "cancel-accepted" || point == "target-cancelled" {
		want = 0
	}
	if target.Jobs != 1 || target.Changes != want {
		return fmt.Errorf("target %+v", target)
	}
	out, e = h.client.GetInvocation(ctx, cp.Request.OperationID)
	if e != nil {
		return e
	}
	if out.Checks < initialChecks {
		return fmt.Errorf("recovery budget reset")
	}
	if out.Applied != out.Revision {
		return fmt.Errorf("outbox not acknowledged")
	}
	return nil
}
