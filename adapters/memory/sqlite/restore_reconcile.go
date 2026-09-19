package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"lerna/memory"
)

type recoveryState struct {
	Progress memory.RecoveryProgress
	Config   string
}

func recoveryStateAt(ctx context.Context, q querier, scope memory.RecoveryScope) (recoveryState, error) {
	out := recoveryState{Progress: memory.RecoveryProgress{Scope: scope}}
	var epoch string
	p := &out.Progress
	err := q.QueryRowContext(ctx, `SELECT config,authority,epoch,origin,verified,applied,retained,missing,state FROM memory_recovery_scopes WHERE namespace=? AND collection=?`, scope.Namespace, scope.Collection).Scan(&out.Config, &p.Authority, &epoch, &p.OriginPosition, &p.VerifiedPosition, &p.AppliedPosition, &p.Retained, &p.Missing, &p.State)
	if err == sql.ErrNoRows {
		return out, memory.Missing
	}
	if err != nil {
		return out, memory.Unavailable
	}
	p.Epoch, err = strconv.ParseUint(epoch, 10, 64)
	if err != nil || p.Epoch == 0 || p.VerifiedPosition > p.OriginPosition || p.AppliedPosition > 1<<32 || p.OriginPosition > 1<<32 || p.Retained < 0 || p.Missing < 0 || p.Retained+p.Missing > 512 || (p.State != "verifying" && p.State != "sanitized") {
		return out, memory.Unavailable
	}
	return out, nil
}

// InspectRecovery reports only this file's last committed maintenance point.
// It does not imply that the authority has stopped changing or grant data access.
func (r *Restore) InspectRecovery(ctx context.Context, scope memory.RecoveryScope) (memory.RecoveryProgress, error) {
	if !memory.ValidRecoveryScope(scope) {
		return memory.RecoveryProgress{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	state, err := recoveryStateAt(ctx, r.store.db, scope)
	return state.Progress, err
}

func recoveryProof(ctx context.Context, b memory.RecoveryBinding, after uint64) (memory.RecoveryProof, error) {
	if !memory.ValidRecoveryScope(b.Scope) || b.Source == nil || b.Verifier == nil || b.Verifier.Configuration() == ([32]byte{}) {
		return memory.RecoveryProof{}, memory.Invalid
	}
	challenge := memory.RecoveryChallenge{Scope: b.Scope, After: after}
	if _, err := rand.Read(challenge.Nonce[:]); err != nil {
		return memory.RecoveryProof{}, memory.Unavailable
	}
	proof, err := b.Source.ProveRecovery(ctx, challenge)
	if err != nil {
		return memory.RecoveryProof{}, err
	}
	if proof.Challenge != challenge || proof.Snapshot.Scope != b.Scope || proof.Snapshot.After != after || !memory.ValidRecoverySnapshot(proof.Snapshot) {
		return memory.RecoveryProof{}, memory.Denied
	}
	if err = b.Verifier.VerifyRecovery(challenge, proof); err != nil {
		return memory.RecoveryProof{}, err
	}
	return proof, nil
}

// Reconcile verifies at most one original operation page per call. Progress and
// eventual erasure are durable; callers resume until sanitized. This never clears
// the ordinary-runtime quarantine or creates a new business operation.
func (r *Restore) Reconcile(ctx context.Context, b memory.RecoveryBinding) (memory.RecoveryProgress, error) {
	p, _, err := r.reconcile(ctx, b)
	return p, err
}

func (r *Restore) reconcile(ctx context.Context, b memory.RecoveryBinding) (memory.RecoveryProgress, memory.RecoveryProof, error) {
	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	fail := func(err error) (memory.RecoveryProgress, memory.RecoveryProof, error) {
		return memory.RecoveryProgress{}, memory.RecoveryProof{}, err
	}
	if !memory.ValidRecoveryScope(b.Scope) || b.Verifier == nil || b.Source == nil {
		return fail(memory.Invalid)
	}
	initial, err := recoveryStateAt(ctx, r.store.db, b.Scope)
	if err != nil && err != memory.Missing {
		return fail(err)
	}
	found := err == nil
	config := fmt.Sprintf("%x", b.Verifier.Configuration())
	after := uint64(0)
	if found && initial.Config == config {
		after = initial.Progress.VerifiedPosition
	}
	proof, err := recoveryProof(ctx, b, after)
	if err != nil {
		return fail(err)
	}
	write, cancelWrite := context.WithTimeout(ctx, Timeout)
	defer cancelWrite()
	tx, err := r.store.db.BeginTx(write, nil)
	if err != nil {
		return fail(memory.Unavailable)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(write, `UPDATE memory_lock SET version=version WHERE id=1`); err != nil {
		return fail(memory.Unavailable)
	}
	var isolated bool
	if err = tx.QueryRowContext(write, `SELECT quarantined FROM memory_recovery WHERE id=1`).Scan(&isolated); err != nil || !isolated {
		return fail(memory.Quarantined)
	}
	current, lookupErr := recoveryStateAt(write, tx, b.Scope)
	if lookupErr != nil && lookupErr != memory.Missing {
		return fail(lookupErr)
	}
	if (lookupErr == nil) != found || (found && current != initial) {
		return fail(memory.Conflict)
	}
	p := initial.Progress
	if !found {
		var scopes int
		if err = tx.QueryRowContext(write, `SELECT COUNT(*) FROM memory_recovery_scopes`).Scan(&scopes); err != nil {
			return fail(memory.Unavailable)
		}
		if scopes >= 512 {
			return fail(memory.Capacity)
		}
		var count uint64
		if err = tx.QueryRowContext(write, `SELECT COALESCE(MAX(position),0),COUNT(*) FROM (SELECT position FROM memory_operations WHERE namespace=? AND collection=? UNION ALL SELECT position FROM memory_erasure_events WHERE namespace=? AND collection=?)`, b.Scope.Namespace, b.Scope.Collection, b.Scope.Namespace, b.Scope.Collection).Scan(&p.OriginPosition, &count); err != nil || count != p.OriginPosition {
			return fail(memory.Unavailable)
		}
	}
	if proof.Snapshot.Position < p.OriginPosition || proof.Snapshot.Position < p.AppliedPosition {
		return fail(memory.Conflict)
	}
	if found && initial.Config == config && (p.Authority != proof.Authority || p.Epoch != proof.Epoch) {
		return fail(memory.IdentityConflict)
	}
	p.Authority, p.Epoch, p.VerifiedPosition, p.State = proof.Authority, proof.Epoch, after, "verifying"
	deleted := map[memory.Ref]uint64{}
	for _, v := range proof.Snapshot.Deleted {
		deleted[v.Ref] = v.Revision
	}
	erased := recoveryErased(proof.Snapshot)
	for _, receipt := range proof.Snapshot.Operations {
		if receipt.Position > p.OriginPosition {
			break
		}
		old, err := lookup(write, tx, b.Scope.Namespace, receipt.OperationID)
		if err != nil {
			return fail(memory.IdentityConflict)
		}
		if old.Ref != receipt.Ref || old.Subject != receipt.Subject || old.Revision != receipt.Revision || old.Position != receipt.Position || (deleted[old.Ref] <= old.Revision && erased[memory.VersionRef{Ref: old.Ref, Revision: old.Revision}].Position == 0 && old.SemanticSHA256 != receipt.SemanticSHA256) {
			return fail(memory.IdentityConflict)
		}
	}
	for _, event := range proof.Snapshot.Erased {
		if event.Position <= after || event.Position > min(p.OriginPosition, after+512) {
			continue
		}
		var count int
		if e := tx.QueryRowContext(write, `SELECT COUNT(*) FROM memory_erasure_events WHERE namespace=? AND collection=? AND record_key=? AND revision=? AND position=?`, event.Ref.Namespace, event.Ref.Collection, event.Ref.Key, event.Revision, event.Position).Scan(&count); e != nil {
			return fail(memory.Unavailable)
		}
		if count != 1 {
			return fail(memory.IdentityConflict)
		}
	}
	p.VerifiedPosition = min(p.OriginPosition, after+512)
	if p.VerifiedPosition == p.OriginPosition {
		retained, missing, err := sanitizeRecovery(write, tx, proof.Snapshot)
		if err != nil {
			return fail(err)
		}
		p.AppliedPosition, p.Retained, p.Missing, p.State = proof.Snapshot.Position, retained, missing, "sanitized"
	}
	_, err = tx.ExecContext(write, `INSERT INTO memory_recovery_scopes VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(namespace,collection) DO UPDATE SET config=excluded.config,authority=excluded.authority,epoch=excluded.epoch,origin=excluded.origin,verified=excluded.verified,applied=excluded.applied,retained=excluded.retained,missing=excluded.missing,state=excluded.state`, b.Scope.Namespace, b.Scope.Collection, config, p.Authority, strconv.FormatUint(p.Epoch, 10), p.OriginPosition, p.VerifiedPosition, p.AppliedPosition, p.Retained, p.Missing, p.State)
	if err != nil {
		return fail(memory.Unavailable)
	}
	if err = tx.Commit(); err != nil {
		return fail(memory.Unavailable)
	}
	return p, proof, nil
}

// ReadVerified is a trusted storage-maintenance seam, not a user disclosure API.
// A host exposing it must still apply current Memory permission and residency
// policy. Each call requires live proof; stored progress never authorizes a read.
func (r *Restore) ReadVerified(ctx context.Context, b memory.RecoveryBinding, version memory.VersionRef) (memory.Revision, error) {
	if version.Ref.Namespace != b.Scope.Namespace || version.Ref.Collection != b.Scope.Collection || !validRef(version.Ref) || version.Revision == 0 {
		return memory.Revision{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, proof, err := r.reconcile(ctx, b)
	if err != nil {
		return memory.Revision{}, err
	}
	if p.State != "sanitized" {
		return memory.Revision{}, memory.Quarantined
	}
	comparison := func(state memory.RecoverySnapshot) string {
		for _, record := range state.Records {
			if record.Version == version {
				return record.SHA256
			}
		}
		return ""
	}
	expected := comparison(proof.Snapshot)
	if expected == "" {
		return memory.Revision{}, memory.Missing
	}
	record, err := r.store.Read(ctx, version.Ref, version.Revision)
	if err != nil {
		return memory.Revision{}, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(record.Document)) != expected {
		return memory.Revision{}, memory.IdentityConflict
	}
	latest, err := recoveryProof(ctx, b, proof.Snapshot.Position)
	if err != nil {
		return memory.Revision{}, err
	}
	if comparison(latest.Snapshot) != expected {
		return memory.Revision{}, memory.Missing
	}
	return record, nil
}
