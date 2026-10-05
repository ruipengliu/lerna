package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// TraceWork 将结构化轨迹提交与效果、预算提交分开排序。
type TraceWork struct{ *Store }

func (s *Store) TraceWork() *TraceWork { return &TraceWork{s} }
func (w *TraceWork) Transaction(ctx context.Context, point string, fn func(context.Context) error) error {
	return w.traceWorkTransaction(ctx, point, fn)
}
func (w *TraceWork) BusinessScope(ctx context.Context, fn func(context.Context) error) error {
	return w.businessScope(ctx, "trace", fn)
}
func (w *TraceWork) Position(ctx context.Context) (uint64, int64, error) {
	tx, e := w.writer(ctx, "trace")
	if e != nil {
		return 0, 0, e
	}
	var pos uint64
	var now int64
	e = tx.QueryRowContext(ctx, "UPDATE trace_commit_clock SET position=position+1 WHERE singleton=1 RETURNING position, CAST(unixepoch('subsec')*1000 AS INTEGER)").Scan(&pos, &now)
	return pos, now, storageError(e, false)
}
func (w *TraceWork) SaveReceipt(ctx context.Context, r *v1.CommandReceipt) error {
	return w.saveRecord(ctx, "trace", "INSERT INTO command_receipts VALUES(?,?,?,?,?)", r, r.Identity.UserId, r.Identity.IssuerId, r.Identity.TargetDomainId, r.Identity.CommandId)
}
func (s *Store) SaveReports(ctx context.Context, r *v1.ObservationReports) error {
	return s.saveRecord(ctx, "ledger", "INSERT INTO observation_reports VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", r, r.ObservationRef.Name.UserId, r.ObservationRef.Name.AuthorityDomainId, r.ObservationRef.Name.LocalId)
}
func (s *Store) LoadReports(ctx context.Context, r *v1.Ref) (*v1.ObservationReports, error) {
	v := new(v1.ObservationReports)
	ok, e := s.load(ctx, v, "SELECT record FROM observation_reports WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) AllReports(ctx context.Context) ([]*v1.ObservationReports, error) {
	var all []*v1.ObservationReports
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM observation_reports WHERE user_id=? AND domain_id=?", s.user, s.domain+"/ledger")
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.ObservationReports)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			all = append(all, v)
		}
		return rows.Err()
	})
	return all, e
}
func (s *Store) SaveUsage(ctx context.Context, r *v1.UsageReport) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO usage_reports VALUES(?,?,?,?,?,?)", r, r.Ref.Name.UserId, r.Ref.Name.AuthorityDomainId, r.Ref.Name.LocalId, r.BillingSource.Name.AuthorityDomainId+":"+r.BillingSource.Name.LocalId, r.SourceRevision)
}
func (s *Store) LoadUsage(ctx context.Context, r *v1.Ref) (*v1.UsageReport, error) {
	v := new(v1.UsageReport)
	ok, e := s.load(ctx, v, "SELECT record FROM usage_reports WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) SaveTraceEvent(ctx context.Context, r *v1.TraceEvent) error {
	return s.saveRecord(ctx, "trace", "INSERT INTO trace_events VALUES(?,?,?,?)", r, r.Ref.Name.UserId, r.Ref.Name.AuthorityDomainId, r.Ref.Name.LocalId)
}
func (s *Store) LoadTraceEvent(ctx context.Context, r *v1.Ref) (*v1.TraceEvent, error) {
	v := new(v1.TraceEvent)
	ok, e := s.load(ctx, v, "SELECT record FROM trace_events WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) SaveInterpretation(ctx context.Context, r *v1.EffectInterpretation) error {
	return s.saveRecord(ctx, "ledger", "INSERT INTO effect_interpretations VALUES(?,?,?,?)", r, r.Ref.Name.UserId, r.Ref.Name.AuthorityDomainId, r.Ref.Name.LocalId)
}
func (s *Store) LoadInterpretation(ctx context.Context, r *v1.Ref) (*v1.EffectInterpretation, error) {
	v := new(v1.EffectInterpretation)
	ok, e := s.load(ctx, v, "SELECT record FROM effect_interpretations WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
