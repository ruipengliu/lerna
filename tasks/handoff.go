package tasks

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"lerna/authorization"
)

type HandoffRequest struct {
	OperationID     string
	Ref             Ref
	Target          string
	ExpectedVersion uint64
}
type HandoffState struct {
	Reports    []ExecutionReport
	Digest     string
	Checkpoint []byte
	Activation HandoffEnvelope
	Request    HandoffRequest
	Phase      string
	Source     string
	Epoch      uint64
}
type HandoffPort struct {
	config  HandoffConfig
	service *Service
	token   string
}

func (s *Service) Handoffs(token string, config HandoffConfig) (*HandoffPort, error) {
	if token == "" {
		return nil, failure(authorization.Invalid)
	}
	copy := *s
	copy.handoffManagement = true
	p := &HandoffPort{service: &copy, token: token, config: config}
	if !p.configured() {
		return nil, failure(authorization.Invalid)
	}
	p.config.PrivateKey = append([]byte(nil), config.PrivateKey...)
	p.config.Peers = map[string]ed25519.PublicKey{}
	for owner, key := range config.Peers {
		if !name(owner) || len(key) != ed25519.PublicKeySize {
			return nil, failure(authorization.Invalid)
		}
		p.config.Peers[owner] = append([]byte(nil), key...)
	}
	return p, nil
}
func (p *HandoffPort) authorize(tx authorization.RuntimeTransaction, r RunSnapshot) error {
	id, e := tx.Authorize(p.token, r.Task.Resource, "task.handoff")
	if e != nil {
		return e
	}
	if id.Subject != r.Task.Subject {
		return failure(authorization.Denied)
	}
	return nil
}
func (p *HandoffPort) Begin(ctx context.Context, in HandoffRequest) (HandoffState, error) {
	var out HandoffState
	if in.Ref.Namespace != p.service.config.Namespace || !name(in.Ref.TaskID) || !name(in.Target) || in.Target == p.service.config.Owner || len(in.OperationID) > 512 || in.OperationID == "" {
		return out, failure(authorization.Invalid)
	}
	r, e := p.authorizedRun(ctx, in.Ref)
	if e != nil {
		return out, e
	}
	if e = p.check(ctx, "begin", r, in.Target); e != nil {
		return out, e
	}
	e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, ok := j.Runs[in.Ref.TaskID]
		if !ok {
			return failure(authorization.NotFound)
		}
		if e := p.authorize(tx, r); e != nil {
			return e
		}
		if old, ok := j.HandoffHistory[in.OperationID]; ok {
			if old.Request != in {
				return failure(authorization.IdentityConflict)
			}
			out = old
			return nil
		}
		if old, ok := j.Handoffs[in.Ref.TaskID]; ok {
			if old.Request.OperationID == in.OperationID {
				if old.Request != in {
					return failure(authorization.IdentityConflict)
				}
				out = old
				return nil
			}
			if old.Phase != "ACTIVE" && old.Phase != "ABORTED" {
				return failure(authorization.Conflict)
			}
			if len(j.HandoffHistory) >= 32 {
				return failure(authorization.Unavailable)
			}
			j.HandoffHistory[old.Request.OperationID] = old
		}
		if r.Task.Owner != p.service.config.Owner || r.Task.Version != in.ExpectedVersion || terminal(r.Task) {
			return failure(authorization.Conflict)
		}
		if _, ok := j.Operations[in.OperationID]; ok {
			return failure(authorization.IdentityConflict)
		}
		if _, ok := j.Controls[in.OperationID]; ok {
			return failure(authorization.IdentityConflict)
		}
		if _, ok := j.InputChanges[in.OperationID]; ok {
			return failure(authorization.IdentityConflict)
		}
		if collaborationOperation(j, in.OperationID) {
			return failure(authorization.IdentityConflict)
		}
		if e := checkRuntimeScope(tx, in.OperationID, in.Ref); e != nil {
			return e
		}
		if e := tx.Operation(in.OperationID, r.Task.Subject, true); e != nil {
			return e
		}
		out = HandoffState{Request: in, Phase: "PREPARING", Source: r.Task.Owner, Epoch: r.Task.OwnerEpoch}
		j.Handoffs[in.Ref.TaskID] = out
		return nil
	})
	return out, e
}
func (p *HandoffPort) Abort(ctx context.Context, ref Ref, op string) error {
	return p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		if ref.Namespace != p.service.config.Namespace {
			return failure(authorization.Denied)
		}
		r, ok := j.Runs[ref.TaskID]
		if !ok {
			return failure(authorization.NotFound)
		}
		if e := p.authorize(tx, r); e != nil {
			return e
		}
		h, ok := j.Handoffs[ref.TaskID]
		if !ok || h.Request.OperationID != op {
			return failure(authorization.IdentityConflict)
		}
		if h.Phase == "ABORTED" {
			return nil
		}
		if h.Phase != "PREPARING" {
			return failure(authorization.Conflict)
		}
		h.Phase = "ABORTED"
		j.Handoffs[ref.TaskID] = h
		return nil
	})
}
func handoffFrozen(j *journal, id string) bool {
	h, ok := j.Handoffs[id]
	return ok && h.Phase != "ABORTED" && h.Phase != "ACTIVE"
}
func frozenRuns(j *journal) map[string]string {
	out := map[string]string{}
	for id, r := range j.Runs {
		if handoffFrozen(j, id) {
			b, _ := json.Marshal(r)
			out[id] = string(b)
		}
	}
	return out
}

func (p *HandoffPort) authorizedRun(ctx context.Context, ref Ref) (RunSnapshot, error) {
	var out RunSnapshot
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		if ref.Namespace != p.service.config.Namespace {
			return failure(authorization.Denied)
		}
		r, ok := j.Runs[ref.TaskID]
		if !ok {
			return failure(authorization.NotFound)
		}
		if e := p.authorize(tx, r); e != nil {
			return e
		}
		out = r
		return nil
	})
	return out, e
}
