package execution

import (
	"context"
	"lerna/authorization"
	"lerna/tasks"
	"reflect"
	"time"
)

// Run starts at most once. A may-have-sent operation is only reconciled; an
// unavailable or timed-out caller must not infer absence of external effects.
func (s *Service) Run(ctx context.Context, op string) (Record, error) {
	r, e := s.GetInvocation(ctx, op)
	if e != nil {
		return Record{}, e
	}
	if r.Started {
		return r, nil
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return Record{}, failure(authorization.Unavailable)
	}
	release := true
	defer func() {
		if release {
			<-s.slots
		}
	}()
	bounded, cancel := context.WithTimeout(ctx, s.config.IOTimeout)
	defer cancel()
	data, e := s.readContent(bounded, r.Request)
	if e != nil {
		return Record{}, e
	}
	if e = s.validate(data, s.cap.Input, s.config.MaxInput); e != nil {
		return Record{}, e
	}
	if s.startGuard != nil {
		// Context assembly has its own bounded work; it must neither inherit the
		// spent content-read deadline nor consume the next transaction's IO time.
		cancel()
		check, stop := context.WithTimeout(ctx, 5*time.Second)
		e = s.startGuard.ValidateStart(check, r.Request)
		stop()
		if e != nil {
			return Record{}, e
		}
		bounded, cancel = context.WithTimeout(ctx, s.config.IOTimeout)
		defer cancel()
	}
	started := false
	e = s.transaction(bounded, func(j *journal, tx authorization.ExecutionTransaction) error {
		started = false
		if e := s.authorize(tx, "capability.invoke"); e != nil {
			return e
		}
		current, ok := j.Records[op]
		if !ok || !s.owned(current) {
			return failure(authorization.Denied)
		}
		if current.CancelID != "" {
			r = current
			return nil
		}
		if current.Started {
			r = current
			return nil
		}
		for _, other := range j.Records {
			if other.ConflictingEvidence {
				return failure(authorization.Conflict)
			}
		}
		if e := s.guardResource(j, current.Request); e != nil {
			return e
		}
		if j.Active != "" && j.Active != op {
			return failure(authorization.Conflict)
		}
		if pending(j) >= s.config.MaxOutbox {
			return failure(authorization.Unavailable)
		}
		if e := tx.ValidateUse(current.Permit, s.binding.Presentation(current.Request), s.action("resource.change")); e != nil {
			return e
		}
		if e := s.guardActionRequest(tx, current.Request); e != nil {
			return e
		}
		if e := s.core.GuardExecution(tx, current.Request.Qualification, op, true); e != nil {
			return e
		}
		if s.cap.Async != nil {
			current.Async = &AsyncState{Until: tx.Now().Add(s.cap.Async.RecoveryWindow).UnixNano(), NextCheck: tx.Now().Add(s.cap.Async.PollInterval).UnixNano()}
		}
		current.Started = true
		current.Revision++
		current.Phase = "UNKNOWN"
		j.Active = op
		j.Records[op] = current
		j.Commits[op+"/start"] = Receipt{op, current.Revision}
		r = current
		started = true
		return nil
	})
	if e != nil {
		return Record{}, e
	}
	if !started {
		return r, nil
	}
	callCtx, stop := context.WithTimeout(ctx, s.config.DriverTimeout)
	type result struct {
		record Record
		err    error
	}
	done := make(chan result, 1)
	release = false
	go func() {
		defer func() { <-s.slots; stop() }()
		defer func() {
			if recover() != nil {
				out, e := s.finish(context.WithoutCancel(ctx), r, unknown())
				done <- result{out, e}
			}
		}()
		call := Call{Request: r.Request, Input: data, Control: s.controlQualification(r.Request)}
		if s.cap.Async != nil {
			f, e := s.driver.(AsyncDriver).StartAsync(callCtx, call)
			out := r
			if e == nil {
				save, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.config.IOTimeout)
				out, e = s.asyncFact(save, r, f)
				cancel()
			}
			done <- result{out, e}
			return
		}
		_ = s.driver.Start(callCtx, call)
		observation := unknown()
		query, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.config.IOTimeout)
		if v, e := s.driver.Inspect(query, call); e == nil {
			observation = v
		}
		cancel()
		out, e := s.finish(context.WithoutCancel(ctx), r, observation)
		done <- result{out, e}
	}()
	select {
	case out := <-done:
		return out.record, out.err
	case <-callCtx.Done():
		return s.finish(context.WithoutCancel(ctx), r, unknown())
	}
}
func unknown() Observation {
	return Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}
}
func validObservation(v Observation) bool {
	if v.Phase == "UNKNOWN" {
		return v.Result == "UNKNOWN" && v.Effect == "UNKNOWN" && len(v.Output) == 0 && len(v.Evidence) == 0
	}
	if v.Phase == "NOT_STARTED" {
		return v.Result == "FAILURE" && v.Effect == "NOT_OCCURRED" && len(v.Evidence) > 0
	}
	// A failure may retain a schema-checked diagnostic result, or only prove an
	// effect after its controlled result was retired. Neither is success.
	if v.Phase == "FINISHED" && v.Result == "FAILURE" && v.Effect == "CONFIRMED" {
		return len(v.Evidence) > 0
	}
	return v.Phase == "FINISHED" && v.Result == "SUCCESS" && v.Effect == "CONFIRMED" && len(v.Evidence) > 0
}
func pending(j *journal) int {
	n := 0
	for _, r := range j.Records {
		for _, p := range r.Reports {
			if p.Revision > r.Applied {
				n++
			}
		}
	}
	return n
}
func (s *Service) finish(ctx context.Context, original Record, v Observation) (Record, error) {
	bounded, cancel := context.WithTimeout(ctx, s.config.IOTimeout)
	defer cancel()
	if !validObservation(v) || len(v.Evidence) > s.config.MaxOutput || len(v.Output) > s.config.MaxOutput {
		v = unknown()
	}
	ref := ""
	// A known failure can carry an internal effect fact while its controlled
	// result is absent or forbidden. Do not manufacture a null-body artifact.
	if v.Effect != "UNKNOWN" && !(v.Result == "FAILURE" && len(v.Output) == 0) {
		schemaErr := s.validate(v.Output, s.cap.Output, s.config.MaxOutput)
		stored, e := s.saveContent(bounded, original.Request, original.OutputOperation, v.Output, v.Evidence)
		if e == nil {
			ref = stored
		}
		if e != nil || schemaErr != nil {
			v.Result = "FAILURE"
		}
	}
	var out Record
	e := s.transaction(bounded, func(j *journal, tx authorization.ExecutionTransaction) error {
		if e := s.authorize(tx, "capability.reconcile"); e != nil {
			return e
		}
		r, ok := j.Records[original.Request.OperationID]
		if !ok || !s.owned(r) || !r.Started || !reflect.DeepEqual(r.Request, original.Request) {
			return failure(authorization.Denied)
		}
		if r.Effect != "UNKNOWN" {
			out = r
			return nil
		}
		if len(r.Reports) > 0 && r.Phase == v.Phase && r.Result == v.Result && r.Effect == v.Effect && r.Reference == ref {
			out = r
			return nil
		}
		if len(r.Reports) >= s.config.MaxReports || pending(j) >= s.config.MaxOutbox {
			return failure(authorization.Unavailable)
		}
		r.Revision++
		r.Phase = v.Phase
		r.Result = v.Result
		r.Effect = v.Effect
		r.Reference = ref
		report := tasks.ExecutionReport{OperationID: r.Request.OperationID, Qualification: r.Request.Qualification, Revision: r.Revision, Phase: r.Phase, Result: r.Result, Effect: r.Effect, Reference: ref}
		r.Reports = append(r.Reports, report)
		j.Records[r.Request.OperationID] = r
		if r.Effect != "UNKNOWN" && j.Active == r.Request.OperationID {
			j.Active = ""
		}
		out = r
		return nil
	})
	return out, e
}
func (s *Service) Reconcile(ctx context.Context, op string) (Record, error) {
	if s.cap.Async != nil {
		return s.reconcileAsync(ctx, op)
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return Record{}, failure(authorization.Unavailable)
	}
	release := true
	defer func() {
		if release {
			<-s.slots
		}
	}()
	var r Record
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		if e := s.authorize(tx, "capability.reconcile"); e != nil {
			return e
		}
		var ok bool
		r, ok = j.Records[op]
		if !ok || !s.owned(r) {
			return failure(authorization.Denied)
		}
		if !r.Started || r.Effect != "UNKNOWN" {
			return nil
		}
		if int(r.Checks) >= s.config.MaxChecks {
			return failure(authorization.Unavailable)
		}
		r.Checks++
		j.Records[op] = r
		return nil
	})
	if e != nil {
		return Record{}, e
	}
	if !r.Started || r.Effect != "UNKNOWN" {
		return r, nil
	}
	// The slot stays occupied until even a non-cooperative Inspector returns.
	bounded, cancel := context.WithTimeout(ctx, s.config.IOTimeout)
	type inspected struct {
		record Record
		err    error
	}
	result := make(chan inspected, 1)
	release = false
	go func() {
		defer func() { <-s.slots; cancel() }()
		v := unknown()
		defer func() {
			_ = recover()
			record, err := s.finish(context.WithoutCancel(ctx), r, v)
			result <- inspected{record, err}
		}()
		if observed, e := s.driver.Inspect(bounded, Call{Request: r.Request}); e == nil {
			v = observed
		}
	}()
	select {
	case out := <-result:
		return out.record, out.err
	case <-bounded.Done():
		return s.finish(context.WithoutCancel(ctx), r, unknown())
	}

}

// Drain atomically acknowledges only after Core has applied the original report.
// Lost acknowledgements cause replay of that report, never another Start.
func (s *Service) Drain(ctx context.Context, limit int) error {
	if limit < 1 || limit > 64 {
		return failure(authorization.Invalid)
	}
	reports := []tasks.ExecutionReport{}
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		reports = nil
		if e := s.authorize(tx, "capability.reconcile"); e != nil {
			return e
		}
		for _, r := range j.Records {
			if !s.owned(r) {
				continue
			}
			// Deliver the newest cumulative conclusion first, so a known conflict
			// cannot be overtaken by an older success in the same outbox.
			for i := len(r.Reports) - 1; i >= 0; i-- {
				v := r.Reports[i]
				if v.Revision > r.Applied && len(reports) < limit {
					reports = append(reports, v)
				}
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	for _, report := range reports {
		bounded, cancel := context.WithTimeout(ctx, s.config.IOTimeout)
		e = s.core.ConsumeExecution(bounded, report)
		cancel()
		if e != nil {
			return e
		}
		e = s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
			if e := s.authorize(tx, "capability.reconcile"); e != nil {
				return e
			}
			r, ok := j.Records[report.OperationID]
			if !ok || !s.owned(r) {
				return failure(authorization.Denied)
			}
			r.Applied = max(r.Applied, report.Revision)
			j.Records[report.OperationID] = r
			return nil
		})
		if e != nil {
			return e
		}
	}
	return nil
}
