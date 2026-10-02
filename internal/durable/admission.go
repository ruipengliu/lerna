package durable

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/contracts"
)

type Preparation struct {
	SourceRef string
	Job       JobKey
}
type Decision struct {
	Receipt     *Receipt
	Preparation *Preparation
}

// Admission ports are trusted method/schema/auth/route and current disclosure checks.
// Check runs on every request. Apply performs only local, recoverable work.
type Admission struct {
	Check        func(context.Context, Scope, FixedIntent) error
	Disclose     func(context.Context, Scope, CommandRecord) (CommandRecord, error)
	NewAllowed   func() bool
	Apply        func(*Tx, FixedIntent) (Decision, error)
	Participants []Participant
}

func (e *Engine) Admit(ctx context.Context, scope Scope, service string, intent FixedIntent, a Admission) (CommandRecord, Result) {
	if a.Check == nil || a.Disclose == nil || a.NewAllowed == nil || a.Apply == nil || intent.digest == "" || !validName(service) {
		return CommandRecord{}, Result{RolledBack, 0, ErrPrecondition}
	}
	policy, ok := contracts.CommandPolicy(intent.Method())
	if !ok {
		return CommandRecord{}, Result{RolledBack, 0, ErrUnsupported}
	}
	if policy.ExpectedRevision != (intent.intent.ExpectedRevision != nil) {
		return CommandRecord{}, Result{RolledBack, 0, ErrPrecondition}
	}
	key := CommandKey{service, intent.CommandID()}
	var record CommandRecord
	result := e.Within(ctx, scope, a.Participants, func(t *Tx) error {
		if err := a.Check(t.ctx, scope, intent); err != nil {
			return err
		}
		var created bool
		var err error
		record, created, err = t.reserve(CommandRecord{Key: key, Digest: intent.digest, Method: intent.Method(), TargetID: intent.TargetID(), State: "reserved", ExpiresAt: intent.expires})
		if err != nil {
			return err
		}
		if !created {
			// Disclosure is checked for every stored result, including conflict/gone.
			disclosed, err := a.Disclose(t.ctx, scope, record)
			if err != nil {
				return err
			}
			if record.Digest != intent.digest {
				return ErrConflict
			}
			if record.State == "gone" {
				return ErrGone
			}
			if record.State == "reserved" {
				return ErrInvariant
			}
			record = disclosed
			return nil
		}
		if created {
			if !a.NewAllowed() {
				return ErrPrecondition
			}
			now, err := t.session.Now(t.ctx)
			if err != nil {
				return err
			}
			if intent.expires <= now {
				return ErrExpired
			}
			decision, err := a.Apply(t, intent)
			if err != nil {
				return err
			}
			if decision.Receipt == nil && decision.Preparation == nil {
				return ErrInvariant
			}
			if decision.Preparation != nil {
				p := decision.Preparation
				if !validName(p.SourceRef) || t.raised[p.Job] != p.SourceRef {
					return ErrInvariant
				}
				record.State = "prepared"
				record.PreparationRef = p.SourceRef
				if err := t.save(record); err != nil {
					return err
				}
			}
			if decision.Receipt != nil {
				if err := t.Decide(key, *decision.Receipt); err != nil {
					return err
				}
				record, err = t.session.Lookup(t.ctx, scope, key)
				if err != nil {
					return err
				}
			}
		}
		record, err = a.Disclose(t.ctx, scope, record)
		return err
	})
	if result.Outcome != Committed {
		return CommandRecord{}, result
	}
	return record, result
}
func (t *Tx) reserve(record CommandRecord) (CommandRecord, bool, error) {
	if err := t.enter(); err != nil {
		return CommandRecord{}, false, err
	}
	defer t.leave()
	if err := t.commandOrder(record.Key); err != nil {
		return CommandRecord{}, false, err
	}
	r, created, err := t.session.Reserve(t.ctx, t.scope, record)
	if err != nil {
		return CommandRecord{}, false, t.fail(err)
	}
	if created {
		t.reserved[record.Key] = false
	}
	return r, created, nil
}

// ReserveLocal uses the same command ledger for an atomic local handoff. Trusted
// domain assembly validates its command before reserving; all newly reserved
// identities must be decided in this transaction. Call before business locks.
func (t *Tx) ReserveLocal(service string, intent FixedIntent) (CommandRecord, bool, error) {
	p, ok := contracts.CommandPolicy(intent.Method())
	if !ok || !validName(service) || intent.digest == "" || p.ExpectedRevision != (intent.intent.ExpectedRevision != nil) {
		return CommandRecord{}, false, t.fail(ErrPrecondition)
	}
	r, created, err := t.reserve(CommandRecord{Key: CommandKey{service, intent.CommandID()}, Digest: intent.digest, Method: intent.Method(), TargetID: intent.TargetID(), State: "reserved", ExpiresAt: intent.expires})
	if err != nil {
		return r, false, err
	}
	if r.Digest != intent.digest {
		return r, false, t.fail(ErrConflict)
	}
	if r.State == "gone" {
		return r, false, t.fail(ErrGone)
	}
	if created {
		now, err := t.Now()
		if err != nil {
			return r, false, err
		}
		if !now.Before(time.UnixMilli(intent.expires)) {
			return r, false, t.fail(ErrExpired)
		}
	}
	return r, created, nil
}
func (t *Tx) Lookup(key CommandKey) (CommandRecord, error) {
	if err := t.enter(); err != nil {
		return CommandRecord{}, err
	}
	defer t.leave()
	if err := t.commandOrder(key); err != nil {
		return CommandRecord{}, err
	}
	r, err := t.session.Lookup(t.ctx, t.scope, key)
	return r, t.fail(err)
}
func (t *Tx) save(record CommandRecord) error {
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	if err := t.session.Save(t.ctx, t.scope, record); err != nil {
		return t.fail(err)
	}
	if _, ok := t.reserved[record.Key]; ok {
		t.reserved[record.Key] = true
	}
	return nil
}

// Decide requires a prior Lookup/Reserve before domain/job locks.
func (t *Tx) Decide(key CommandKey, receipt Receipt) error {
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	if !t.commands[key] || !key.valid() {
		return t.fail(ErrLockOrder)
	}
	r, err := t.session.Lookup(t.ctx, t.scope, key)
	if err != nil {
		return t.fail(err)
	}
	stages, ok := contracts.CommandStages(r.Method)
	allowed := false
	for _, s := range stages {
		if s == receipt.Stage {
			allowed = true
		}
	}
	if !ok || !allowed || receipt.CommandID != key.CommandID || (receipt.Revision != nil && (*receipt.Revision < 0 || *receipt.Revision > 9007199254740991)) || (receipt.Stage == "rejected" && len(receipt.Error) == 0) || (receipt.Stage != "rejected" && len(receipt.Error) != 0) {
		return t.fail(ErrInvariant)
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return t.fail(err)
	}
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return t.fail(err)
	}
	if r.State == "gone" {
		return t.fail(ErrGone)
	}
	if r.State == "applied" || r.State == "rejected" {
		previous, _ := json.Marshal(r.Receipt)
		prior, err := CanonicalJSON(previous)
		if err != nil || string(prior) != string(canonical) {
			return t.fail(ErrInvariant)
		}
		return nil
	}
	if receipt.Stage == "accepted" && (r.PreparationRef == "" || len(t.raised) == 0 && r.State == "reserved") {
		return t.fail(ErrInvariant)
	}
	now, err := t.session.Now(t.ctx)
	if err != nil {
		return t.fail(err)
	}
	r.State = receipt.Stage
	r.Receipt = &receipt
	r.RetainUntil = max(r.ExpiresAt, now) + t.engine.options.QueryRetention.Milliseconds()
	if receipt.Stage == "applied" || receipt.Stage == "rejected" {
		r.DecisionType = receipt.Stage
	}
	if err := t.session.Save(t.ctx, t.scope, r); err != nil {
		return t.fail(err)
	}
	if _, ok := t.reserved[key]; ok {
		t.reserved[key] = true
	}
	return nil
}

// Compact requires domain proof that all effects/fees/cleanup are closed.
// It keeps key/digest/target/final decision; it never deletes an identity.
func (t *Tx) Compact(key CommandKey, closedAt time.Time) error {
	r, err := t.Lookup(key)
	if err != nil {
		return err
	}
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	now, err := t.session.Now(t.ctx)
	if err != nil {
		return t.fail(err)
	}
	if closedAt.IsZero() || closedAt.UnixMilli() > now || (r.State != "applied" && r.State != "rejected") || max(r.RetainUntil, max(r.ExpiresAt, closedAt.UnixMilli())+t.engine.options.QueryRetention.Milliseconds()) > now {
		return t.fail(ErrPrecondition)
	}
	r.State = "gone"
	r.Receipt = nil
	r.PreparationRef = ""
	return t.fail(t.session.Save(t.ctx, t.scope, r))
}

// Await queries a final-only command with bounded request time. Preparation is
// distinct from absence; this loop owns no background work and cannot cancel it.
func (e *Engine) Await(ctx context.Context, scope Scope, key CommandKey, disclose func(context.Context, Scope, CommandRecord) (CommandRecord, error)) (CommandRecord, error) {
	if disclose == nil {
		return CommandRecord{}, ErrPrecondition
	}
	for {
		var r CommandRecord
		result := e.Within(ctx, scope, nil, func(t *Tx) error {
			var err error
			r, err = t.Lookup(key)
			if err != nil {
				return err
			}
			r, err = disclose(t.ctx, scope, r)
			return err
		})
		if result.Outcome != Committed {
			return CommandRecord{}, result.Err
		}
		if r.State == "gone" {
			return CommandRecord{}, ErrGone
		}
		if r.Receipt != nil {
			return r, nil
		}
		if r.State != "prepared" {
			return CommandRecord{}, ErrInvariant
		}
		select {
		case <-ctx.Done():
			return CommandRecord{}, errors.Join(ErrPreparation, ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}
