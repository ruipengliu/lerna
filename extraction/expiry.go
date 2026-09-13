package extraction

import (
	"context"
	"lerna/memory"
	"time"
)

// CandidateExpiry is trusted host cleanup, not a public mutation API. The
// cutoff must come from the host clock. Retirement removes candidate/save
// intent bodies while preserving original operation facts and cleanup IDs.
type CandidateExpiry interface {
	RetireExpired(context.Context, string, string, int64, int) (int, error)
}

type ExpiryWorker struct {
	store              CandidateExpiry
	clock              memory.Clock
	namespace, subject string
	limit              int
}

func NewExpiryWorker(store CandidateExpiry, clock memory.Clock, namespace, subject string, limit int) (*ExpiryWorker, error) {
	if store == nil || clock == nil || namespace == "" || len(namespace) > 256 || subject == "" || len(subject) > 256 || limit < 1 || limit > 16 {
		return nil, memory.Invalid
	}
	return &ExpiryWorker{store, clock, namespace, subject, limit}, nil
}

// Run is one host-scheduled bounded pass. Repeated passes select the oldest
// still-retained expired candidates; they require no resettable cursor. This
// count attests only to candidate/save-intent retirement. Memory and external
// consumers still require their own cleanup attempts and acknowledgments.
func (w *ExpiryWorker) Run(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	now, err := w.clock.Now()
	if err != nil {
		return 0, memory.Unavailable
	}
	return w.store.RetireExpired(ctx, w.namespace, w.subject, now.Unix(), w.limit)
}
