package durable

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type Participant struct {
	engine *Engine
	name   string
}
type Metrics struct{ Committed, RolledBack, Unknown, Claims int64 }
type Engine struct {
	backend                                Backend
	options                                Options
	kinds                                  map[string]bool
	participants                           sync.Map
	committed, rolledBack, unknown, claims atomic.Int64
}

func New(backend Backend, options Options) (*Engine, error) {
	if backend == nil {
		return nil, ErrPrecondition
	}
	if options.TxTimeout == 0 {
		options.TxTimeout = 2 * time.Second
	}
	if options.Attempts == 0 {
		options.Attempts = 3
	}
	if options.TxTimeout <= 0 || options.TxTimeout > 30*time.Second || options.Attempts < 1 || options.Attempts > 5 || options.QueryRetention < 0 {
		return nil, ErrPrecondition
	}
	e := &Engine{backend: backend, options: options, kinds: map[string]bool{}}
	for _, k := range options.Kinds {
		if !validName(k) || e.kinds[k] {
			return nil, ErrUnsupported
		}
		e.kinds[k] = true
	}
	return e, nil
}

// Register is for trusted assembly. Keep capabilities inside repository adapters.
func (e *Engine) Register(name string) (Participant, error) {
	if !validName(name) {
		return Participant{}, ErrPrecondition
	}
	if _, loaded := e.participants.LoadOrStore(name, true); loaded {
		return Participant{}, ErrPrecondition
	}
	return Participant{e, name}, nil
}
func (e *Engine) Metrics() Metrics {
	return Metrics{e.committed.Load(), e.rolledBack.Load(), e.unknown.Load(), e.claims.Load()}
}

type transactionMarker struct{}
type Tx struct {
	engine                             *Engine
	scope                              Scope
	ctx                                context.Context
	session                            Session
	active                             atomic.Bool
	gate                               sync.Mutex
	invalid                            atomic.Bool
	allowed                            map[string]bool
	locked                             map[string]bool
	lastCommand, lastBusiness, lastJob string
	reserved                           map[CommandKey]bool
	commands                           map[CommandKey]bool
	raised                             map[JobKey]string
	guarded                            map[string]int64
	wake                               bool
	poison                             error
}

func (t *Tx) Scope() Scope             { return t.scope }
func (t *Tx) Context() context.Context { return t.ctx }
func (t *Tx) fail(err error) error {
	if err != nil && t.poison == nil {
		t.poison = err
	}
	return err
}
func (t *Tx) enter() error {
	if !t.active.Load() || t.ctx.Err() != nil {
		return ErrTx
	}
	if !t.gate.TryLock() {
		t.invalid.Store(true)
		return ErrTx
	}
	if !t.active.Load() || t.ctx.Err() != nil {
		t.gate.Unlock()
		return ErrTx
	}
	return nil
}
func (t *Tx) leave() { t.gate.Unlock() }

// Lock declares and acquires one repository business key, before all job locks.
// handle is adapter-only local access; it carries no Commit/Rollback methods.
func (t *Tx) Lock(p Participant, key string, fn func(any) error) error {
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	name := p.name + "\x00" + key
	if p.engine != t.engine || !t.allowed[p.name] || !validName(key) {
		return t.fail(ErrTx)
	}
	if t.locked[name] {
		return nil
	}
	if t.lastJob != "" || name < t.lastBusiness {
		return t.fail(ErrLockOrder)
	}
	adapterCtx, cancel := context.WithCancel(t.ctx)
	defer cancel()
	if err := fn(t.session.Handle(adapterCtx)); err != nil {
		return t.fail(err)
	}
	t.locked[name] = true
	t.lastBusiness = name
	return nil
}
func (t *Tx) Use(p Participant, key string, fn func(any) error) error {
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	if p.engine != t.engine || !t.allowed[p.name] || !t.locked[p.name+"\x00"+key] {
		return t.fail(ErrTx)
	}
	adapterCtx, cancel := context.WithCancel(t.ctx)
	defer cancel()
	return t.fail(fn(t.session.Handle(adapterCtx)))
}
func (t *Tx) commandOrder(k CommandKey) error {
	name := k.ServiceID + "\x00" + k.CommandID
	if !k.valid() || t.lastBusiness != "" || t.lastJob != "" || name < t.lastCommand {
		return t.fail(ErrLockOrder)
	}
	t.lastCommand = name
	t.commands[k] = true
	return nil
}
func (t *Tx) jobOrder(k JobKey) error {
	if !k.valid() || !t.engine.kinds[k.Kind] {
		return t.fail(ErrUnsupported)
	}
	if k.order() < t.lastJob {
		return t.fail(ErrLockOrder)
	}
	t.lastJob = k.order()
	return nil
}

func (e *Engine) Within(ctx context.Context, scope Scope, participants []Participant, fn func(*Tx) error) Result {
	if !scope.Valid() || fn == nil {
		return Result{RolledBack, 0, ErrScope}
	}
	if ctx.Value(transactionMarker{}) != nil {
		return Result{RolledBack, 0, ErrNested}
	}
	allowed := map[string]bool{}
	for _, p := range participants {
		if p.engine != e {
			return Result{RolledBack, 0, ErrTx}
		}
		allowed[p.name] = true
	}
	for attempt := 1; attempt <= e.options.Attempts; attempt++ {
		bounded, cancel := context.WithTimeout(ctx, e.options.TxTimeout)
		bounded = context.WithValue(bounded, transactionMarker{}, true)
		session, err := e.backend.Begin(bounded, scope)
		if err != nil {
			cancel()
			e.rolledBack.Add(1)
			if e.backend.Retryable(err) && attempt < e.options.Attempts && ctx.Err() == nil {
				continue
			}
			return Result{RolledBack, attempt, err}
		}
		t := &Tx{engine: e, scope: scope, ctx: bounded, session: session, allowed: allowed, locked: map[string]bool{}, reserved: map[CommandKey]bool{}, commands: map[CommandKey]bool{}, raised: map[JobKey]string{}, guarded: map[string]int64{}}
		t.active.Store(true)
		var panicValue any
		func() { defer func() { panicValue = recover() }(); err = fn(t) }()
		t.active.Store(false)
		if !t.gate.TryLock() {
			// Cancel local SQL before waiting for an escaped call to leave its gate.
			cancel()
			t.gate.Lock()
			err = errors.Join(err, ErrTx)
		}
		if t.invalid.Load() {
			err = errors.Join(err, ErrTx)
		}
		if err == nil {
			err = t.poison
		}
		for _, complete := range t.reserved {
			if !complete && err == nil {
				err = ErrInvariant
			}
		}
		if panicValue != nil {
			err = ErrInvariant
		}
		if err == nil && len(t.guarded) > 0 {
			var now int64
			now, err = session.Now(bounded)
			for _, until := range t.guarded {
				if err == nil && until <= now {
					err = ErrClaim
				}
			}
		}
		t.gate.Unlock()
		if err != nil || bounded.Err() != nil {
			if err == nil {
				err = bounded.Err()
			}
			rb, rbCancel := context.WithTimeout(context.Background(), e.options.TxTimeout)
			rbErr := session.Rollback(rb)
			rbCancel()
			cancel()
			if panicValue != nil {
				panic(panicValue)
			}
			if rbErr != nil {
				e.unknown.Add(1)
				return Result{CommitUnknown, attempt, errors.Join(err, rbErr)}
			}
			e.rolledBack.Add(1)
			if e.backend.Retryable(err) && attempt < e.options.Attempts && ctx.Err() == nil {
				continue
			}
			return Result{RolledBack, attempt, err}
		}
		err = session.Commit(bounded)
		cancel()
		if err != nil {
			if e.backend.CommitRolledBack(err) {
				e.rolledBack.Add(1)
				if e.backend.Retryable(err) && attempt < e.options.Attempts && ctx.Err() == nil {
					continue
				}
				return Result{RolledBack, attempt, err}
			}
			e.unknown.Add(1)
			return Result{CommitUnknown, attempt, err}
		}
		e.committed.Add(1)
		if t.wake {
			e.backend.Notify()
		}
		return Result{Committed, attempt, nil}
	}
	panic("unreachable")
}
