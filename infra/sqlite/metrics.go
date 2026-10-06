package sqlite

import (
	"context"
	"errors"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/core/trace"
	"google.golang.org/protobuf/proto"
)

// metricSnapshot 使用延迟只读快照，不取得写事务或递增负责方提交位置。
func (s *Store) metricSnapshot(ctx context.Context, fn func(querier) error) (err error) {
	if ctx.Value(txKey{}) != nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e := s.conn.ExecContext(ctx, "BEGIN DEFERRED"); e != nil {
		return storageError(e, false)
	}
	defer func() {
		_, rollbackErr := s.conn.ExecContext(context.Background(), "ROLLBACK")
		err = errors.Join(err, rollbackErr)
	}()
	return fn(s.conn)
}

func metricRecords(ctx context.Context, q querier, table, user, domain string, prototype proto.Message) ([]proto.Message, error) {
	statement := "SELECT record FROM " + table + " WHERE user_id=?"
	args := []any{user}
	if domain != "" {
		statement += " AND domain_id=?"
		args = append(args, domain)
	}
	rows, e := q.QueryContext(ctx, statement, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var all []proto.Message
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		m := prototype.ProtoReflect().New().Interface()
		if e = proto.Unmarshal(b, m); e != nil {
			return nil, e
		}
		all = append(all, m)
	}
	return all, rows.Err()
}

func (s *Store) LedgerMetricFacts(ctx context.Context) (*ledger.MetricFacts, error) {
	facts := new(ledger.MetricFacts)
	e := s.metricSnapshot(ctx, func(q querier) error {
		if e := q.QueryRowContext(ctx, "SELECT position, CAST(unixepoch('subsec')*1000 AS INTEGER) FROM ledger_commit_clock WHERE singleton=1").Scan(&facts.Revision, &facts.CapturedAtUnixMs); e != nil {
			return e
		}
		operations, e := metricRecords(ctx, q, "operations", s.user, s.domain+"/ledger", &v1.Operation{})
		if e != nil {
			return e
		}
		for _, record := range operations {
			facts.Operations = append(facts.Operations, record.(*v1.Operation))
		}
		plans, e := metricRecords(ctx, q, "reconciliations", s.user, s.domain+"/ledger", &v1.Reconciliation{})
		if e != nil {
			return e
		}
		for _, record := range plans {
			facts.Reconciliations = append(facts.Reconciliations, record.(*v1.Reconciliation))
		}
		return nil
	})
	return facts, e
}

func (s *Store) TraceMetricFacts(ctx context.Context) (*trace.MetricFacts, error) {
	facts := &trace.MetricFacts{Indexed: map[string]bool{}}
	e := s.metricSnapshot(ctx, func(q querier) error {
		if e := q.QueryRowContext(ctx, "SELECT position, CAST(unixepoch('subsec')*1000 AS INTEGER) FROM trace_commit_clock WHERE singleton=1").Scan(&facts.AcceptancePosition, &facts.CapturedAtUnixMs); e != nil {
			return e
		}
		for _, producer := range []string{"tasks", "sessions", "grants", "budget", "ledger", "content"} {
			p := &v1.TraceProgress{SourceStreamId: s.domain + "/" + producer, SourceKnown: true}
			if e := q.QueryRowContext(ctx, "SELECT COALESCE((SELECT seq FROM source_trace_streams WHERE user_id=? AND producer=?),0)", s.user, producer).Scan(&p.SourceHighWater); e != nil {
				return e
			}
			facts.Heads = append(facts.Heads, p)
		}
		sources, e := metricRecords(ctx, q, "source_trace_outbox", s.user, "", &v1.TraceSourceRecord{})
		if e != nil {
			return e
		}
		for _, record := range sources {
			facts.Sources = append(facts.Sources, record.(*v1.TraceSourceRecord))
		}
		events, e := metricRecords(ctx, q, "trace_events", s.user, "", &v1.TraceEvent{})
		if e != nil {
			return e
		}
		for _, record := range events {
			facts.Events = append(facts.Events, record.(*v1.TraceEvent))
		}
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
			facts.Indexed[id] = true
		}
		return rows.Err()
	})
	return facts, e
}
