package memory

import "context"

type SourceEventKind string

const (
	// SourceErased affects exactly Revision, never an earlier/later revision.
	SourceErased    SourceEventKind = "erased"
	SourceCreated   SourceEventKind = "created"
	SourceCorrected SourceEventKind = "corrected"
	SourceDeleted   SourceEventKind = "deleted"
)

// SourceEvent is internal authoritative change metadata. It deliberately carries
// no body, payload digest or disclosure credential. Position is collection-local.
type SourceEvent struct {
	Ref                Ref
	Kind               SourceEventKind
	Revision, Position uint64
}

// SourceEvents feeds trusted invalidation consumers. Implementations return
// ordered committed events even after historical bodies have been removed.
// It is not a user disclosure or synchronization authorization interface.
type SourceEvents interface {
	ReadEvents(context.Context, string, string, uint64, int) ([]SourceEvent, error)
}

// ConsumerBinding pins the trusted consumer's effective configuration before it
// performs effects. A new configuration cannot silently reuse an old cursor.
type ConsumerBinding struct {
	Namespace, Collection, Consumer, ConfigSHA256 string
}

// ConsumerProgress stores applied positions, not receipt or delivery positions.
// AckEvent advances exactly one committed event after consumer confirmation.
type ConsumerProgress interface {
	BindConsumer(context.Context, ConsumerBinding) (uint64, error)
	AckEvent(context.Context, ConsumerBinding, uint64, uint64) error
}

// ConsumerInspection reads existing confirmed progress without registering work.
// Missing means no durable binding exists, not that cleanup has completed.
type ConsumerInspection interface {
	InspectConsumer(context.Context, ConsumerBinding) (uint64, error)
}
