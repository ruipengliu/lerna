package contextassembly

import "context"

// CheckpointStore retains a snapshot's original integrity comparison inside the
// same cleanup authority as its body. These are trusted host ports, not grants
// or disclosure APIs. Binding never creates a snapshot or replaces its identity.
// VerifyCheckpoint returns Invalidated only when retirement is durable and the
// comparison has been erased; an unbound live snapshot returns Missing.
// Hosts can persist the Key instead of retaining a digest in another file.
type CheckpointStore interface {
	BindCheckpoint(context.Context, Key, [32]byte) error
	VerifyCheckpoint(context.Context, Key) error
}
