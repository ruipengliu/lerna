package credentials

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// KeyLifecycle is privileged host composition; consumers of a Driver cannot
// prepare, activate, read or retire master-key material.
type KeyLifecycle interface {
	KeySource
	Active(context.Context) (string, error)
	Prepare(context.Context, string) (string, error)
	Activate(context.Context, string, string) error
	Retire(context.Context, string) error
}
type Rotation struct {
	OperationID string
	Binding     Binding
	KeyVersion  string
	Phase       string
	Completed   []string
	SourceKeys  []string
}
type LifecycleState struct {
	Revision    uint64
	Rotations   []Rotation
	Backups     []BackupEntry
	Retirements []Retirement
	Renewals    []RenewalEntry
}
type LifecycleChange struct {
	State          LifecycleState
	Record         *Record
	ExpectedRecord uint64
}

// LifecycleStore commits ciphertext and its corresponding progress together.
type LifecycleStore interface {
	Store
	LoadLifecycle(context.Context) (LifecycleState, error)
	CommitLifecycle(context.Context, uint64, LifecycleChange) error
}
type Lifecycle struct {
	store    LifecycleStore
	keys     KeyLifecycle
	broker   *Broker
	archive  BackupArchive
	provider RenewalProvider
}

func NewLifecycle(store LifecycleStore, keys KeyLifecycle, auth Authority, clock Clock, cfg Config) (*Lifecycle, error) {
	b, e := New(store, keys, auth, clock, cfg)
	if e != nil {
		return nil, e
	}
	return &Lifecycle{store: store, keys: keys, broker: b}, nil
}
func validLifecycleOperation(op string) bool {
	if len(op) == 0 || len(op) > 64 {
		return false
	}
	for _, c := range op {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func ValidLifecycleState(s LifecycleState) bool {
	if s.Revision >= 1<<32 || len(s.Rotations) > 32 || !validBackupState(s) || !validRenewalState(s) {
		return false
	}
	seen := map[string]bool{}
	active := 0
	for _, r := range s.Rotations {
		if !validLifecycleOperation(r.OperationID) || !r.Binding.Valid() || seen[r.OperationID] || len(r.Completed) > 64 {
			return false
		}
		seen[r.OperationID] = true
		switch r.Phase {
		case "pending":
			if len(r.Completed) != 0 {
				return false
			}
		case "active", "completed":
			if r.KeyVersion == "" {
				return false
			}
		default:
			return false
		}
		if !uniqueVersions(r.SourceKeys) || len(r.KeyVersion) > 128 {
			return false
		}
		if r.Phase != "completed" {
			active++
		}
		refs := map[string]bool{}
		for _, ref := range r.Completed {
			if !validRef(ref) || refs[ref] {
				return false
			}
			refs[ref] = true
		}
	}
	return active <= 1
}
func rotationIndex(s LifecycleState, op string) int {
	for i, r := range s.Rotations {
		if r.OperationID == op {
			return i
		}
	}
	return -1
}
func keyOperation(r Rotation) string {
	raw, _ := json.Marshal(struct {
		Operation string
		Binding   Binding
	}{r.OperationID, r.Binding})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (l *Lifecycle) save(ctx context.Context, token string, target Binding, s *LifecycleState, record *Record, expectedRecord uint64) error {
	if e := l.broker.authorize(ctx, token, target, "manage"); e != nil {
		return e
	}
	old := s.Revision
	s.Revision++
	return l.store.CommitLifecycle(ctx, old, LifecycleChange{State: *s, Record: record, ExpectedRecord: expectedRecord})
}
func (l *Lifecycle) StartRotation(ctx context.Context, token, operation string, target Binding) (Rotation, error) {
	ctx, cancel := context.WithTimeout(ctx, l.broker.config.Timeout)
	defer cancel()
	if !validLifecycleOperation(operation) {
		return Rotation{}, Invalid
	}
	if e := l.broker.authorize(ctx, token, target, "manage"); e != nil {
		return Rotation{}, e
	}
	s, e := l.store.LoadLifecycle(ctx)
	if e != nil {
		return Rotation{}, storeError(e)
	}
	if i := rotationIndex(s, operation); i >= 0 {
		if s.Rotations[i].Binding != target {
			return Rotation{}, Denied
		}
		return s.Rotations[i], nil
	}
	if len(s.Rotations) >= 32 {
		return Rotation{}, Exhausted
	}
	for _, r := range s.Rotations {
		if r.Phase != "completed" {
			return Rotation{}, Conflict
		}
	}
	r := Rotation{OperationID: operation, Binding: target, Phase: "pending", Completed: []string{}}
	s.Rotations = append(s.Rotations, r)
	if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
		return Rotation{}, storeError(e)
	}
	return r, nil
}

// StepRotation performs at most one record migration. Each retry loads the
// original journal; an unknown commit is reconciled instead of replaying a write.
func (l *Lifecycle) StepRotation(ctx context.Context, token, operation string, target Binding) (Rotation, error) {
	ctx, cancel := context.WithTimeout(ctx, l.broker.config.Timeout)
	defer cancel()
	if !validLifecycleOperation(operation) {
		return Rotation{}, Invalid
	}
	if e := l.broker.authorize(ctx, token, target, "manage"); e != nil {
		return Rotation{}, e
	}
	s, e := l.store.LoadLifecycle(ctx)
	if e != nil {
		return Rotation{}, storeError(e)
	}
	i := rotationIndex(s, operation)
	if i < 0 {
		return Rotation{}, Missing
	}
	r := s.Rotations[i]
	if r.Binding != target {
		return Rotation{}, Denied
	}
	if r.Phase == "completed" {
		return r, nil
	}
	if r.KeyVersion == "" {
		active, err := l.keys.Active(ctx)
		if err != nil {
			return Rotation{}, KeyUnavailable
		}
		if !contains(r.SourceKeys, active) {
			r.SourceKeys = append(r.SourceKeys, active)
		}
		rows, err := l.store.List(ctx)
		if err != nil {
			return Rotation{}, storeError(err)
		}
		for _, row := range rows {
			if row.Binding == target && !contains(r.SourceKeys, row.KeyVersion) {
				r.SourceKeys = append(r.SourceKeys, row.KeyVersion)
			}
		}

		r.KeyVersion, e = l.keys.Prepare(ctx, keyOperation(r))
		if e != nil {
			return Rotation{}, KeyUnavailable
		}
		material, err := l.keys.Read(ctx, r.KeyVersion)
		valid := err == nil && len(material) == 32
		clear(material)
		if !valid {
			return Rotation{}, KeyUnavailable
		}
		s.Rotations[i] = r
		if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
			return Rotation{}, storeError(e)
		}
	}
	if r.Phase == "pending" {
		r.Phase = "active"
		s.Rotations[i] = r
		// The database write fence is durable before the active material changes.
		if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
			return Rotation{}, storeError(e)
		}
	}
	if e = l.broker.authorize(ctx, token, target, "manage"); e != nil {
		return Rotation{}, e
	}
	if e = l.keys.Activate(ctx, keyOperation(r), r.KeyVersion); e != nil {
		return Rotation{}, KeyUnavailable
	}
	rows, e := l.store.List(ctx)
	if e != nil {
		return Rotation{}, storeError(e)
	}
	complete := []string{}
	for _, row := range rows {
		if row.Binding != target {
			continue
		}
		secret, err := l.broker.open(ctx, row)
		if err != nil {
			return Rotation{}, err
		}
		if row.KeyVersion == r.KeyVersion {
			clear(secret)
			complete = append(complete, row.Ref)
			continue
		}
		expected := row.Revision
		if expected >= 1<<32-1 {
			clear(secret)
			return Rotation{}, Exhausted
		}
		row.Revision++
		row, e = l.broker.seal(ctx, row, secret)
		clear(secret)
		if e != nil {
			return Rotation{}, e
		}
		if row.KeyVersion != r.KeyVersion {
			return Rotation{}, Conflict
		}
		found := false
		for _, ref := range r.Completed {
			found = found || ref == row.Ref
		}
		if !found {
			r.Completed = append(r.Completed, row.Ref)
		}
		s.Rotations[i] = r
		if e = l.save(ctx, token, target, &s, &row, expected); e != nil {
			return Rotation{}, storeError(e)
		}
		return r, nil
	}
	r.Completed = complete
	r.Phase = "completed"
	s.Rotations[i] = r
	if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
		return Rotation{}, storeError(e)
	}
	return r, nil
}
