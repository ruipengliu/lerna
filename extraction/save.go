package extraction

import (
	"context"
	wire "lerna/gen/harness/v1"
)

// SaveIntent is a durable association, not proof of a Memory commit. Pending
// carries the exact original write; retired retains only its operation and ref
// for reconciliation/cleanup, with no Spec that could be resubmitted.
type SaveIntent struct {
	State   string
	Request *wire.MemoryWrite
}

// SaveJournal is trusted host persistence. Reserve before calling Memory.Put;
// on any uncertain result, look up this candidate's original request. A missing
// Memory reply never authorizes allocating a replacement operation. Callers
// separately enforce current save authorization and source constraints.
type SaveJournal interface {
	ReserveSave(context.Context, string, string, string, *wire.MemoryWrite) error
	LookupSave(context.Context, string, string, string) (SaveIntent, error)
}
