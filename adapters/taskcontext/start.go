package taskcontext

import (
	"context"
	"encoding/json"
	"lerna/contextassembly"
	"lerna/execution"
	"lerna/tasks"
)

type startGuard struct {
	session                *Session
	task                   tasks.Task
	operation, fingerprint string
}

// StartGuard binds one admitted operation to the original decision Session.
// The host restores the original request/session before binding an execution
// service. A new request or operation cannot reuse this guard's context identity.
func (s *Session) StartGuard(request execution.Request, task tasks.Task) (execution.StartGuard, error) {
	if s == nil || request.OperationID == "" || request.InputRef == "" || request.Qualification.Ref != task.Ref {
		return nil, contextassembly.Invalid
	}
	if e := s.check(task, s.request.Location); e != nil {
		return nil, e
	}
	raw, e := json.Marshal(task)
	if e != nil || len(raw) > 1048576 {
		return nil, contextassembly.Invalid
	}
	var owned tasks.Task
	if json.Unmarshal(raw, &owned) != nil {
		return nil, contextassembly.Invalid
	}
	return &startGuard{session: s, task: owned, operation: request.OperationID, fingerprint: request.Fingerprint()}, nil
}
func (g *startGuard) ValidateStart(ctx context.Context, request execution.Request) error {
	if request.OperationID != g.operation || request.Fingerprint() != g.fingerprint {
		return contextassembly.Denied
	}
	return g.session.Validate(ctx, g.task, g.session.request.Location)
}
