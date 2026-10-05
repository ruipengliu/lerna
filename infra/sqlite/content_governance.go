package sqlite

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) SaveContentRegistration(ctx context.Context, r *v1.ContentRegistration) error {
	return s.saveRecord(ctx, "content", "INSERT INTO content_registrations VALUES(?,?,?,?,?) ON CONFLICT(user_id,id) DO UPDATE SET record=excluded.record", r, r.Content.Ref.Name.UserId, r.Content.Ref.Name.LocalId, r.Content.ContentId, r.Content.ContentVersion)
}
func (s *Store) LoadContentRegistration(ctx context.Context, r *v1.Ref) (*v1.ContentRegistration, error) {
	v := new(v1.ContentRegistration)
	ok, e := s.load(ctx, v, "SELECT record FROM content_registrations WHERE user_id=? AND id=?", r.Name.UserId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) PendingContentRegistrations(ctx context.Context) ([]*v1.ContentRegistration, error) {
	var all []*v1.ContentRegistration
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM content_registrations WHERE user_id=?", s.user)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			r := new(v1.ContentRegistration)
			if e = proto.Unmarshal(b, r); e != nil {
				return e
			}
			if r.State != "PUBLISHED" {
				all = append(all, r)
			}
		}
		return rows.Err()
	})
	return all, e
}
func (s *Store) ContentTime(ctx context.Context) (int64, error) {
	tx, e := s.writer(ctx, "content")
	if e != nil {
		return 0, e
	}
	var now int64
	e = tx.QueryRowContext(ctx, "SELECT CAST(unixepoch('subsec')*1000 AS INTEGER)").Scan(&now)
	return now, e
}
func (s *Store) AcceptContentBody(ctx context.Context, d *v1.BodyDeposit, body []byte) (*v1.BodyReceipt, error) {
	var receipt *v1.BodyReceipt
	e := s.transact(ctx, "body", "body.accept", func(txctx context.Context) error {
		var e error
		receipt, e = s.QueryContentBodyReceipt(txctx, d)
		if e != nil {
			return e
		}
		fingerprint := command.SemanticFingerprint("body", d, append([]byte{}, body...))
		if receipt != nil {
			if receipt.Fingerprint != fingerprint {
				return command.Fail("IDEMPOTENCY_CONFLICT")
			}
			return nil
		}
		if d.Digest != fmt.Sprintf("%x", sha256.Sum256(body)) || d.ByteSize != uint64(len(body)) {
			return command.Fail("CONTENT_INTEGRITY")
		}
		tx, e := s.writer(txctx, "body")
		if e != nil {
			return e
		}
		var now int64
		if e = tx.QueryRowContext(txctx, "SELECT CAST(unixepoch('subsec')*1000 AS INTEGER)").Scan(&now); e != nil {
			return e
		}
		receipt = &v1.BodyReceipt{Deposit: d, Fingerprint: fingerprint, AcceptedAtUnixMs: now, DurabilityProfile: "LOCAL"}
		b, e := proto.Marshal(receipt)
		if e != nil {
			return e
		}
		if body == nil {
			body = []byte{}
		}
		_, e = tx.ExecContext(txctx, "INSERT INTO content_bodies VALUES(?,?,?,?,?,?,?)", d.ContentRef.Name.UserId, d.LocationRef.Name.LocalId, d.Identity.IssuerId, d.Identity.TargetDomainId, d.Identity.CommandId, body, b)
		return storageError(e, false)
	})
	return receipt, e
}
func (s *Store) QueryContentBodyReceipt(ctx context.Context, d *v1.BodyDeposit) (*v1.BodyReceipt, error) {
	r := new(v1.BodyReceipt)
	ok, e := s.load(ctx, r, "SELECT receipt FROM content_bodies WHERE user_id=? AND issuer_id=? AND domain_id=? AND command_id=?", d.Identity.UserId, d.Identity.IssuerId, d.Identity.TargetDomainId, d.Identity.CommandId)
	if !ok {
		return nil, e
	}
	if !proto.Equal(r.Deposit, d) {
		return nil, command.Fail("IDEMPOTENCY_CONFLICT")
	}
	return r, e
}
func (s *Store) ReadContentBody(ctx context.Context, d *v1.BodyDeposit) ([]byte, error) {
	if e := contentReadBoundary(ctx); e != nil {
		return nil, e
	}
	var b []byte
	e := s.read(ctx, func(q querier) error {
		return q.QueryRowContext(ctx, "SELECT body FROM content_bodies WHERE user_id=? AND id=?", d.ContentRef.Name.UserId, d.LocationRef.Name.LocalId).Scan(&b)
	})
	return b, storageError(e, false)
}

func (s *Store) SaveContentDerivation(ctx context.Context, d *v1.ContentDerivation) error {
	return s.saveRecord(ctx, "content", "INSERT INTO content_derivations VALUES(?,?,?) ON CONFLICT(user_id,id) DO UPDATE SET record=excluded.record", d, d.Ref.Name.UserId, d.Ref.Name.LocalId)
}
func (s *Store) LoadContentDerivation(ctx context.Context, r *v1.Ref) (*v1.ContentDerivation, error) {
	d := new(v1.ContentDerivation)
	ok, e := s.load(ctx, d, "SELECT record FROM content_derivations WHERE user_id=? AND id=?", r.Name.UserId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return d, e
}

func (s *Store) NextContentVersion(ctx context.Context, id string) (uint64, error) {
	tx, e := s.writer(ctx, "content")
	if e != nil {
		return 0, e
	}
	var version uint64
	e = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(content_version),0)+1 FROM content_registrations WHERE user_id=? AND content_id=?", s.user, id).Scan(&version)
	return version, e
}

// BodyReceipts 提供持有方自己的只读回执，不经内容域发布状态解释。
type BodyReceipts struct{ store *Store }

func (s *Store) BodyReceipts() *BodyReceipts { return &BodyReceipts{store: s} }
func (b *BodyReceipts) QueryReceipt(ctx context.Context, caller *v1.Caller, d *v1.BodyDeposit) (*v1.BodyReceipt, error) {
	if e := command.CheckCaller(caller, b.store.user); e != nil {
		return nil, e
	}
	if d == nil || d.ContentRef == nil || d.LocationRef == nil || d.Identity == nil {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(caller, d.ContentRef.Name, b.store.user, b.store.domain+"/content", "content"); e != nil {
		return nil, e
	}
	if e := command.CheckName(caller, d.LocationRef.Name, b.store.user, b.store.domain+"/content/body", "body"); e != nil {
		return nil, e
	}
	if d.Identity.UserId != b.store.user || d.Identity.IssuerId != "content-holder" || d.Identity.TargetDomainId != b.store.domain+"/content/body" || d.Identity.CommandId != d.ContentRef.Name.LocalId {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return b.store.QueryContentBodyReceipt(ctx, d)
}
