package memory

import (
	"context"
	"encoding/hex"
	"time"
)

type CleanupDimension string

const (
	LocalCleanup   CleanupDimension = "local"
	ReplicaCleanup CleanupDimension = "replicas"
	DerivedCleanup CleanupDimension = "derived_archives"
)

// CleanupTarget is trusted host inventory for one collection. A nil binding
// explicitly denotes an unconnected target. It is not supplied by a peer.
// Binding config must cover the sink's complete effective scope; an applied
// cursor proves only that scope, never undeclared archives or independent copies.
type CleanupTarget struct {
	Name      string
	Dimension CleanupDimension
	Binding   *ConsumerBinding
}
type CleanupProgress struct {
	Name, State string
	Position    uint64
}
type DeletionStatus struct {
	Event                    SourceEvent
	Authority                string
	Local, Replicas, Derived []CleanupProgress
}

type DeletionReporter struct {
	events   SourceEvents
	progress ConsumerInspection
	targets  []CleanupTarget
}

func NewDeletionReporter(events SourceEvents, progress ConsumerInspection, targets []CleanupTarget) (*DeletionReporter, error) {
	if events == nil || progress == nil || len(targets) > 32 {
		return nil, Invalid
	}
	out := &DeletionReporter{events: events, progress: progress}
	names := map[string]bool{}
	bindings := map[[3]string]bool{}
	for _, t := range targets {
		if !text(t.Name, 256) || names[t.Name] {
			return nil, Invalid
		}
		names[t.Name] = true
		switch t.Dimension {
		case LocalCleanup, ReplicaCleanup, DerivedCleanup:
		default:
			return nil, Invalid
		}
		if t.Binding != nil {
			b := *t.Binding
			hash, err := hex.DecodeString(b.ConfigSHA256)
			key := [3]string{b.Namespace, b.Collection, b.Consumer}
			if !text(b.Namespace, 256) || !text(b.Collection, 256) || !text(b.Consumer, 256) || err != nil || len(hash) != 32 || hex.EncodeToString(hash) != b.ConfigSHA256 || bindings[key] {
				return nil, Invalid
			}
			bindings[key] = true
			t.Binding = &b
		}
		out.targets = append(out.targets, t)
	}
	return out, nil
}

// Status is a bounded internal evidence read, not a disclosure API. A user-
// facing adapter must authorize the record and operation before calling it.
// Independent monotonic cursors may be observed at different instants: a lagging
// observation remains pending, and no global atomic completion is asserted.
func (r *DeletionReporter) Status(ctx context.Context, event SourceEvent) (DeletionStatus, error) {
	if !ValidSourceEvent(event) || event.Kind != SourceDeleted {
		return DeletionStatus{}, Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	events, err := r.events.ReadEvents(ctx, event.Ref.Namespace, event.Ref.Collection, event.Position-1, 1)
	if err != nil {
		return DeletionStatus{}, err
	}
	if len(events) != 1 || events[0] != event {
		return DeletionStatus{}, Conflict
	}
	out := DeletionStatus{Event: event, Authority: "committed"}
	for _, t := range r.targets {
		p := CleanupProgress{Name: t.Name, State: "not_covered"}
		if t.Binding != nil {
			b := *t.Binding
			if b.Namespace != event.Ref.Namespace || b.Collection != event.Ref.Collection {
				return DeletionStatus{}, Invalid
			}
			p.State = "pending"
			position, e := r.progress.InspectConsumer(ctx, b)
			if e != nil && e != Missing {
				return DeletionStatus{}, e
			}
			if e == nil {
				p.Position = position
				if position >= event.Position {
					p.State = "applied"
				}
			}
		}
		switch t.Dimension {
		case LocalCleanup:
			out.Local = append(out.Local, p)
		case ReplicaCleanup:
			out.Replicas = append(out.Replicas, p)
		case DerivedCleanup:
			out.Derived = append(out.Derived, p)
		}
	}
	uncovered := func(items []CleanupProgress) []CleanupProgress {
		if len(items) == 0 {
			return []CleanupProgress{{Name: "unregistered", State: "not_covered"}}
		}
		return items
	}
	out.Local = uncovered(out.Local)
	out.Replicas = uncovered(out.Replicas)
	out.Derived = uncovered(out.Derived)
	return out, nil
}
