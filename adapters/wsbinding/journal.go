package wsbinding

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/taskwire"
	"sort"
	"time"
)

// JournalConfig bounds persisted payloads and work across connection replacement.
type JournalConfig struct {
	Records, Bytes, Reorder, Attempts int
	Retention                         time.Duration
}

type retained struct {
	DispositionAttempts    int
	Guard                  []byte
	Fingerprint, MessageID string
	ReplyID                string
	Raw                    []byte
	Until                  int64
	Consumed               bool
	Attempts               int
}
type deliveryLog struct {
	Recovery                                          []RecoveryRecord
	Config                                            JournalConfig
	Generation, ReceivedGeneration, Next, Prefix, Ack uint64
	Session                                           uint64
	Out, In                                           map[uint64]retained
}

// Journal uses the authority's durable transaction, including the task partition.
// The trusted host binds a stable peer scope; connection nonces are not scopes.
type Journal struct {
	authority *authorization.Service
	peer      string
	config    JournalConfig
	session   uint64
}

func NewJournal(a *authorization.Service, peer string, c JournalConfig) (*Journal, error) {
	if a == nil || len(peer) == 0 || len(peer) > 1024 || c.Records < 1 || c.Records > 256 || c.Bytes < 4096 || c.Bytes > 1<<20 || c.Reorder < 1 || c.Reorder > 32 || c.Attempts < 1 || c.Attempts > 3 || c.Retention < time.Second || c.Retention > 24*time.Hour {
		return nil, failure(authorization.Invalid)
	}
	return &Journal{authority: a, peer: peer, config: c}, nil
}
func (j *Journal) update(ctx context.Context, fn func(*deliveryLog, authorization.DeliveryTransaction) error) error {
	return j.authority.UpdateDelivery(ctx, func(tx authorization.DeliveryTransaction) error {
		logs := map[string]*deliveryLog{}
		if raw := tx.DeliveryData(); len(raw) > 0 {
			if e := json.Unmarshal(raw, &logs); e != nil {
				return failure(authorization.Invalid)
			}
		}
		l := logs[j.peer]
		if l == nil {
			if len(logs) >= 16 {
				return failure(authorization.Unavailable)
			}
			l = &deliveryLog{Config: j.config, Generation: 1, ReceivedGeneration: 1, Out: map[uint64]retained{}, In: map[uint64]retained{}}
			logs[j.peer] = l
		}
		if j.session != 0 && l.Session != j.session {
			return failure(authorization.Conflict)
		}
		if l.Config != j.config {
			return failure(authorization.Conflict)
		}
		if e := fn(l, tx); e != nil {
			return e
		}
		size := 0
		for _, r := range l.Out {
			size += len(r.Raw) + len(r.Guard)
		}
		for _, r := range l.In {
			size += len(r.Raw) + len(r.Guard)
		}
		for _, r := range l.Recovery {
			size += len(r.Snapshot) + len(r.OperationID) + len(r.MessageID)
		}
		records := len(l.Out) + len(l.In) + len(l.Recovery)
		// Omission markers are bounded control metadata for an existing recovery
		// record, not a second business record requiring another quota slot.
		for _, r := range l.Out {
			m, e := decodeReliable(r)
			if e != nil {
				return e
			}
			if len(m.OmittedSha256) > 0 {
				records--
			}
		}
		if records > j.config.Records || size > j.config.Bytes {
			return failure(authorization.Unavailable)
		}
		raw, e := json.Marshal(logs)
		if e != nil {
			return e
		}
		tx.SetDeliveryData(raw)
		return nil
	})
}
func reliableBytes(m *wire.WSReliable) ([]byte, error) {
	if m == nil || m.Generation == 0 || m.Position == 0 || m.Position > 1<<53 || m.Generation > 1<<53 || m.MessageId == "" || len(m.MessageId) > 128 || !taskwire.Known(m.ProtoReflect()) || proto.Size(m) > 1<<20 {
		return nil, failure(authorization.Invalid)
	}
	if len(m.OmittedSha256) > 0 {
		if len(m.OmittedSha256) != 32 || m.Request != nil || m.Response != nil {
			return nil, failure(authorization.Invalid)
		}
		return proto.MarshalOptions{Deterministic: true}.Marshal(m)
	}
	if (m.Request == nil) == (m.Response == nil) {
		return nil, failure(authorization.Invalid)
	}
	if m.Request != nil && m.Request.MessageId != m.MessageId || m.Response != nil && m.Response.MessageId != m.MessageId {
		return nil, failure(authorization.Invalid)
	}
	namespace := ""
	if m.Request != nil {
		namespace = m.Request.Namespace
	} else {
		namespace = m.Response.Namespace
		if m.Response.ReplyTo == "" || len(m.Response.ReplyTo) > 128 {
			return nil, failure(authorization.Invalid)
		}
	}
	if namespace == "" || len(namespace) > 128 {
		return nil, failure(authorization.Invalid)
	}
	return proto.MarshalOptions{Deterministic: true}.Marshal(m)
}
func decodeReliable(r retained) (*wire.WSReliable, error) {
	m := new(wire.WSReliable)
	if e := proto.Unmarshal(r.Raw, m); e != nil {
		return nil, e
	}
	return m, nil
}

// Receive returns only the durable contiguous prefix, never a receipt for a gap.
func (j *Journal) Receive(ctx context.Context, m *wire.WSReliable) (uint64, error) {
	var prefix uint64
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		var e error
		prefix, e = receiveLog(l, tx, m)
		return e
	})
	return prefix, e
}
func receiveLog(l *deliveryLog, tx authorization.DeliveryTransaction, m *wire.WSReliable) (uint64, error) {
	raw, e := reliableBytes(m)
	if e != nil {
		return 0, e
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(raw))
	omitted := len(m.OmittedSha256) > 0
	if omitted {
		fingerprint = fmt.Sprintf("%x", m.OmittedSha256)
	}
	if m.Generation != l.ReceivedGeneration {
		return 0, failure(authorization.Expired)
	}
	if old, ok := l.In[m.Position]; ok {
		if old.Fingerprint != fingerprint || old.MessageID != m.MessageId {
			return 0, failure(authorization.IdentityConflict)
		}
		return l.Prefix, nil
	}
	if m.Position <= l.Prefix {
		return 0, failure(authorization.Expired)
	}
	if m.Position > l.Prefix+uint64(l.Config.Reorder) {
		return 0, failure(authorization.Unavailable)
	}
	for _, r := range l.In {
		if r.MessageID == m.MessageId {
			return 0, failure(authorization.IdentityConflict)
		}
	}
	l.In[m.Position] = retained{Raw: raw, Fingerprint: fingerprint, MessageID: m.MessageId, Consumed: omitted, Until: tx.Now().Add(l.Config.Retention).UnixNano()}
	for {
		if _, ok := l.In[l.Prefix+1]; !ok {
			break
		}
		l.Prefix++
	}
	return l.Prefix, nil
}

// Unconsumed is a bounded recovery scan. Out-of-order records wait for their gap.
func (j *Journal) Unconsumed(ctx context.Context) ([]*wire.WSReliable, error) {
	var out []*wire.WSReliable
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		out = nil
		for seq, r := range l.In {
			if seq <= l.Prefix && !r.Consumed {
				m, e := decodeReliable(r)
				if e != nil {
					return e
				}
				out = append(out, m)
			}
		}
		sort.Slice(out, func(a, b int) bool { return out[a].Position < out[b].Position })
		return nil
	})
	return out, e
}

func appendReliable(l *deliveryLog, tx authorization.DeliveryTransaction, m *wire.WSReliable) (*wire.WSReliable, error) {
	for _, r := range l.Recovery {
		if r.MessageID == m.MessageId && !r.Incoming {
			return nil, failure(authorization.Expired)
		}
	}
	for _, r := range l.Out {
		old, e := decodeReliable(r)
		if e != nil {
			return nil, e
		}
		if old.MessageId == m.MessageId {
			if !proto.Equal(old.Request, m.Request) || !proto.Equal(old.Response, m.Response) {
				return nil, failure(authorization.IdentityConflict)
			}
			return old, nil
		}
	}
	m.Generation = l.Generation
	m.Position = l.Next + 1
	raw, e := reliableBytes(m)
	if e != nil {
		return nil, e
	}
	l.Next = m.Position
	l.Out[m.Position] = retained{Raw: raw, Until: tx.Now().Add(l.Config.Retention).UnixNano()}
	return m, nil
}

// Prepare persists the actual invocation intent as the original outgoing message.
func (j *Journal) Prepare(ctx context.Context, r *wire.CapabilityRequest) (*wire.WSReliable, error) {
	if r == nil {
		return nil, failure(authorization.Invalid)
	}
	var out *wire.WSReliable
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		var e error
		out, e = appendReliable(l, tx, &wire.WSReliable{MessageId: r.MessageId, Request: proto.Clone(r).(*wire.CapabilityRequest)})
		return e
	})
	return out, e
}

// Complete commits a local runtime change, consumption, and reply together.
// For an independent store, call only after original-operation acceptance is
// established. The callback is local and retryable, never external I/O.
func (j *Journal) Complete(ctx context.Context, m *wire.WSReliable, response *wire.CapabilityResponse, apply func(authorization.RuntimeTransaction) error) error {
	raw, e := reliableBytes(m)
	if e != nil {
		return e
	}
	return j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		r, ok := l.In[m.Position]
		if !ok || m.Generation != l.ReceivedGeneration || r.Fingerprint != fmt.Sprintf("%x", sha256.Sum256(raw)) {
			return failure(authorization.IdentityConflict)
		}
		if r.Consumed {
			return nil
		}
		if m.Position > l.Prefix {
			return failure(authorization.Conflict)
		}
		if apply != nil {
			if e := apply(tx); e != nil {
				return e
			}
		}
		if response != nil {
			if m.Request == nil || response.ReplyTo != m.MessageId || response.Namespace != m.Request.Namespace {
				return failure(authorization.Invalid)
			}
			reply, e := appendReliable(l, tx, &wire.WSReliable{MessageId: response.MessageId, Response: proto.Clone(response).(*wire.CapabilityResponse)})
			if e != nil {
				return e
			}
			saved := l.Out[reply.Position]
			if response.GetFailure() == nil {
				guard := proto.Clone(m.Request).(*wire.CapabilityRequest)
				if guard.GetInvoke() != nil {
					guard.GetInvoke().GrantMaterial = ""
				}
				saved.Guard, e = proto.Marshal(guard)
				if e != nil {
					return e
				}
			}
			l.Out[reply.Position] = saved
			r.ReplyID = response.MessageId
		}
		r.Consumed = true
		if m.Request != nil {
			r.Raw = nil
		}
		l.In[m.Position] = r
		return nil
	})
}

// Replay enumerates retained messages. Attempt charges each actual send before
// network I/O, so an earlier refusal cannot consume later messages' budgets.
func (j *Journal) Replay(ctx context.Context, generation, after uint64) ([]*wire.WSReliable, error) {
	var out []*wire.WSReliable
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		out = nil
		if generation != l.Generation {
			return failure(authorization.Expired)
		}
		if after > l.Next {
			return failure(authorization.Invalid)
		}
		for seq := after + 1; seq <= l.Next; seq++ {
			r, ok := l.Out[seq]
			if !ok || tx.Now().UnixNano() >= r.Until {
				return failure(authorization.Expired)
			}

			m, e := decodeReliable(r)
			if e != nil {
				return e
			}
			out = append(out, m)

		}
		return nil
	})
	return out, e
}
func (j *Journal) Acknowledge(ctx context.Context, generation, prefix uint64) error {
	return j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		if generation != l.Generation {
			return failure(authorization.Expired)
		}
		if prefix > l.Next {
			return failure(authorization.Invalid)
		}
		// A peer cannot acknowledge a position which has never been sent.
		for seq := l.Ack + 1; seq <= prefix; seq++ {
			r, ok := l.Out[seq]
			if !ok || (r.Attempts == 0 && r.DispositionAttempts == 0) {
				return failure(authorization.Invalid)
			}
		}
		l.Ack = max(l.Ack, prefix)
		return nil
	})
}

func (j *Journal) Cursor(ctx context.Context) (*wire.WSCursor, error) {
	var out *wire.WSCursor
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		out = &wire.WSCursor{Kind: "RESUME", Generation: l.ReceivedGeneration, Prefix: l.Prefix}
		return nil
	})
	return out, e
}
func (j *Journal) Sending(ctx context.Context) (uint64, uint64, error) {
	var generation, next uint64
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		generation, next = l.Generation, l.Next
		return nil
	})
	return generation, next, e
}
func (j *Journal) Request(ctx context.Context, id string) (*wire.CapabilityRequest, error) {
	var out *wire.CapabilityRequest
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		out = nil
		for _, r := range l.Out {
			m, e := decodeReliable(r)
			if e != nil {
				return e
			}
			if m.MessageId == id && m.Request != nil {
				out = m.Request
				return nil
			}
		}
		for _, r := range l.Recovery {
			if !r.Incoming && r.MessageID == id {
				out = &wire.CapabilityRequest{MessageId: id, Namespace: r.Namespace, Body: &wire.CapabilityRequest_Invoke{Invoke: &wire.InvokeCapability{Invocation: &wire.CapabilityInvocation{OperationId: r.OperationID}}}}
				return nil
			}
		}
		return failure(authorization.NotFound)
	})
	return out, e
}
func (j *Journal) replyGuard(ctx context.Context, id string) (*wire.CapabilityRequest, error) {
	var out *wire.CapabilityRequest
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		out = nil
		for _, r := range l.Out {
			m, e := decodeReliable(r)
			if e != nil {
				return e
			}
			if m.MessageId == id && m.Response != nil {
				if m.Response.GetFailure() != nil {
					return nil
				}
				out = new(wire.CapabilityRequest)
				if len(r.Guard) == 0 || proto.Unmarshal(r.Guard, out) != nil {
					return failure(authorization.Invalid)
				}
				return nil
			}
		}
		return failure(authorization.NotFound)
	})
	return out, e
}
func (j *Journal) Result(ctx context.Context, id string) (*wire.CapabilityResponse, error) {
	var out *wire.CapabilityResponse
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		out = nil
		for seq, r := range l.In {
			m, e := decodeReliable(r)
			if e != nil {
				return e
			}
			if seq <= l.Prefix && m.Response != nil && m.Response.ReplyTo == id {
				out = m.Response
				return nil
			}
		}
		return failure(authorization.NotFound)
	})
	return out, e
}

// Claim fences earlier connections, including another process using this store.
func (j *Journal) Claim(ctx context.Context) (*Journal, error) {
	var session uint64
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		if l.Session >= 1<<53 {
			return failure(authorization.Unavailable)
		}
		l.Session++
		session = l.Session
		return nil
	})
	if e != nil {
		return nil, e
	}
	copy := *j
	copy.session = session
	return &copy, nil
}
func (j *Journal) checkSession(ctx context.Context) error {
	return j.update(ctx, func(*deliveryLog, authorization.DeliveryTransaction) error { return nil })
}
func (j *Journal) canConsume(ctx context.Context, m *wire.WSReliable) error {
	return j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		r, ok := l.In[m.Position]
		if !ok || m.Generation != l.ReceivedGeneration {
			return failure(authorization.Expired)
		}
		if tx.Now().UnixNano() >= r.Until || r.Attempts >= j.config.Attempts {
			return failure(authorization.Expired)
		}
		r.Attempts++
		l.In[m.Position] = r
		return nil
	})
}

// DeliveryStatus reports transport durability, independently of business effect.
type DeliveryStatus struct {
	Generation, ReceivedGeneration, Allocated, Confirmed, Received uint64
	Outbox, Inbox, Recovering                                      int
}

func (j *Journal) Status(ctx context.Context) (DeliveryStatus, error) {
	var out DeliveryStatus
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		out = DeliveryStatus{Generation: l.Generation, ReceivedGeneration: l.ReceivedGeneration, Allocated: l.Next, Confirmed: l.Ack, Received: l.Prefix, Outbox: len(l.Out), Inbox: len(l.In), Recovering: len(l.Recovery)}
		return nil
	})
	return out, e
}

// Attempt durably charges one attempted transmission. An omission has its own
// equally finite control budget and does not reset original payload attempts.
func (j *Journal) Attempt(ctx context.Context, m *wire.WSReliable) error {
	return j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		r, ok := l.Out[m.Position]
		if !ok || m.Generation != l.Generation {
			return failure(authorization.Expired)
		}
		saved, e := decodeReliable(r)
		if e != nil {
			return e
		}
		if !proto.Equal(saved, m) {
			return failure(authorization.IdentityConflict)
		}
		if len(m.OmittedSha256) > 0 {
			if r.DispositionAttempts >= j.config.Attempts {
				return failure(authorization.Unavailable)
			}
			r.DispositionAttempts++
		} else {
			if r.Attempts >= j.config.Attempts {
				return failure(authorization.Unavailable)
			}
			r.Attempts++
		}
		l.Out[m.Position] = r
		return nil
	})
}

// Omit retires a refused payload without creating a reliable sequence hole.
// The receiver records the original digest and never executes this disposition.
func (j *Journal) Omit(ctx context.Context, m *wire.WSReliable) (*wire.WSReliable, error) {
	var out *wire.WSReliable
	e := j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		r, ok := l.Out[m.Position]
		if !ok || m.Generation != l.Generation {
			return failure(authorization.Expired)
		}
		old, e := decodeReliable(r)
		if e != nil {
			return e
		}
		if len(old.OmittedSha256) > 0 {
			out = old
			return nil
		}
		if !proto.Equal(old, m) {
			return failure(authorization.IdentityConflict)
		}
		if e = archive(l, map[uint64]retained{m.Position: r}, false); e != nil {
			return e
		}
		hash := sha256.Sum256(r.Raw)
		out = &wire.WSReliable{Generation: m.Generation, Position: m.Position, MessageId: m.MessageId, OmittedSha256: hash[:]}
		r.Raw, e = proto.Marshal(out)
		if e != nil {
			return e
		}
		r.Guard = nil
		l.Out[m.Position] = r
		return nil
	})
	return out, e
}

// Reject persists only the original digest and a generic failure reply. A
// per-object policy denial must not retain its forbidden body or stop its peers.
func (j *Journal) Reject(ctx context.Context, m *wire.WSReliable, responseID string, denial authorization.Code) (uint64, error) {
	if denial != authorization.Denied && denial != authorization.Expired {
		return 0, failure(authorization.Invalid)
	}
	raw, e := reliableBytes(m)
	if e != nil {
		return 0, e
	}
	hash := sha256.Sum256(raw)
	marker := &wire.WSReliable{Generation: m.Generation, Position: m.Position, MessageId: m.MessageId, OmittedSha256: hash[:]}
	var prefix uint64
	e = j.update(ctx, func(l *deliveryLog, tx authorization.DeliveryTransaction) error {
		var e error
		prefix, e = receiveLog(l, tx, marker)
		if e != nil {
			return e
		}
		r := l.In[m.Position]
		if m.Request != nil && r.ReplyID == "" {
			response := &wire.CapabilityResponse{MessageId: responseID, ReplyTo: m.MessageId, Namespace: m.Request.Namespace, Evidence: "durable_capability_execution", Body: &wire.CapabilityResponse_Failure{Failure: &wire.CapabilityFailure{Code: string(denial)}}}
			if _, e = appendReliable(l, tx, &wire.WSReliable{MessageId: responseID, Response: response}); e != nil {
				return e
			}
			r.ReplyID = responseID
		}
		r.Raw = nil
		r.Consumed = true
		l.In[m.Position] = r
		return nil
	})
	return prefix, e
}
