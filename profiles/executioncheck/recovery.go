package executioncheck

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/execution"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
)

var crashPoints = []string{"admitted-reply-lost", "start-registered", "target-committed", "report-outbox", "core-applied"}

type checkpoint struct {
	Token   string
	Request execution.Request
}
type crashConsumer struct{ execution.Core }

func (c crashConsumer) ConsumeExecution(ctx context.Context, r tasks.ExecutionReport) error {
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
	r, material, e := h.request(ctx)
	if e != nil {
		return e
	}
	if point == "start-registered" || point == "target-committed" {
		if e = h.replace(wrappedDriver{Driver: h.target, start: func(c context.Context, call execution.Call) error {
			if point == "target-committed" {
				if e := h.target.Start(c, call); e != nil {
					return e
				}
			}
			os.Exit(73)
			return nil
		}}, h.work); e != nil {
			return e
		}
	}
	if point == "core-applied" {
		if e = h.replace(h.target, crashConsumer{h.work}); e != nil {
			return e
		}
	}
	raw, _ := json.Marshal(checkpoint{h.token, r})
	if e = os.WriteFile(filepath.Join(root, "checkpoint.json"), raw, 0600); e != nil {
		return e
	}
	if _, e = h.client.Invoke(ctx, r, material); e != nil {
		return e
	}
	if point == "admitted-reply-lost" {
		os.Exit(73)
	}
	if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
		return e
	}
	if point == "report-outbox" {
		os.Exit(73)
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		return e
	}
	return fmt.Errorf("unreached crash point")
}
func recovery(ctx context.Context, exe, point string) error {
	root, e := os.MkdirTemp("", "execution-crash-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(root)
	cmd := exec.CommandContext(ctx, exe, "execution-crash-probe", root, point)
	result := cmd.Run()
	exit, ok := result.(*exec.ExitError)
	if !ok || exit.ExitCode() != 73 {
		return fmt.Errorf("probe %s: %v", point, result)
	}
	raw, e := os.ReadFile(filepath.Join(root, "checkpoint.json"))
	if e != nil {
		return e
	}
	var saved checkpoint
	if e = json.Unmarshal(raw, &saved); e != nil {
		return e
	}
	h, e := open(ctx, root, saved.Token)
	if e != nil {
		return e
	}
	defer h.close()
	receipt, e := h.client.Invoke(ctx, saved.Request, "")
	if e != nil || receipt.Revision != 1 {
		return fmt.Errorf("admission replay: %v", e)
	}
	target, e := h.target.Snapshot(ctx)
	if e != nil {
		return e
	}
	if point == "admitted-reply-lost" {
		if target.Changes != 0 {
			return fmt.Errorf("premature effect")
		}
		if _, e = h.exec.Run(ctx, saved.Request.OperationID); e != nil {
			return e
		}
	} else {
		start, e := h.exec.LookupCommit(ctx, saved.Request.OperationID, "start")
		if e != nil || start.Revision != 2 {
			return fmt.Errorf("lost start %v", e)
		}
		if point == "start-registered" || point == "target-committed" {
			if _, e = h.client.Reconcile(ctx, saved.Request.OperationID); e != nil {
				return e
			}
		}
	}
	before, e := h.core.Get(ctx, h.token, saved.Request.Qualification.Ref)
	if e != nil {
		return e
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		return e
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		return e
	}
	target, e = h.target.Snapshot(ctx)
	if e != nil {
		return e
	}
	r, e := h.client.GetInvocation(ctx, saved.Request.OperationID)
	if e != nil {
		return e
	}
	task, e := h.core.Get(ctx, h.token, saved.Request.Qualification.Ref)
	if e != nil {
		return e
	}
	if point == "start-registered" {
		return require(target.Changes == 0 && r.Effect == "UNKNOWN" && r.Started && task.State == "WAITING", "uncertain send repeated")
	}
	if point == "core-applied" && before.Version != task.Version {
		return fmt.Errorf("Core consumed twice")
	}
	return require(target.Value == 3 && target.Changes == 1 && r.Effect == "CONFIRMED" && r.Applied == r.Revision && task.State == "COMPLETED", "lost durable recovery")
}
