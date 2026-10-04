package target

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"time"
	"unicode/utf8"
)

var ErrPending = errors.New("original_request_pending")

func validateRequest(r Request) error {
	if !utf8.ValidString(r.Key) || !utf8.ValidString(r.Resource) || r.Key == "" || len(r.Key) > 128 || r.Resource == "" || len(r.Resource) > 128 || len(r.Data) > 1024*1024 {
		return errors.New("invalid bounded test write")
	}
	return nil
}
func requestDigest(r Request) string {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(r.Resource)))
	meaning := append(append(append([]byte{}, length[:]...), r.Resource...), r.Data...)
	sum := sha256.Sum256(meaning)
	return hex.EncodeToString(sum[:])
}
func (t *Target) window() (time.Time, time.Time, error) {
	now := t.cfg.Now().UTC()
	deadline := now.Add(t.cfg.Window)
	if now.Before(time.Unix(0, math.MinInt64)) || deadline.After(time.Unix(0, math.MaxInt64)) {
		return time.Time{}, time.Time{}, errors.New("test clock window cannot be represented as durable nanoseconds")
	}
	return now, deadline, nil
}
func writeRejection(err error) bool {
	return errors.Is(err, ErrPending) || errors.Is(err, ErrConflict) || errors.Is(err, ErrGuaranteeExpired)
}
func (t *Target) writeTx(ctx context.Context, tx *sql.Tx, r Request, receiveOnly bool) (Receipt, error) {
	now, deadline, err := t.window()
	if err != nil {
		return Receipt{}, err
	}
	digest := requestDigest(r)
	original, err := received(ctx, tx, r.Key)
	if err == nil {
		outcome := "replayed"
		var cause error
		if !now.Before(original.Deadline) {
			outcome = "guarantee_expired"
			cause = ErrGuaranteeExpired
		} else if original.Digest != digest {
			outcome = "conflict"
			cause = ErrConflict
		} else if original.Value.Version == 0 && !receiveOnly {
			outcome = "pending"
			cause = ErrPending
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO target_receives(original_key,at_ns,digest,outcome) VALUES(?,?,?,?)", r.Key, now.UnixNano(), digest, outcome); err != nil {
			return Receipt{}, err
		}
		return original, cause
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, err
	}
	out := Receipt{Key: r.Key, Digest: digest, Start: now, Deadline: deadline, Value: Value{Resource: r.Resource}}
	if _, err = tx.ExecContext(ctx, "INSERT INTO target_requests(original_key,resource,digest,start_ns,deadline_ns) VALUES(?,?,?,?,?)", r.Key, r.Resource, digest, now.UnixNano(), deadline.UnixNano()); err != nil {
		return Receipt{}, err
	}
	outcome := "received"
	if receiveOnly {
		_, err = tx.ExecContext(ctx, "INSERT INTO target_pending(original_key,data) VALUES(?,?)", r.Key, append([]byte{}, r.Data...))
	} else {
		out, err = applyTx(ctx, tx, out, r.Data)
		outcome = "applied"
	}
	if err != nil {
		return Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO target_receives(original_key,at_ns,digest,outcome) VALUES(?,?,?,?)", r.Key, now.UnixNano(), digest, outcome); err != nil {
		return Receipt{}, err
	}
	return out, nil
}
func applyTx(ctx context.Context, tx *sql.Tx, out Receipt, data []byte) (Receipt, error) {
	version := int64(0)
	err := tx.QueryRowContext(ctx, "SELECT version FROM target_values WHERE resource=?", out.Value.Resource).Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, err
	}
	out.Value.Version = version + 1
	out.Value.Data = append([]byte{}, data...)
	if _, err = tx.ExecContext(ctx, "INSERT INTO target_commits(original_key,version,data) VALUES(?,?,?)", out.Key, out.Value.Version, out.Value.Data); err != nil {
		return Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO target_values(resource,version,data) VALUES(?,?,?) ON CONFLICT(resource) DO UPDATE SET version=excluded.version,data=excluded.data", out.Value.Resource, out.Value.Version, out.Value.Data); err != nil {
		return Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM target_pending WHERE original_key=?", out.Key); err != nil {
		return Receipt{}, err
	}
	return out, nil
}
func applyReceivedTx(ctx context.Context, tx *sql.Tx, r Request) (Receipt, error) {
	original, err := received(ctx, tx, r.Key)
	if errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, ErrNotFound
	}
	if err != nil {
		return Receipt{}, err
	}
	if original.Digest != requestDigest(r) {
		return Receipt{}, ErrConflict
	}
	if original.Value.Version != 0 {
		return original, nil
	}
	var data []byte
	if err = tx.QueryRowContext(ctx, "SELECT data FROM target_pending WHERE original_key=?", r.Key).Scan(&data); err != nil {
		return Receipt{}, err
	}
	return applyTx(ctx, tx, original, data)
}
func received(ctx context.Context, tx *sql.Tx, key string) (Receipt, error) {
	var out Receipt
	var start, deadline int64
	var version sql.NullInt64
	err := tx.QueryRowContext(ctx, "SELECT r.original_key,r.resource,r.digest,r.start_ns,r.deadline_ns,c.version,c.data FROM target_requests r LEFT JOIN target_commits c USING(original_key) WHERE r.original_key=?", key).Scan(&out.Key, &out.Value.Resource, &out.Digest, &start, &deadline, &version, &out.Value.Data)
	if err != nil {
		return Receipt{}, err
	}
	out.Start = time.Unix(0, start).UTC()
	out.Deadline = time.Unix(0, deadline).UTC()
	out.Value.Version = version.Int64
	return out, nil
}
