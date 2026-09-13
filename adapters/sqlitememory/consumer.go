package sqlitememory

import (
	"context"
	"database/sql"
	"encoding/hex"
	"lerna/memory"
)

const maxConsumers = 512

func validConsumer(b memory.ConsumerBinding) bool {
	hash, err := hex.DecodeString(b.ConfigSHA256)
	return label(b.Namespace) && label(b.Collection) && label(b.Consumer) && err == nil && len(hash) == 32 && hex.EncodeToString(hash) == b.ConfigSHA256
}
func consumerPosition(ctx context.Context, q querier, b memory.ConsumerBinding) (uint64, error) {
	var config string
	var position uint64
	err := q.QueryRowContext(ctx, `SELECT config,position FROM memory_consumers WHERE namespace=? AND collection=? AND consumer=?`, b.Namespace, b.Collection, b.Consumer).Scan(&config, &position)
	if err == sql.ErrNoRows {
		return 0, memory.Missing
	}
	if err != nil {
		return 0, memory.Unavailable
	}
	if config != b.ConfigSHA256 {
		return 0, memory.IdentityConflict
	}
	return position, nil
}
func (s *Store) BindConsumer(ctx context.Context, b memory.ConsumerBinding) (uint64, error) {
	if !validConsumer(b) {
		return 0, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, memory.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE memory_lock SET version=version WHERE id=1`); err != nil {
		return 0, memory.Unavailable
	}
	position, err := consumerPosition(ctx, tx, b)
	if err != memory.Missing {
		return position, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_consumers`).Scan(&count); err != nil {
		return 0, memory.Unavailable
	}
	if count >= maxConsumers {
		return 0, memory.Capacity
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_consumers VALUES(?,?,?,?,0)`, b.Namespace, b.Collection, b.Consumer, b.ConfigSHA256); err != nil {
		return 0, memory.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return 0, memory.Unavailable
	}
	return 0, nil
}
func (s *Store) AckEvent(ctx context.Context, b memory.ConsumerBinding, expected, next uint64) error {
	if !validConsumer(b) || expected >= 1<<63-1 || next != expected+1 {
		return memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE memory_lock SET version=version WHERE id=1`); err != nil {
		return memory.Unavailable
	}
	current, err := consumerPosition(ctx, tx, b)
	if err != nil {
		return err
	}
	if current == next {
		return nil
	}
	if current != expected {
		return memory.Conflict
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT position FROM memory_operations WHERE namespace=? AND collection=? AND position=? UNION ALL SELECT position FROM memory_erasure_events WHERE namespace=? AND collection=? AND position=?)`, b.Namespace, b.Collection, next, b.Namespace, b.Collection, next).Scan(&exists); err != nil {
		return memory.Unavailable
	}
	if exists != 1 {
		return memory.Conflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE memory_consumers SET position=? WHERE namespace=? AND collection=? AND consumer=? AND config=? AND position=?`, next, b.Namespace, b.Collection, b.Consumer, b.ConfigSHA256, expected); err != nil {
		return memory.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return memory.Unavailable
	}
	return nil
}

var _ memory.ConsumerProgress = (*Store)(nil)

func (s *Store) InspectConsumer(ctx context.Context, b memory.ConsumerBinding) (uint64, error) {
	if !validConsumer(b) {
		return 0, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	return consumerPosition(ctx, s.db, b)
}

var _ memory.ConsumerInspection = (*Store)(nil)
