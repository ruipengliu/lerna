package extraction

import (
	"context"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
)

// TriggerRound binds an authoritative source revision event to the exact
// original Core submission before Submit. The source revision is the event
// identity; retransmission identifiers never allocate another round.
type TriggerRound struct {
	Event      *wire.ContentSource
	Submission tasks.Submission
}
type TriggerRounds interface {
	ReserveTriggerRound(context.Context, string, string, string, int64, TriggerRound) error
	GetTriggerRound(context.Context, string, string, string, *wire.ContentSource) (TriggerRound, error)
}
