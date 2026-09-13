package execution

import (
	"context"
	"lerna/authorization"
	"lerna/tasks"
	"sort"
)

type taskCancellation interface {
	ExecutionCancelled(context.Context, tasks.Qualification) (bool, error)
}

// Recover performs a bounded pass; callers schedule another pass explicitly.
// Persistent due dates and attempt counts survive process replacement.
func (s *Service) Recover(ctx context.Context, limit int) error {
	if s.cap.Async == nil {
		return failure(authorization.Unsupported)
	}
	if limit < 1 || limit > 16 {
		return failure(authorization.Invalid)
	}
	var records []Record
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		if e := s.authorize(tx, "capability.reconcile"); e != nil {
			return e
		}
		records = nil
		keys := []string{}
		for op, r := range j.Records {
			if s.owned(r) && (r.Effect == "UNKNOWN" || r.Applied < r.Revision) {
				keys = append(keys, op)
			}
		}
		sort.Strings(keys)
		start := sort.SearchStrings(keys, j.RecoveryAfter)
		for start < len(keys) && keys[start] <= j.RecoveryAfter {
			start++
		}
		for i := 0; i < min(limit, len(keys)); i++ {
			op := keys[(start+i)%len(keys)]
			records = append(records, j.Records[op])
			j.RecoveryAfter = op
		}
		return nil
	})
	if e != nil {
		return e
	}
	for _, r := range records {
		bounded, stop := context.WithTimeout(ctx, s.config.IOTimeout)
		if c, ok := s.core.(taskCancellation); ok && r.CancelID == "" {
			cancel, err := c.ExecutionCancelled(bounded, r.Request.Qualification)
			if err == nil && cancel {
				_, err = s.RequestCancel(bounded, CancelRequest{r.TaskCancelOperation, r.Request.OperationID})
				if err == nil {
					r.CancelID = r.TaskCancelOperation
				}
			}
			if err != nil {
				stop()
				return err
			}
		}
		stop()
		if r.CancelID != "" {
			if _, e = s.RunCancel(ctx, r.CancelID); e != nil && !authorization.Is(e, authorization.Conflict) {
				return e
			}
		}
		if r.Started && r.Effect == "UNKNOWN" {
			_, e = s.Reconcile(ctx, r.Request.OperationID)
			if e != nil && !authorization.Is(e, authorization.Conflict) && !authorization.Is(e, authorization.Unavailable) {
				return e
			}
		}
	}
	return s.Drain(ctx, limit)
}

// ProgressPort is constructed by an authenticated local host for its driver.
// The source is a host binding; an incoming Fact cannot select its authority.
type ProgressPort struct {
	s      *Service
	source string
}

func (s *Service) BindProgress(source string) (*ProgressPort, error) {
	if s.cap.Async == nil || source != s.cap.Async.Source {
		return nil, failure(authorization.Denied)
	}
	return &ProgressPort{s, source}, nil
}
func (p *ProgressPort) Accept(ctx context.Context, op string, f Fact) error {
	select {
	case p.s.slots <- struct{}{}:
		defer func() { <-p.s.slots }()
	default:
		return failure(authorization.Unavailable)
	}
	if f.Source != p.source {
		return failure(authorization.Denied)
	}
	var r Record
	e := p.s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		if e := p.s.authorize(tx, "capability.reconcile"); e != nil {
			return e
		}
		var ok bool
		r, ok = j.Records[op]
		if !ok || !p.s.owned(r) {
			return failure(authorization.Denied)
		}
		return nil
	})
	if e != nil {
		return e
	}
	bounded, stop := context.WithTimeout(ctx, p.s.config.IOTimeout)
	defer stop()
	_, e = p.s.asyncFact(bounded, r, f)
	return e
}
