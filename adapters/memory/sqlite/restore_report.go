package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"syscall"
	"unicode/utf8"

	"lerna/memory"
)

// CleanupTarget binds a read-only deletion-report target to this actual private
// backup file, collection and independently supplied recovery trust. It reports
// completed erasure only, never synchronization or runtime activation. It does
// not run reconciliation, issue acknowledgments, or contact the authority.
func (r *Restore) CleanupTarget(name string, b memory.RecoveryBinding) (memory.CleanupTarget, memory.ConsumerInspection, error) {
	fail := func(err error) (memory.CleanupTarget, memory.ConsumerInspection, error) {
		return memory.CleanupTarget{}, nil, err
	}
	if !label(name) || !utf8.ValidString(name) || !memory.ValidRecoveryScope(b.Scope) || b.Verifier == nil {
		return fail(memory.Invalid)
	}
	trust := b.Verifier.Configuration()
	if trust == ([32]byte{}) {
		return fail(memory.Invalid)
	}
	info, err := r.store.file.Stat()
	if err != nil {
		return fail(memory.Unavailable)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fail(memory.Unavailable)
	}
	encoded, err := json.Marshal(struct {
		Name          string
		Scope         memory.RecoveryScope
		Trust         [32]byte
		Device, Inode uint64
	}{name, b.Scope, trust, uint64(stat.Dev), stat.Ino})
	if err != nil {
		return fail(memory.Invalid)
	}
	config := sha256.Sum256(append([]byte("memory-restored-cleanup-target-v1\n"), encoded...))
	binding := memory.ConsumerBinding{Namespace: b.Scope.Namespace, Collection: b.Scope.Collection, Consumer: name, ConfigSHA256: fmt.Sprintf("%x", config)}
	inspector := &restoredCleanupInspection{restore: r, binding: binding, trust: fmt.Sprintf("%x", trust)}
	return memory.CleanupTarget{Name: name, Dimension: memory.DerivedCleanup, Binding: &binding}, inspector, nil
}

type restoredCleanupInspection struct {
	restore *Restore
	binding memory.ConsumerBinding
	trust   string
}

func (i *restoredCleanupInspection) InspectConsumer(ctx context.Context, b memory.ConsumerBinding) (uint64, error) {
	if b != i.binding {
		return 0, memory.IdentityConflict
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	state, err := recoveryStateAt(ctx, i.restore.store.db, memory.RecoveryScope{Namespace: b.Namespace, Collection: b.Collection})
	if err != nil {
		return 0, err
	}
	if state.Config != i.trust || state.Progress.State != "sanitized" || state.Progress.VerifiedPosition != state.Progress.OriginPosition {
		return 0, memory.Missing
	}
	return state.Progress.AppliedPosition, nil
}
