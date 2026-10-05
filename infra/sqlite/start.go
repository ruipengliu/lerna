package sqlite

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) SaveStart(ctx context.Context, v *v1.StartRecord) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO starts VALUES(?,?,?,?,?)", v, v.Ref.Name.UserId, v.Ref.Name.AuthorityDomainId, v.Ref.Name.LocalId, v.SendRef.Name.LocalId)
}
func (s *Store) LoadStart(ctx context.Context, r *v1.Ref) (*v1.StartRecord, error) {
	v := new(v1.StartRecord)
	ok, e := s.load(ctx, v, "SELECT record FROM starts WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) LoadCurrentReservation(ctx context.Context, r *v1.Ref) (*v1.Reservation, error) {
	v := new(v1.Reservation)
	ok, e := s.load(ctx, v, "SELECT record FROM reservations WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) LoadReservationVersion(ctx context.Context, r *v1.Ref) (*v1.Reservation, error) {
	v := new(v1.Reservation)
	ok, e := s.load(ctx, v, "SELECT record FROM reservation_versions WHERE user_id=? AND domain_id=? AND id=? AND revision=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId, r.Revision)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) saveReservationVersion(ctx context.Context, r *v1.Reservation) error {
	old, e := s.LoadReservationVersion(ctx, r.Ref)
	if e != nil {
		return e
	}
	if old != nil {
		if !proto.Equal(old, r) {
			return command.Fail("IMMUTABLE_REFERENCE_CONFLICT")
		}
		return nil
	}
	return s.saveRecord(ctx, "adjudication", "INSERT INTO reservation_versions VALUES(?,?,?,?,?)", r, r.Ref.Name.UserId, r.Ref.Name.AuthorityDomainId, r.Ref.Name.LocalId, r.Ref.Revision)
}
func (s *Store) SaveSendConsumption(ctx context.Context, v *v1.SendConsumption) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO send_consumptions VALUES(?,?,?,?)", v, v.Ref.Name.UserId, v.Ref.Name.AuthorityDomainId, v.SendRef.Name.LocalId)
}
func (s *Store) LoadSendConsumption(ctx context.Context, r *v1.Ref) (*v1.SendConsumption, error) {
	v := new(v1.SendConsumption)
	ok, e := s.load(ctx, v, "SELECT record FROM send_consumptions WHERE user_id=? AND domain_id=? AND send_id=?", r.Name.UserId, s.domain, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
