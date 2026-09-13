package fetchcheck

import (
	"context"
	"lerna/internal/randomid"
	"lerna/tasks"
	"time"
)

// Keep the original answer worker qualified while a bounded model call is in
// flight. Renew checks the original owner/epoch/work generation and current
// authorization; it cannot extend the task deadline or reopen completed work.
func keepResearchAnswerLease(parent context.Context, port *tasks.GenerationPort, q tasks.Qualification) func() {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(limits().RenewEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			id, err := randomid.New()
			if err != nil {
				return
			}
			call, stop := context.WithTimeout(ctx, limits().IOTimeout)
			_, err = port.Commit(call, tasks.WorkChange{Kind: "renew", ChangeID: id, Qualification: q})
			stop()
			if err != nil {
				return
			}
		}
	}()
	return func() { cancel(); <-done }
}
