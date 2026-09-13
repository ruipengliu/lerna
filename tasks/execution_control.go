package tasks

import (
	"context"
	"lerna/authorization"
)

// ExecutionCancelled is a trusted disposition query, not result disclosure.
func (p *WorkPort) ExecutionCancelled(ctx context.Context, q Qualification) (bool, error) {
	var cancel bool
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, ok := j.Runs[q.Ref.TaskID]
		if !ok || q.Ref.Namespace != p.service.config.Namespace {
			return failure(authorization.Denied)
		}
		id, e := tx.Authorize(p.binding.Token, r.Task.Resource, "task.reconcile")
		if e != nil {
			return e
		}
		if id.Subject != p.binding.Subject || id.Subject != r.Task.Subject {
			return failure(authorization.Denied)
		}
		if len(r.Work) != 1 || r.Work[0].ID != q.WorkID || r.Work[0].Generation != q.Generation {
			return failure(authorization.Conflict)
		}
		cancel = controlIntent(r.Task) == "CANCEL"
		return nil
	})
	return cancel, e
}
