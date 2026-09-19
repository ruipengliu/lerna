package sqlite

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"sort"
	"time"

	"lerna/memory"
)

// ReadView adapts a quarantined collection to the existing governed Memory
// Reader. The host must use current authorization and residency configuration,
// independently of the backup. This view grants no mutation or runtime role.
func (r *Restore) ReadView(b memory.RecoveryBinding) (memory.QueryStore, error) {
	if !memory.ValidRecoveryScope(b.Scope) || b.Source == nil || b.Verifier == nil || b.Verifier.Configuration() == ([32]byte{}) {
		return nil, memory.Invalid
	}
	return &restoredReadView{restore: r, binding: b}, nil
}

type restoredReadView struct {
	restore *Restore
	binding memory.RecoveryBinding
}

func (v *restoredReadView) Read(ctx context.Context, ref memory.Ref, revision uint64) (memory.Revision, error) {
	return v.restore.ReadVerified(ctx, v.binding, memory.VersionRef{Ref: ref, Revision: revision})
}
func (v *restoredReadView) Commit(context.Context, memory.Change) (memory.Receipt, error) {
	return memory.Receipt{}, memory.Quarantined
}
func (v *restoredReadView) LookupOperation(context.Context, string, string) (memory.Receipt, error) {
	return memory.Receipt{}, memory.Quarantined
}
func (v *restoredReadView) ReadChanges(context.Context, string, string, uint64, int) ([]memory.Receipt, error) {
	return nil, memory.Quarantined
}

func (v *restoredReadView) current(ctx context.Context) (memory.RecoveryProof, error) {
	progress, proof, err := v.restore.reconcile(ctx, v.binding)
	if err != nil {
		return memory.RecoveryProof{}, err
	}
	if progress.State != "sanitized" {
		return memory.RecoveryProof{}, memory.Quarantined
	}
	return proof, nil
}

func recoveryHeads(s memory.RecoverySnapshot) map[memory.Ref]memory.RecoveryRecord {
	out := map[memory.Ref]memory.RecoveryRecord{}
	for _, record := range s.Records {
		if record.Version.Revision > out[record.Version.Ref].Version.Revision {
			out[record.Version.Ref] = record
		}
	}
	for _, event := range s.Erased {
		if event.Revision >= out[event.Ref].Version.Revision {
			delete(out, event.Ref)
		}
	}
	return out
}

func (v *restoredReadView) Scan(ctx context.Context, namespace, collection string) ([]memory.Revision, error) {
	if namespace != v.binding.Scope.Namespace || collection != v.binding.Scope.Collection {
		return nil, memory.Denied
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	proof, err := v.current(ctx)
	if err != nil {
		return nil, err
	}
	heads := recoveryHeads(proof.Snapshot)
	rows := make([]memory.Revision, 0, len(heads))
	for ref, expected := range heads {
		row, e := v.restore.store.Read(ctx, ref, expected.Version.Revision)
		if e != nil {
			return nil, memory.Unavailable
		}
		if expected.SHA256 != fmt.Sprintf("%x", sha256.Sum256(row.Document)) {
			return nil, memory.IdentityConflict
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Ref.Key < rows[j].Ref.Key })
	latest, err := recoveryProof(ctx, v.binding, proof.Snapshot.Position)
	if err != nil {
		return nil, err
	}
	if !maps.Equal(heads, recoveryHeads(latest.Snapshot)) {
		return nil, memory.Conflict
	}
	return rows, nil
}

func (v *restoredReadView) ValidateVersions(ctx context.Context, refs []memory.VersionRef) error {
	if len(refs) > 34 {
		return memory.Invalid
	}
	for _, ref := range refs {
		if ref.Ref.Namespace != v.binding.Scope.Namespace || ref.Ref.Collection != v.binding.Scope.Collection || !validRef(ref.Ref) || ref.Revision == 0 || ref.Revision > 1<<32 {
			return memory.Invalid
		}
	}
	proof, err := v.current(ctx)
	if err != nil {
		return err
	}
	present := map[memory.VersionRef]bool{}
	for _, record := range proof.Snapshot.Records {
		present[record.Version] = true
	}
	for _, ref := range refs {
		if !present[ref] {
			return memory.Missing
		}
	}
	return v.restore.store.ValidateVersions(ctx, refs)
}

func (v *restoredReadView) checkBinding(b memory.ReadBinding) error {
	if b.Namespace != v.binding.Scope.Namespace {
		return memory.Denied
	}
	if !validBinding(b) {
		return memory.Invalid
	}
	witnesses, err := b.CoverageReferences()
	if err != nil {
		return err
	}
	for _, ref := range append(witnesses, b.Results...) {
		if ref.Ref.Namespace != v.binding.Scope.Namespace || ref.Ref.Collection != v.binding.Scope.Collection {
			return memory.Denied
		}
	}
	return nil
}
func (v *restoredReadView) LookupRead(ctx context.Context, namespace, id string) (memory.ReadBinding, error) {
	if namespace != v.binding.Scope.Namespace {
		return memory.ReadBinding{}, memory.Denied
	}
	out, err := v.restore.store.LookupRead(ctx, namespace, id)
	if err != nil {
		return memory.ReadBinding{}, err
	}
	if err = v.checkBinding(out); err != nil {
		return memory.ReadBinding{}, err
	}
	return out, nil
}
func (v *restoredReadView) BindRead(ctx context.Context, b memory.ReadBinding) (memory.ReadBinding, error) {
	if err := v.checkBinding(b); err != nil {
		return memory.ReadBinding{}, err
	}
	return v.restore.store.BindRead(ctx, b)
}
