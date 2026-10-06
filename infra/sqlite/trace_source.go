package sqlite

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func traceSourceDomain(producer string) string {
	switch producer {
	case "tasks", "sessions", "grants", "budget":
		return "adjudication"
	case "ledger":
		return "ledger"
	case "content":
		return "content"
	}
	return ""
}

// SaveTraceSource 将源序号及白名单结构与负责方事实原子保存。
func (s *Store) SaveTraceSource(ctx context.Context, producer string, event *v1.TraceEvent) error {
	_, err := s.CreateTraceSource(ctx, producer, event, nil)
	return err
}

// CreateTraceSource 为已有原交接保留命令身份；元数据仍只进入源方 outbox。
func (s *Store) CreateTraceSource(ctx context.Context, producer string, event *v1.TraceEvent, header *v1.CommandHeader) (*v1.AcceptTraceCommand, error) {
	domain := traceSourceDomain(producer)
	if domain == "" {
		return nil, command.Fail("INVALID_TRACE_SOURCE")
	}
	tx, err := s.writer(ctx, domain)
	if err != nil {
		return nil, err
	}
	event = proto.Clone(event).(*v1.TraceEvent)
	event.Producer = producer
	event.SourceStreamId = s.domain + "/" + producer
	event.MappingVersion = "lerna.m1.local.v1"
	event.SourceSchemaVersion = 1
	event.ProducerVersion = "lerna-m1-source-v1"
	if event.Ref == nil {
		event.Ref = command.NewRef(s.user, s.domain+"/trace", "trace-event", "lerna.v1.TraceEvent")
	}
	refs := make([]*v1.Ref, 0, len(event.RelatedRefs))
	for _, ref := range event.RelatedRefs {
		if ref != nil && ref.Name != nil {
			refs = append(refs, ref)
		}
	}
	event.RelatedRefs = refs
	if err = tx.QueryRowContext(ctx, "INSERT INTO source_trace_streams VALUES(?,?,1) ON CONFLICT(user_id,producer) DO UPDATE SET seq=seq+1 RETURNING seq", s.user, producer).Scan(&event.SourceSeq); err != nil {
		return nil, storageError(err, false)
	}
	if header == nil {
		header = &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: s.user, IssuerId: "trace-source/" + producer, TargetDomainId: s.domain + "/trace", CommandId: event.Ref.Name.LocalId}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
	}
	c := &v1.AcceptTraceCommand{Header: header, Event: event}
	source := &v1.TraceSourceRecord{Command: c}
	err = s.saveRecord(ctx, domain, "INSERT INTO source_trace_outbox VALUES(?,?,?,?,?,?,?,?)", source, s.user, producer, event.SourceSeq, header.Identity.IssuerId, header.Identity.TargetDomainId, header.Identity.CommandId, command.SemanticFingerprint("accept-trace", event))
	return c, err
}
func (s *Store) TraceSources(ctx context.Context) ([]*v1.TraceSourceRecord, error) {
	var result []*v1.TraceSourceRecord
	err := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM source_trace_outbox WHERE user_id=? ORDER BY producer,seq", s.user)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.TraceSourceRecord)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			result = append(result, v)
		}
		return rows.Err()
	})
	return result, err
}
func (s *Store) LoadTraceSource(ctx context.Context, id *v1.CommandIdentity) (*v1.TraceSourceRecord, error) {
	v := new(v1.TraceSourceRecord)
	ok, err := s.load(ctx, v, "SELECT record FROM source_trace_outbox WHERE user_id=? AND issuer=? AND target_domain=? AND id=?", s.user, id.IssuerId, id.TargetDomainId, id.CommandId)
	if !ok {
		return nil, err
	}
	return v, err
}
func (s *Store) AcknowledgeTraceSource(ctx context.Context, r *v1.CommandReceipt) error {
	source, err := s.LoadTraceSource(ctx, r.Identity)
	if err != nil {
		return err
	}
	if source == nil {
		return command.Fail("INVALID_TRACE_SOURCE")
	}
	return s.transact(ctx, traceSourceDomain(source.Command.Event.Producer), "trace.source_ack", func(tx context.Context) error {
		current, e := s.LoadTraceSource(tx, r.Identity)
		if e != nil {
			return e
		}
		if !proto.Equal(current.Command.Header.Identity, r.Identity) || r.Decision != v1.Decision_DECISION_ACCEPTED || r.Fingerprint != command.SemanticFingerprint("accept-trace", current.Command.Event) {
			return command.Fail("INVALID_RECEIPT")
		}
		if current.Receipt != nil {
			if !proto.Equal(current.Receipt, r) {
				return command.Fail("IDEMPOTENCY_CONFLICT")
			}
			return nil
		}
		current.Receipt = r
		return s.saveRecord(tx, traceSourceDomain(current.Command.Event.Producer), "UPDATE source_trace_outbox SET record=?5 WHERE user_id=?1 AND issuer=?2 AND target_domain=?3 AND id=?4", current, s.user, r.Identity.IssuerId, r.Identity.TargetDomainId, r.Identity.CommandId)
	})
}
func (s *Store) AllTraceEvents(ctx context.Context) ([]*v1.TraceEvent, error) {
	var result []*v1.TraceEvent
	err := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM trace_events WHERE user_id=?", s.user)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.TraceEvent)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			result = append(result, v)
		}
		return rows.Err()
	})
	return result, err
}

func (s *Store) IndexTraceEvents(ctx context.Context) error {
	return s.transact(ctx, "trace", "trace.index", func(tx context.Context) error {
		w, err := s.writer(tx, "trace")
		if err != nil {
			return err
		}
		_, err = w.ExecContext(tx, "INSERT OR IGNORE INTO trace_indexes SELECT user_id,id FROM trace_events WHERE user_id=?", s.user)
		return storageError(err, false)
	})
}
func (s *Store) IndexedTraceEvents(ctx context.Context) (map[string]bool, error) {
	result := map[string]bool{}
	err := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT id FROM trace_indexes WHERE user_id=?", s.user)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				return e
			}
			result[id] = true
		}
		return rows.Err()
	})
	return result, err
}

func (s *Store) TraceSourceHeads(ctx context.Context) ([]*v1.TraceProgress, error) {
	var result []*v1.TraceProgress
	err := s.read(ctx, func(q querier) error {
		for _, producer := range []string{"tasks", "sessions", "grants", "budget", "ledger", "content"} {
			p := &v1.TraceProgress{SourceStreamId: s.domain + "/" + producer, SourceKnown: true}
			err := q.QueryRowContext(ctx, "SELECT COALESCE((SELECT seq FROM source_trace_streams WHERE user_id=? AND producer=?),0)", s.user, producer).Scan(&p.SourceHighWater)
			if err != nil {
				return err
			}
			result = append(result, p)
		}
		return nil
	})
	return result, err
}
