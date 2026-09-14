package tasks

import (
	"context"
	"encoding/json"
	"lerna/authorization"
)

func taskScope(ref Ref) string {
	if ref == (Ref{}) {
		return ""
	}
	return ref.Namespace + "/" + ref.TaskID
}
func checkRuntimeScope(tx authorization.RuntimeTransaction, op string, ref Ref) error {
	if s, ok := tx.(authorization.RuntimeOperationScope); ok {
		return s.CheckRuntimeScope(op, taskScope(ref))
	}
	return nil
}
func (p *HandoffPort) OriginalRequest(ctx context.Context, ref Ref, handoff, original string) (HandoffEnvelope, error) {
	h, e := p.Status(ctx, ref, handoff)
	if e != nil {
		return HandoffEnvelope{}, e
	}
	if h.Phase != "ACTIVE" || h.Request.Target != p.service.config.Owner || len(original) == 0 || len(original) > 512 {
		return HandoffEnvelope{}, failure(authorization.Denied)
	}
	m := handoffMessageOf(h, "validate-admission")
	m.Checkpoint = []byte(original)
	return p.sign(m)
}

// ValidateOriginal consults the original issuer's still-live window, without
// exporting its HMAC secret. The result reserves identity, not business effects.
func (p *HandoffPort) ValidateOriginal(ctx context.Context, in HandoffEnvelope) (HandoffEnvelope, error) {
	m, e := p.verify(in, "validate-admission")
	if e != nil {
		return HandoffEnvelope{}, e
	}
	h, e := p.Status(ctx, m.Request.Ref, m.Request.OperationID)
	if e != nil {
		return HandoffEnvelope{}, e
	}
	if h.Phase != "SEALED" || !matchesHandoff(h, m) || h.Source != p.service.config.Owner || len(m.Checkpoint) == 0 || len(m.Checkpoint) > 512 {
		return HandoffEnvelope{}, failure(authorization.Denied)
	}
	var admission authorization.RuntimeAdmission
	e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r := j.Runs[m.Request.Ref.TaskID]
		if e := p.authorize(tx, r); e != nil {
			return e
		}
		op := string(m.Checkpoint)
		if collaborationOperation(j, op) {
			return failure(authorization.IdentityConflict)
		}
		if _, ok := j.Operations[op]; ok {
			return failure(authorization.IdentityConflict)
		}
		if _, ok := j.Controls[op]; ok {
			return failure(authorization.IdentityConflict)
		}
		if _, ok := j.InputChanges[op]; ok {
			return failure(authorization.IdentityConflict)
		}
		if e := tx.Operation(op, r.Task.Subject, true); e != nil {
			return e
		}
		scope, ok := tx.(authorization.RuntimeOperationScope)
		if !ok {
			return failure(authorization.Unsupported)
		}
		if e := scope.BindRuntimeScope(op, taskScope(r.Task.Ref)); e != nil {
			return e
		}
		a, ok := tx.(authorization.RuntimeHandoffTransaction)
		if !ok {
			return failure(authorization.Unsupported)
		}
		var e error
		admission, e = a.ExportRuntimeAdmission([]string{op})
		return e
	})
	if e != nil {
		return HandoffEnvelope{}, e
	}
	m.Kind = "admission"
	m.Checkpoint, e = json.Marshal(admission)
	if e != nil {
		return HandoffEnvelope{}, e
	}
	return p.sign(m)
}
func (p *HandoffPort) ImportOriginal(ctx context.Context, in HandoffEnvelope) error {
	m, e := p.verify(in, "admission")
	if e != nil {
		return e
	}
	h, e := p.Status(ctx, m.Request.Ref, m.Request.OperationID)
	if e != nil {
		return e
	}
	if h.Phase != "ACTIVE" || !matchesHandoff(h, m) || m.Request.Target != p.service.config.Owner {
		return failure(authorization.Denied)
	}
	var admission authorization.RuntimeAdmission
	if json.Unmarshal(m.Checkpoint, &admission) != nil || len(admission.Operations) != 1 || len(admission.Scopes) != 1 {
		return failure(authorization.Invalid)
	}
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		current := j.Handoffs[m.Request.Ref.TaskID]
		if current.Phase != "ACTIVE" || !matchesHandoff(current, m) {
			return failure(authorization.Conflict)
		}
		r := j.Runs[m.Request.Ref.TaskID]
		if e := p.authorize(tx, r); e != nil {
			return e
		}
		for op, subject := range admission.Operations {
			if subject != r.Task.Subject || admission.Scopes[op] != taskScope(r.Task.Ref) {
				return failure(authorization.Denied)
			}
			if tx.Now().UnixNano() >= admission.WindowExpires {
				return failure(authorization.Expired)
			}
		}
		a, ok := tx.(authorization.RuntimeHandoffTransaction)
		if !ok {
			return failure(authorization.Unsupported)
		}
		return a.ImportRuntimeAdmission(admission)
	})
}
