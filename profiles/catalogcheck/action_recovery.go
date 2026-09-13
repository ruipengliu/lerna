package catalogcheck

import (
	"context"
	"errors"
	"lerna/tasks"
)

var errActionInterrupted = errors.New("reference controller interrupted")

// Inject lost replies only after durable Core writes. The replacement Brain
// loads the same SQLite state after the worker's lease has expired.
type actionRecoveryPoint struct {
	*tasks.ActionPort
	phase       string
	interrupted bool
}

func (p *actionRecoveryPoint) Record(ctx context.Context, q tasks.Qualification, n uint32, in tasks.DecisionRecord) error {
	e := p.ActionPort.Record(ctx, q, n, in)
	if e == nil && !p.interrupted && (p.phase == "recover-record" || p.phase == "revoked-record") {
		p.interrupted = true
		return errActionInterrupted
	}
	return e
}
func (p *actionRecoveryPoint) Next(ctx context.Context, q tasks.Qualification) (tasks.Action, error) {
	a, e := p.ActionPort.Next(ctx, q)
	if e == nil && a.OperationID != "" && !p.interrupted && p.phase == "recover-dispatch" {
		p.interrupted = true
		return a, errActionInterrupted
	}
	return a, e
}
