package tasks

import (
	"context"
	"lerna/authorization"
	"time"
)

// Quarantine is the mandatory host entry point for restored backups, rolled-back
// storage or potentially cloned identities. An ordinary reopen assumes exclusive,
// non-rolled-back storage; it cannot detect a dishonest host from its own bytes.
func (s *Service) Quarantine() *Service { copy := *s; copy.quarantined = true; return &copy }

// VerifyRecovery requires external exclusive-ownership evidence for every task.
// It neither treats the local epoch nor a user retry as that evidence. The host
// must keep the store isolated during this check and use only the returned core.
func (s *Service) VerifyRecovery(ctx context.Context, token string, verify func(context.Context, RunSnapshot) error) (*Service, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !s.quarantined || verify == nil {
		return nil, failure(authorization.Denied)
	}
	var runs []RunSnapshot
	e := s.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		for _, r := range j.Runs {
			id, e := tx.Authorize(token, r.Task.Resource, "task.handoff")
			if e != nil {
				return e
			}
			if id.Subject != r.Task.Subject {
				return failure(authorization.Denied)
			}
			if handoffFrozen(j, r.Task.Ref.TaskID) {
				return failure(authorization.Unavailable)
			}
			runs = append(runs, r)
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	for _, r := range runs {
		if e = verify(ctx, r); e != nil {
			return nil, e
		}
	}
	copy := *s
	copy.quarantined = false
	return &copy, nil
}
