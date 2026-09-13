package execution

import (
	"context"
	"lerna/authorization"
	"lerna/tasks"
)

type CancelRequest struct{ OperationID, Invocation string }
type CancelRecord struct {
	Request  CancelRequest
	Subject  string
	Receipt  Receipt
	Progress string
	Sent     bool
}
type CancelDriver interface {
	Cancel(context.Context, Call, Association, string) error
}

func (s *Service) RequestCancel(ctx context.Context, in CancelRequest) (Receipt, error) {
	if s.cap.Async == nil {
		return Receipt{}, failure(authorization.Unsupported)
	}
	if in.OperationID == "" || in.Invocation == "" || len(in.OperationID) > 512 || len(in.Invocation) > 512 {
		return Receipt{}, failure(authorization.Invalid)
	}
	var out Receipt
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		if e := s.authorize(tx, "capability.cancel"); e != nil {
			return e
		}
		if old, ok := j.Cancels[in.OperationID]; ok {
			if old.Subject != s.binding.Subject {
				return failure(authorization.Denied)
			}
			if old.Request != in {
				return failure(authorization.IdentityConflict)
			}
			if e := s.authorize(tx, "capability.read"); e != nil {
				return e
			}
			out = old.Receipt
			return nil
		}
		if _, ok := j.Shared.Operations[in.OperationID]; ok {
			return failure(authorization.IdentityConflict)
		}
		if _, ok := j.Shared.Invocations[in.OperationID]; ok {
			return failure(authorization.IdentityConflict)
		}
		if owner, ok := j.Shared.CancelOwners[in.OperationID]; ok && owner != s.cap.Digest() {
			return failure(authorization.IdentityConflict)
		}
		if _, ok := j.Records[in.OperationID]; ok {
			return failure(authorization.IdentityConflict)
		}
		if reserved, ok := j.ReservedCancels[in.OperationID]; ok && reserved != in.Invocation {
			return failure(authorization.IdentityConflict)
		}
		r, ok := j.Records[in.Invocation]
		if !ok || !s.owned(r) {
			return failure(authorization.Denied)
		}
		if len(j.Cancels) >= s.config.MaxOperations {
			return failure(authorization.Unavailable)
		}
		if r.CancelID != "" {
			return failure(authorization.Conflict)
		}
		if e := tx.ExecutionControlOperation(in.OperationID, s.binding.Subject); e != nil {
			return e
		}
		out = Receipt{in.OperationID, 1}
		j.Cancels[in.OperationID] = CancelRecord{Request: in, Subject: s.binding.Subject, Receipt: out, Progress: "ACCEPTED"}
		r.CancelID = in.OperationID
		j.Records[in.Invocation] = r
		return nil
	})
	return out, e
}
func (s *Service) GetCancel(ctx context.Context, op string) (CancelRecord, error) {
	var out CancelRecord
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		if e := s.authorize(tx, "capability.read"); e != nil {
			return e
		}
		var ok bool
		out, ok = j.Cancels[op]
		if !ok || out.Subject != s.binding.Subject {
			return failure(authorization.Denied)
		}
		r := j.Records[out.Request.Invocation]
		if !s.owned(r) {
			return failure(authorization.Denied)
		}
		return nil
	})
	return out, e
}
func (s *Service) RunCancel(ctx context.Context, op string) (CancelRecord, error) {
	if s.cap.Async == nil {
		return CancelRecord{}, failure(authorization.Unsupported)
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return CancelRecord{}, failure(authorization.Unavailable)
	}
	release := true
	defer func() {
		if release {
			<-s.slots
		}
	}()
	var c CancelRecord
	var r Record
	send := false
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		send = false
		if e := s.authorize(tx, "capability.cancel"); e != nil {
			return e
		}
		var ok bool
		c, ok = j.Cancels[op]
		if !ok || c.Subject != s.binding.Subject {
			return failure(authorization.Denied)
		}
		r = j.Records[c.Request.Invocation]
		if !s.owned(r) {
			return failure(authorization.Denied)
		}
		if c.Sent || c.Progress != "ACCEPTED" {
			return nil
		}
		if !r.Started {
			if len(r.Reports) >= s.config.MaxReports || pending(j) >= s.config.MaxOutbox {
				return failure(authorization.Unavailable)
			}
			c.Progress = "STOPPED"
			r.Phase = "NOT_STARTED"
			r.Result = "FAILURE"
			r.Effect = "NOT_OCCURRED"
			r.Revision++
			r.Reports = append(r.Reports, tasks.ExecutionReport{OperationID: r.Request.OperationID, Qualification: r.Request.Qualification, Revision: r.Revision, Phase: r.Phase, Result: r.Result, Effect: r.Effect, PreStart: true})
			j.Records[r.Request.OperationID] = r
			j.Cancels[op] = c
			return nil
		}
		if r.Effect != "UNKNOWN" {
			if r.Effect == "CONFIRMED" {
				c.Progress = "NOT_PREVENTED"
			} else {
				c.Progress = "STOPPED"
			}
			j.Cancels[op] = c
			return nil
		}
		if !s.cap.Async.Cancel {
			c.Progress = "UNSUPPORTED"
			j.Cancels[op] = c
			return nil
		}
		if tx.Now().UnixNano() >= r.Async.Until {
			return failure(authorization.Unavailable)
		}
		if r.Async.LeaseUntil > tx.Now().UnixNano() {
			return failure(authorization.Conflict)
		}
		c.Sent = true
		c.Progress = "UNKNOWN"
		j.Cancels[op] = c
		send = true
		return nil
	})
	if e != nil || !send {
		return c, e
	}
	bounded, cancel := context.WithTimeout(ctx, s.config.IOTimeout)
	done := make(chan CancelRecord, 1)
	release = false
	initial := c
	go func() {
		c := initial
		defer func() {
			_ = recover()
			// A completed response must also release its execution slot. Recover
			// immediately reconciles after cancellation; publishing first could
			// spuriously reject that next step as still busy.
			<-s.slots
			done <- c
			cancel()
		}()
		err := s.driver.(CancelDriver).Cancel(bounded, Call{Request: r.Request}, r.Async.Association, op)
		if err == nil {
			save, stop := context.WithTimeout(context.WithoutCancel(ctx), s.config.IOTimeout)
			defer stop()
			_ = s.transaction(save, func(j *journal, tx authorization.ExecutionTransaction) error {
				if e := s.authorize(tx, "capability.reconcile"); e != nil {
					return e
				}
				v := j.Cancels[op]
				if v.Progress == "UNKNOWN" {
					v.Progress = "REQUESTED"
					j.Cancels[op] = v
				}
				c = v
				return nil
			})
		}
	}()
	select {
	case v := <-done:
		return v, nil
	case <-bounded.Done():
		select {
		case v := <-done:
			return v, nil
		default:
			return c, nil
		}
	}
}
