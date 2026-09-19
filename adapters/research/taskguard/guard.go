// Package taskguard checks the current original Core work qualification before
// each acquisition boundary without consuming a second execution permit.
package taskguard

import (
	"context"
	"lerna/authorization"
	"lerna/execution"
	"lerna/fetch"
	"lerna/tasks"
	"time"
)

type Authority interface {
	UpdateRuntime(context.Context, func(authorization.RuntimeTransaction) error) error
}
type Core interface {
	GuardExecution(authorization.RuntimeTransaction, tasks.Qualification, string, bool) error
}
type TaskReader interface {
	Get(context.Context, string, tasks.Ref) (tasks.Task, error)
}
type Guard struct {
	tasks     TaskReader
	token     string
	authority Authority
	core      Core
}

func New(a Authority, c Core, reader TaskReader, token string) (*Guard, error) {
	if a == nil || c == nil || reader == nil || token == "" {
		return nil, fetch.Invalid
	}
	return &Guard{authority: a, core: c, tasks: reader, token: token}, nil
}
func (g *Guard) Check(ctx context.Context, r execution.Request) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	task, err := g.tasks.Get(ctx, g.token, r.Qualification.Ref)
	if err != nil {
		return fetch.Denied
	}
	expired := false
	err = g.authority.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error {
		expired = task.Constraints.DeadlineUnix <= tx.Now().Unix()
		return g.core.GuardExecution(tx, r.Qualification, r.OperationID, false)
	})
	if err != nil {
		if expired && task.Version == r.Qualification.Version && authorization.Is(err, authorization.Conflict) {
			// The metadata read and guard use separate transactions. Only retain the
			// timeout classification if the task version and deadline remained stable.
			current, e := g.tasks.Get(ctx, g.token, r.Qualification.Ref)
			if e == nil && current.Version == task.Version && current.Constraints.DeadlineUnix == task.Constraints.DeadlineUnix {
				return fetch.TimedOut
			}
		}
		return fetch.Denied
	}
	return nil
}
