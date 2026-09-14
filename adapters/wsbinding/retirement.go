package wsbinding

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
)

// RecoveryRecord retains only the operation association after replay expires.
// Snapshot is a public InvocationSnapshot, not a new invocation authorization.
type RecoveryRecord struct {
	MessageID, Namespace, OperationID, State string
	Incoming                                 bool
	Attempts                                 int
	Snapshot                                 []byte
}

func archive(l *deliveryLog, records map[uint64]retained, incoming bool) error {
	for _, r := range records {
		m, e := decodeReliable(r)
		if e != nil {
			return e
		}
		request := m.Request
		local := incoming
		if request == nil && len(r.Guard) > 0 {
			request = new(wire.CapabilityRequest)
			if e = proto.Unmarshal(r.Guard, request); e != nil {
				return e
			}
			local = true
		}
		if request == nil {
			continue
		}
		op := request.GetInvoke().GetInvocation().GetOperationId()
		if op == "" {
			continue
		}
		l.Recovery = append(l.Recovery, RecoveryRecord{MessageID: m.MessageId, Namespace: request.Namespace, OperationID: op, State: "UNKNOWN", Incoming: local})
	}
	return nil
}

// CloseSending persists the old stream's boundary before removing payloads.
// Remaining obligations become operation queries; none become new invocations.
func (j *Journal) CloseSending(ctx context.Context) (uint64, error) {
	var generation uint64
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		if l.Generation >= 1<<53 {
			return failure(authorization.Unavailable)
		}
		if e := archive(l, l.Out, false); e != nil {
			return e
		}
		l.Out = map[uint64]retained{}
		l.Next = 0
		l.Ack = 0
		l.Generation++
		generation = l.Generation
		return nil
	})
	return generation, e
}
func (j *Journal) CloseReceiving(ctx context.Context, generation uint64) error {
	return j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		if generation == l.ReceivedGeneration {
			return nil
		}
		if generation != l.ReceivedGeneration+1 {
			return failure(authorization.Invalid)
		}
		if e := archive(l, l.In, true); e != nil {
			return e
		}
		l.In = map[uint64]retained{}
		l.Prefix = 0
		l.ReceivedGeneration = generation
		return nil
	})
}
func (j *Journal) Recovery(ctx context.Context) ([]RecoveryRecord, error) {
	var out []RecoveryRecord
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		out = append([]RecoveryRecord(nil), l.Recovery...)
		return nil
	})
	return out, e
}
func (j *Journal) beginRecovery(ctx context.Context, id string, incoming bool) error {
	return j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		for i, r := range l.Recovery {
			if r.MessageID == id && r.Incoming == incoming {
				if r.Attempts >= j.config.Attempts {
					return failure(authorization.Unavailable)
				}
				l.Recovery[i].Attempts++
				return nil
			}
		}
		return failure(authorization.NotFound)
	})
}
func (j *Journal) saveRecovery(ctx context.Context, r RecoveryRecord, snapshot *wire.InvocationSnapshot) error {
	if snapshot == nil || snapshot.GetInvocation().GetOperationId() != r.OperationID {
		return failure(authorization.Invalid)
	}
	raw, e := proto.Marshal(snapshot)
	if e != nil {
		return e
	}
	return j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		for i, v := range l.Recovery {
			if v.MessageID == r.MessageID && v.Incoming == r.Incoming {
				l.Recovery[i].Snapshot = raw
				l.Recovery[i].State = "KNOWN"
				return nil
			}
		}
		return failure(authorization.NotFound)
	})
}
func (j *Journal) expired(ctx context.Context) (bool, error) {
	expired := false
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		expired = false
		for _, r := range l.Out {
			if tx.Now().UnixNano() >= r.Until {
				expired = true
			}
		}
		return nil
	})
	return expired, e
}

// Clean removes reconciled terminal snapshots. Durable generation watermarks
// remain, and business operation/window retention stays with its authority.
func (j *Journal) Clean(ctx context.Context) error {
	return j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		kept := l.Recovery[:0]
		for _, r := range l.Recovery {
			snapshot := new(wire.InvocationSnapshot)
			terminal := r.State == "KNOWN" && proto.Unmarshal(r.Snapshot, snapshot) == nil && snapshot.Phase == "FINISHED" && snapshot.Effect != "UNKNOWN"
			if !terminal {
				kept = append(kept, r)
			}
		}
		l.Recovery = kept
		return nil
	})
}
