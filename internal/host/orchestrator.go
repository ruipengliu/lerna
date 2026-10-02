package host

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
	o "github.com/ruipengliu/lerna/internal/orchestrator"
	"github.com/ruipengliu/lerna/internal/storage/sqlstore"
	"github.com/ruipengliu/lerna/internal/storage/taskstore"
)

// TaskService is trusted assembly. Engines, SQL adapters and participants never
// cross the public Orchestrator port or reach a domain/extension handler.
type TaskService struct {
	engine          *durable.Engine
	repository      *taskstore.Store
	coordinator     *o.Coordinator
	participants    []durable.Participant
	recovered       atomic.Bool
	started         atomic.Bool
	runners         []*durable.Runner
	recoverMu       sync.Mutex
	exit            chan struct{}
	onError         func(error)
	reconcileCursor string
}
type TaskServiceOptions struct {
	Scope                                    durable.Scope
	ServiceID                                string
	Limits                                   o.Limits
	Capacity                                 int
	OnError                                  func(error)
	ProviderConcurrency, ResourceConcurrency int
}

func NewTaskService(backend durable.Backend, queries taskstore.Queries, ports api.OrchestratorPorts, options TaskServiceOptions) (*TaskService, error) {
	if !options.Scope.Valid() || options.ServiceID == "" || ports.Clock == nil || ports.Authority == nil {
		return nil, durable.ErrPrecondition
	}
	if options.Limits == (o.Limits{}) {
		options.Limits = o.DefaultLimits()
	}
	if !options.Limits.Valid() {
		return nil, durable.ErrPrecondition
	}
	if options.Capacity == 0 {
		options.Capacity = 4
	}
	if options.Capacity < 1 || options.Capacity > 16 {
		return nil, durable.ErrPrecondition
	}
	if options.ProviderConcurrency == 0 {
		options.ProviderConcurrency = 1
	}
	if options.ResourceConcurrency == 0 {
		options.ResourceConcurrency = 1
	}
	if options.ProviderConcurrency < 1 || options.ProviderConcurrency > 32 || options.ResourceConcurrency < 1 || options.ResourceConcurrency > 32 {
		return nil, durable.ErrPrecondition
	}
	selectable, ok := backend.(interface {
		SetCandidateSelector(durable.Scope, sqlstore.CandidateSelector) error
	})
	if !ok {
		return nil, durable.ErrPrecondition
	}
	if e := selectable.SetCandidateSelector(options.Scope, taskstore.Scheduler(queries, options.ProviderConcurrency, options.ResourceConcurrency)); e != nil {
		return nil, e
	}
	engine, e := durable.New(backend, durable.Options{Kinds: o.Kinds, TxTimeout: 2 * time.Second, QueryRetention: 24 * time.Hour})
	if e != nil {
		return nil, e
	}
	participant, e := engine.Register("orchestrator")
	if e != nil {
		return nil, e
	}
	repo := taskstore.New(participant, queries)
	s := &TaskService{engine: engine, exit: make(chan struct{}), onError: options.OnError, repository: repo, participants: []durable.Participant{participant}, coordinator: &o.Coordinator{Repository: repo, Ports: ports, Config: o.Config{Scope: options.Scope, ServiceID: options.ServiceID, Limits: options.Limits}}}
	// Control and settlement own slots even when all goal work is blocked.
	for _, group := range [][]string{{"control"}, {"settle"}, {"decide", "dispatch", "poll", "verify", "extract"}} {
		capacity := options.Capacity
		if len(group) == 1 {
			capacity = 1
		}
		bindings := []durable.Binding{}
		for _, kind := range group {
			bindings = append(bindings, durable.Binding{Kind: kind, Handler: durable.HandlerFunc(func(ctx context.Context, w *durable.Work) error {
				return s.coordinator.Handle(ctx, taskUnit{w, s.participants})
			})})
		}
		r, e := durable.NewRunner(engine, bindings, durable.RunnerOptions{Scope: options.Scope, Capacity: capacity, Batch: 1, Steps: 128, Scan: 20 * time.Millisecond, Lease: 3 * time.Second, Renew: 500 * time.Millisecond, WorkTimeout: 20 * time.Second, Drain: 5 * time.Second, OnError: options.OnError})
		if e != nil {
			return nil, e
		}
		s.runners = append(s.runners, r)
	}
	return s, nil
}

type taskUnit struct {
	w            *durable.Work
	participants []durable.Participant
}

func (u taskUnit) Claim() durable.Claim                            { return u.w.Claim() }
func (u taskUnit) Step(ctx context.Context, fn func() error) error { return u.w.Step(ctx, fn) }
func (u taskUnit) Within(ctx context.Context, fn func(*durable.Tx) error) durable.Result {
	return u.w.Within(ctx, u.participants, fn)
}
func taskOutcome(result durable.Result) error {
	if result.Outcome == durable.CommitUnknown {
		return api.ErrCommitUnknown
	}
	return result.Err
}
func commandResult(record durable.CommandRecord) api.CommandResult {
	v := api.CommandResult{CommandID: record.Key.CommandID, Stage: record.State}
	if record.Receipt != nil {
		v.ResourceID = record.Receipt.ResourceID
		v.Revision = record.Receipt.Revision
		v.Output = append(json.RawMessage(nil), record.Receipt.Output...)
		if len(record.Receipt.Error) > 0 {
			v.Failure = &api.Error{}
			_ = json.Unmarshal(record.Receipt.Error, v.Failure)
		}
	}
	return v
}
func (s *TaskService) Ready() bool { return s.recovered.Load() && s.coordinator.Ready() }
func (s *TaskService) lookup(ctx context.Context, caller api.Caller, id string) (durable.CommandRecord, error) {
	var record durable.CommandRecord
	result := s.engine.Within(ctx, s.coordinator.Config.Scope, nil, func(tx *durable.Tx) error {
		var e error
		record, e = tx.Lookup(durable.CommandKey{ServiceID: s.coordinator.Config.ServiceID, CommandID: id})
		if e != nil {
			return e
		}
		record, e = s.coordinator.Disclose(tx.Context(), caller, record)
		return e
	})
	return record, taskOutcome(result)
}
func (s *TaskService) CommandStatus(ctx context.Context, caller api.Caller, id string) (api.CommandResult, error) {
	if caller.TenantID != s.coordinator.Config.Scope.TenantID || caller.ActorID == "" {
		return api.CommandResult{}, durable.ErrScope
	}
	r, e := s.lookup(ctx, caller, id)
	return commandResult(r), e
}
func (s *TaskService) Execute(ctx context.Context, caller api.Caller, command api.Command) (api.CommandResult, error) {
	if e := s.coordinator.Metadata(caller, command.Method, command.TargetID, command.Payload, true); e != nil {
		return api.CommandResult{}, e
	}
	intent, e := durable.FixIntent(durable.Intent{CommandID: command.CommandID, Method: command.Method, TargetID: command.TargetID, ExpiresAt: command.ExpiresAt, ExpectedRevision: command.ExpectedRevision, Payload: command.Payload})
	if e != nil {
		return api.CommandResult{}, e
	}
	// The original receipt does not depend on availability of its old inputs or
	// policy adapter. Auth/route and current disclosure still apply on replay.
	r, e := s.lookup(ctx, caller, command.CommandID)
	if e == nil {
		if r.Digest != intent.Digest() {
			return api.CommandResult{}, durable.ErrConflict
		}
		if r.State == "gone" {
			return api.CommandResult{}, durable.ErrGone
		}
		return commandResult(r), nil
	}
	if !errors.Is(e, durable.ErrNotFound) {
		return api.CommandResult{}, e
	}
	if !s.allowed(command.Method) {
		return api.CommandResult{}, &api.Failure{Detail: api.Error{Code: "dependency_unavailable", Message: "original dependencies and domain recovery must be ready", Retry: "same_command"}}
	}
	prepared, prepareError := s.coordinator.Prepare(ctx, caller, intent)
	record, result := s.engine.Admit(ctx, s.coordinator.Config.Scope, s.coordinator.Config.ServiceID, intent, durable.Admission{Participants: s.participants, Check: func(ctx context.Context, _ durable.Scope, i durable.FixedIntent) error {
		return s.coordinator.Check(ctx, caller, i)
	}, Disclose: func(ctx context.Context, _ durable.Scope, r durable.CommandRecord) (durable.CommandRecord, error) {
		return s.coordinator.Disclose(ctx, caller, r)
	}, NewAllowed: func() bool { return s.allowed(command.Method) }, Apply: func(tx *durable.Tx, i durable.FixedIntent) (durable.Decision, error) {
		if prepareError != nil {
			var f *api.Failure
			if !errors.As(prepareError, &f) {
				return durable.Decision{}, prepareError
			}
			return durable.Decision{Receipt: &durable.Receipt{CommandID: i.CommandID(), Stage: "rejected", Error: o.Raw(f.Detail)}}, nil
		}
		d, e := s.coordinator.Apply(tx, prepared)
		if e == nil {
			e = s.coordinator.ReceiptValid(i, d)
		}
		return d, e
	}})
	return commandResult(record), taskOutcome(result)
}
func (s *TaskService) Query(ctx context.Context, caller api.Caller, q api.Query) (api.QueryResult, error) {
	if e := s.coordinator.Metadata(caller, q.Method, q.TargetID, q.Payload, false); e != nil {
		return api.QueryResult{}, e
	}
	generation, e := s.coordinator.Ports.Authority.DisclosureGeneration(ctx, caller)
	if e != nil {
		return api.QueryResult{}, e
	}
	var result api.QueryResult
	r := s.engine.Within(ctx, s.coordinator.Config.Scope, s.participants, func(tx *durable.Tx) error {
		var e error
		result, e = s.coordinator.Query(tx, caller, q, generation)
		return e
	})
	if e = taskOutcome(r); e != nil {
		return api.QueryResult{}, e
	}
	current, e := s.coordinator.Ports.Authority.DisclosureGeneration(ctx, caller)
	if e != nil {
		return api.QueryResult{}, e
	}
	if generation != current {
		return api.QueryResult{}, &api.Failure{Detail: api.Error{Code: "revision_conflict", Message: "disclosure scope changed during page read", Retry: "after_change"}}
	}
	return result, nil
}
func (s *TaskService) Recover(ctx context.Context) error {
	s.recoverMu.Lock()
	defer s.recoverMu.Unlock()
	cursor := ""
	for {
		tasks := []*o.TaskState{}
		r := s.engine.Within(ctx, s.coordinator.Config.Scope, s.participants, func(tx *durable.Tx) error {
			if e := s.repository.Read(tx); e != nil {
				return e
			}
			var e error
			tasks, e = s.repository.Recovery(tx, cursor, s.coordinator.Config.Limits.Page)
			return e
		})
		if e := taskOutcome(r); e != nil {
			return e
		}
		for _, task := range tasks {
			r = s.engine.Within(ctx, s.coordinator.Config.Scope, s.participants, func(tx *durable.Tx) error { return s.coordinator.RecoverTask(tx, task.Task.TaskID) })
			if e := taskOutcome(r); e != nil {
				return e
			}
			cursor = task.Task.TaskID
		}
		if len(tasks) < s.coordinator.Config.Limits.Page {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	for _, kind := range []string{"receiver_work", "defect_impact"} {
		cursor := ""
		for {
			next := ""
			r := s.engine.Within(ctx, s.coordinator.Config.Scope, s.participants, func(tx *durable.Tx) error {
				var e error
				next, e = s.coordinator.RecoverGlobal(tx, kind, cursor, s.coordinator.Config.Limits.Page)
				return e
			})
			if e := taskOutcome(r); e != nil {
				return e
			}
			if next == "" {
				break
			}
			cursor = next
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
	s.recovered.Store(true)
	return nil
}
func (s *TaskService) Run(ctx context.Context) error {
	if !s.started.CompareAndSwap(false, true) {
		return durable.ErrPrecondition
	}
	defer close(s.exit)
	if e := s.Recover(ctx); e != nil {
		return e
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); s.reconcileLoop(ctx) }()
	errs := make(chan error, len(s.runners))
	for _, r := range s.runners {
		wg.Add(1)
		go func(r *durable.Runner) { defer wg.Done(); errs <- r.Run(ctx) }(r)
	}
	wg.Wait()
	close(errs)
	var result error
	for e := range errs {
		result = errors.Join(result, e)
	}
	return result
}
func (s *TaskService) Wait(ctx context.Context) error {
	if !s.started.Load() {
		return durable.ErrPrecondition
	}
	select {
	case <-s.exit:
	case <-ctx.Done():
		return ctx.Err()
	}
	var result error
	for _, r := range s.runners {
		e := r.Wait(ctx)
		if !errors.Is(e, durable.ErrPrecondition) {
			result = errors.Join(result, e)
		}
	}
	return result
}
func (s *TaskService) allowed(method string) bool {
	if method == "task.cancel" || method == "task.pause" {
		return s.recovered.Load() && s.coordinator.Ports.Clock.Ready() && s.coordinator.Ports.Authority.AdmissionReady()
	}
	return s.Ready()
}
func (s *TaskService) reconcileLoop(ctx context.Context) {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			e := s.reconcilePage(ctx)
			if e != nil && s.onError != nil && ctx.Err() == nil {
				s.onError(e)
			}
		}
	}
}
func (s *TaskService) reconcilePage(ctx context.Context) error {
	s.recoverMu.Lock()
	defer s.recoverMu.Unlock()
	var tasks []*o.TaskState
	r := s.engine.Within(ctx, s.coordinator.Config.Scope, s.participants, func(tx *durable.Tx) error {
		if e := s.repository.Read(tx); e != nil {
			return e
		}
		var e error
		tasks, e = s.repository.Recovery(tx, s.reconcileCursor, 1)
		return e
	})
	if e := taskOutcome(r); e != nil {
		return e
	}
	if len(tasks) == 0 {
		s.reconcileCursor = ""
	} else {
		r = s.engine.Within(ctx, s.coordinator.Config.Scope, s.participants, func(tx *durable.Tx) error { return s.coordinator.RecoverTask(tx, tasks[0].Task.TaskID) })
		if e := taskOutcome(r); e != nil {
			return e
		}
		s.reconcileCursor = tasks[0].Task.TaskID
	}
	for _, kind := range []string{"receiver_work", "defect_impact"} {
		r = s.engine.Within(ctx, s.coordinator.Config.Scope, s.participants, func(tx *durable.Tx) error { _, e := s.coordinator.RecoverGlobal(tx, kind, "", 1); return e })
		if e := taskOutcome(r); e != nil {
			return e
		}
	}
	return nil
}
func (s *TaskService) ReconcileBilling(ctx context.Context, caller api.Caller, q api.BillingQuery) error {
	var binding o.Reservation
	r := s.engine.Within(ctx, s.coordinator.Config.Scope, s.participants, func(tx *durable.Tx) error {
		var e error
		binding, e = s.coordinator.BillingBinding(tx, caller, q)
		return e
	})
	if e := taskOutcome(r); e != nil {
		return e
	}
	q.ObjectOwnerID, q.ObjectKind, q.ObjectID, q.Source = binding.ObjectOwnerID, binding.ObjectKind, binding.ObjectID, binding.Source
	ports := s.coordinator.Ports
	if ports.Billing == nil || ports.Content == nil || !ports.Clock.Ready() {
		return durable.ErrPrecondition
	}
	if e := ports.Authority.Current(ctx, caller, api.Access{Method: "billing.fact.receive", TargetID: q.ObjectID, TaskID: q.TaskRef.TaskID}, ports.Clock.Now()); e != nil {
		return e
	}
	bill, e := ports.Billing.Read(ctx, caller, q)
	if e != nil {
		return e
	}
	body, e := ports.Content.Read(ctx, caller, bill.ProofRef)
	if e != nil {
		return e
	}
	if e = o.ContentMatches(bill.ProofRef, body); e != nil {
		return e
	}
	r = s.engine.Within(ctx, s.coordinator.Config.Scope, s.participants, func(tx *durable.Tx) error { return s.coordinator.ReconcileBilling(tx, caller, q, bill) })
	return taskOutcome(r)
}

var _ api.Orchestrator = (*TaskService)(nil)

func (s *TaskService) RegisterEvidenceDefect(ctx context.Context, caller api.Caller, d api.EvidenceDefect) error {
	if e := s.coordinator.PrepareDefect(ctx, caller, d); e != nil {
		return e
	}
	r := s.engine.Within(ctx, s.coordinator.Config.Scope, s.participants, func(tx *durable.Tx) error { return s.coordinator.RegisterDefect(tx, caller, d) })
	return taskOutcome(r)
}
