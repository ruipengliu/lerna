package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"time"
)

// ExecutionTransaction is a trusted local unit of work. Execution and task
// partitions share a CAS for current qualification, without sharing formats.
// No callback may invoke another authority/store or perform external I/O.
type ExecutionTransaction interface {
	RuntimeTransaction
	ExecutionData() []byte
	SetExecutionData([]byte)
	ExecutionOperation(string, string, bool) error
	ReserveExecutionOperation(string, string) error
	ExecutionControlOperation(string, string) error
	AuthorizeAction(string, *wire.AuthorizationAction) (Identity, error)
	ValidateUse(UsePermit, GrantPresentation, *wire.AuthorizationAction) error
}
type executionTransaction struct {
	*runtimeTransaction
	state *State
	grant *GrantAuthority
}

func (t *executionTransaction) ExecutionData() []byte     { return t.state.ExecutionData }
func (t *executionTransaction) SetExecutionData(v []byte) { t.state.ExecutionData = v }
func (t *executionTransaction) AuthorizeAction(token string, a *wire.AuthorizationAction) (Identity, error) {
	p, e := authenticate(t.state, token, t.now)
	if e != nil {
		return Identity{}, e
	}
	d, e := t.grant.service.evaluate(t.state, p, a, t.now)
	if e != nil {
		return Identity{}, e
	}
	if !d.Allowed {
		return Identity{}, fail(Denied)
	}
	return Identity{Namespace: t.state.Namespace, Subject: p.Subject}, nil
}
func (t *executionTransaction) ExecutionOperation(id, subject string, claim bool) error {
	st := t.state
	epoch, e := windowOf(st, id)
	if e != nil {
		return e
	}
	if _, ok := st.Operations[id]; ok {
		return fail(IdentityConflict)
	}
	if _, ok := st.MemoryOperations[id]; ok {
		return fail(IdentityConflict)
	}
	if _, ok := st.ContentOperations[id]; ok {
		return fail(IdentityConflict)
	}
	if _, ok := st.RuntimeOperations[id]; ok {
		return fail(IdentityConflict)
	}
	if st.Signed != nil {
		if _, ok := st.Signed.Operations[id]; ok {
			return fail(IdentityConflict)
		}
	}
	if old, ok := st.ExecutionOperations[id]; ok {
		if old != subject {
			return fail(Denied)
		}
		if claim {
			delete(st.ExecutionReservations, id)
		}
		return nil
	}
	if epoch <= st.ClosedThrough || epoch != st.Window || t.now.UnixNano() >= st.WindowExpires {
		return fail(Expired)
	}
	if claim {
		if st.ExecutionOperations == nil {
			st.ExecutionOperations = map[string]string{}
		}
		st.ExecutionOperations[id] = subject
	}
	return nil
}
func (t *executionTransaction) ValidateUse(use UsePermit, p GrantPresentation, a *wire.AuthorizationAction) error {
	st := t.state
	g := t.grant
	if a == nil || !known(a) || st.Namespace != p.Namespace || !use.matches(p) || use.Units != 1 || st.Signed == nil || st.Signed.Uses[p.OperationID] != use {
		return fail(Denied)
	}
	bytes, e := proto.MarshalOptions{Deterministic: true}.Marshal(a)
	if e != nil {
		return fail(Invalid)
	}
	digest := sha256.Sum256(bytes)
	if use.ActionSHA256 != hex.EncodeToString(digest[:]) {
		return fail(Denied)
	}
	if e = g.chain(st, use.GrantID, t.now); e != nil {
		return e
	}
	entry := st.Signed.Grants[use.GrantID]
	spec := entry.Record.Spec
	if spec.Subject != p.Subject || spec.Audience != p.Audience || spec.Presenter != p.Presenter || spec.CertificateSha256 != p.CertificateSHA256 || t.now.Unix() < spec.NotBefore {
		return fail(Denied)
	}
	b := g.service.newBudget()
	ok, e := g.service.matches(st, spec.Scope, a, t.now, b)
	if e != nil {
		return e
	}
	if !ok {
		return fail(Denied)
	}
	ok, _, e = g.service.policyAllows(st, a, t.now, b)
	if e != nil {
		return e
	}
	if !ok {
		return fail(Denied)
	}
	if len(a.RequiredConstraints) > 0 {
		return fail(Unsupported)
	}
	return nil
}
func (g *GrantAuthority) UpdateExecution(ctx context.Context, fn func(ExecutionTransaction) error) error {
	if fn == nil {
		return fail(Invalid)
	}
	return g.service.update(ctx, func(st *State, now time.Time) error {
		if e := g.journal(st); e != nil {
			return e
		}
		tx := &executionTransaction{g.service.runtime(st, now), st, g}
		if e := fn(tx); e != nil {
			return e
		}
		if len(st.ExecutionData) > 4<<20 {
			return fail(Unavailable)
		}
		st.RuntimeData = tx.data
		return nil
	})
}

// ExecutionControlOperation cannot repurpose an allocated execution permit.
func (t *executionTransaction) ExecutionControlOperation(id, subject string) error {
	if t.state.ExecutionReservations[id] {
		return fail(IdentityConflict)
	}
	if t.state.Signed != nil {
		if _, ok := t.state.Signed.Uses[id]; ok {
			return fail(IdentityConflict)
		}
	}
	return t.ExecutionOperation(id, subject, true)
}

// ReserveExecutionOperation retains a proposal identity beyond its issuance
// window. It reserves the domain only; Core admission and a valid UsePermit
// are still required before an Invocation can be recorded or started.
func (t *executionTransaction) ReserveExecutionOperation(id, subject string) error {
	st := t.state
	if old, ok := st.ExecutionOperations[id]; ok {
		if old != subject {
			return fail(Denied)
		}
		if !st.ExecutionReservations[id] {
			return fail(IdentityConflict)
		}
		return nil
	}
	if st.Signed != nil {
		if _, ok := st.Signed.Uses[id]; ok {
			return fail(IdentityConflict)
		}
	}
	if len(st.ExecutionReservations) >= t.grant.config.MaxRecords {
		return fail(Unavailable)
	}
	if err := t.ExecutionOperation(id, subject, true); err != nil {
		return err
	}
	if st.ExecutionReservations == nil {
		st.ExecutionReservations = map[string]bool{}
	}
	st.ExecutionReservations[id] = true
	return nil
}
