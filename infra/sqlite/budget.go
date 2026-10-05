package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ruipengliu/lerna/contracts/command"
	"google.golang.org/protobuf/proto"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Store) saveBudgetVersion(ctx context.Context, b *v1.Budget) error {
	old, e := s.LoadBudgetVersion(ctx, b.Ref)
	if e != nil {
		return e
	}
	if old != nil {
		if !proto.Equal(old, b) {
			return command.Fail("IMMUTABLE_REFERENCE_CONFLICT")
		}
		return nil
	}
	return s.saveRecord(ctx, "adjudication", "INSERT INTO budget_versions VALUES(?,?,?,?,?)", b, b.Ref.Name.UserId, b.Ref.Name.AuthorityDomainId, b.Ref.Name.LocalId, b.Ref.Revision)
}
func (s *Store) LoadBudgetVersion(ctx context.Context, r *v1.Ref) (*v1.Budget, error) {
	b := new(v1.Budget)
	ok, e := s.load(ctx, b, "SELECT record FROM budget_versions WHERE user_id=? AND domain_id=? AND id=? AND revision=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId, r.Revision)
	if !ok {
		return nil, e
	}
	return b, e
}
func (s *Store) SaveBillingSource(ctx context.Context, b *v1.BillingSource) error {
	if e := s.saveRecord(ctx, "adjudication", "INSERT INTO billing_source_versions VALUES(?,?,?,?,?)", b, b.Ref.Name.UserId, b.Ref.Name.AuthorityDomainId, b.Ref.Name.LocalId, b.Ref.Revision); e != nil {
		return e
	}
	return s.saveRecord(ctx, "adjudication", "INSERT INTO billing_sources VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,send_id) DO UPDATE SET record=excluded.record", b, b.Ref.Name.UserId, b.Ref.Name.AuthorityDomainId, b.SendRef.Name.LocalId)
}
func (s *Store) LoadBillingSource(ctx context.Context, r *v1.Ref) (*v1.BillingSource, error) {
	b := new(v1.BillingSource)
	ok, e := s.load(ctx, b, "SELECT record FROM billing_sources WHERE user_id=? AND domain_id=? AND send_id=?", r.Name.UserId, s.domain, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return b, e
}
func (s *Store) SaveBillingEntry(ctx context.Context, b *v1.BillingEntry) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO billing_entries VALUES(?,?,?,?,?,?)", b, b.Ref.Name.UserId, b.Ref.Name.AuthorityDomainId, b.Ref.Name.LocalId, b.SourceRef.Name.LocalId, b.SourceVersion)
}
func (s *Store) LoadBillingEntry(ctx context.Context, r *v1.Ref) (*v1.BillingEntry, error) {
	b := new(v1.BillingEntry)
	ok, e := s.load(ctx, b, "SELECT record FROM billing_entries WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return b, e
}
func (s *Store) LoadBillingAlias(ctx context.Context, alias string) (string, error) {
	var send string
	e := s.read(ctx, func(q querier) error {
		return q.QueryRowContext(ctx, "SELECT send_id FROM billing_aliases WHERE user_id=? AND alias=?", s.user, alias).Scan(&send)
	})
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil
	}
	return send, e
}
func (s *Store) SaveBillingAlias(ctx context.Context, alias, send string) error {
	tx, e := s.writer(ctx, "adjudication")
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO billing_aliases VALUES(?,?,?)", s.user, alias, send)
	return storageError(e, false)
}
func (s *Store) SaveReservationRelease(ctx context.Context, r *v1.ReservationRelease) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO reservation_releases VALUES(?,?,?,?)", r, r.Ref.Name.UserId, r.Ref.Name.AuthorityDomainId, r.ReservationRef.Name.LocalId)
}
func (s *Store) LoadReservationRelease(ctx context.Context, r *v1.Ref) (*v1.ReservationRelease, error) {
	v := new(v1.ReservationRelease)
	ok, e := s.load(ctx, v, "SELECT record FROM reservation_releases WHERE user_id=? AND domain_id=? AND reservation_id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) SaveBillingConflict(ctx context.Context, c *v1.BillingConflict) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO billing_conflicts VALUES(?,?,?,?)", c, c.Ref.Name.UserId, c.Ref.Name.AuthorityDomainId, c.Ref.Name.LocalId)
}
func (s *Store) LoadBillingConflict(ctx context.Context, r *v1.Ref) (*v1.BillingConflict, error) {
	c := new(v1.BillingConflict)
	ok, e := s.load(ctx, c, "SELECT record FROM billing_conflicts WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return c, e
}
func (s *Store) LoadBillingSourceVersion(ctx context.Context, r *v1.Ref) (*v1.BillingSource, error) {
	v := new(v1.BillingSource)
	ok, e := s.load(ctx, v, "SELECT record FROM billing_source_versions WHERE user_id=? AND domain_id=? AND id=? AND revision=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId, r.Revision)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) AllReservations(ctx context.Context) ([]*v1.Reservation, error) {
	var result []*v1.Reservation
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM send_reservations WHERE user_id=? AND domain_id=?", s.user, s.domain)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.Reservation)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			result = append(result, v)
		}
		return rows.Err()
	})
	return result, e
}
