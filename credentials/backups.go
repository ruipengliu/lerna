package credentials

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// BackupArchive stores immutable ciphertext snapshots. Delete leaves a durable
// tombstone, so a delayed Put cannot recreate disposed material.
type BackupArchive interface {
	ID() string
	Put(context.Context, string, []byte) (string, error)
	Check(context.Context, string, string) error
	Delete(context.Context, string, string) error
}
type Backup struct {
	OperationID, StorageID, ArchiveID, Digest, Phase string
	Binding                                          Binding
	Keys                                             []string
}
type BackupEntry struct {
	Backup  Backup
	Pending []Record
}
type Retirement struct {
	KeyVersion string
	Binding    Binding
	Phase      string
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func uniqueVersions(values []string) bool {
	if len(values) > 8 {
		return false
	}
	seen := map[string]bool{}
	for _, v := range values {
		if !name(v) || len(v) > 128 || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
func validBackupState(s LifecycleState) bool {
	if len(s.Backups) > 32 || len(s.Retirements) > 32 {
		return false
	}
	retired := map[string]bool{}
	for _, r := range s.Retirements {
		if !uniqueVersions([]string{r.KeyVersion}) || retired[r.KeyVersion] || !r.Binding.Valid() || (r.Phase != "pending" && r.Phase != "completed") {
			return false
		}
		retired[r.KeyVersion] = true
	}
	seen := map[string]bool{}
	pending := 0
	for _, entry := range s.Backups {
		b := entry.Backup
		if !validLifecycleOperation(b.OperationID) || !b.Binding.Valid() || !validLifecycleOperation(b.ArchiveID) || len(b.StorageID) != 64 || len(b.Digest) != 64 || seen[b.OperationID] || !uniqueVersions(b.Keys) {
			return false
		}
		seen[b.OperationID] = true
		switch b.Phase {
		case "pending":
			pending++
			if len(entry.Pending) > 64 {
				return false
			}
			for _, r := range entry.Pending {
				if !ValidRecord(r) || r.Binding != b.Binding || !contains(b.Keys, r.KeyVersion) {
					return false
				}
			}
		case "available", "disposing", "disposed":
			if len(entry.Pending) != 0 {
				return false
			}
		default:
			return false
		}
		if b.Phase != "disposed" {
			for _, v := range b.Keys {
				if retired[v] {
					return false
				}
			}
		}
	}
	return pending <= 1
}
func (l *Lifecycle) WithArchive(a BackupArchive) (*Lifecycle, error) {
	if a == nil || !validLifecycleOperation(a.ID()) {
		return nil, Invalid
	}
	copy := *l
	copy.archive = a
	return &copy, nil
}
func backupIndex(s LifecycleState, op string) int {
	for i, b := range s.Backups {
		if b.Backup.OperationID == op {
			return i
		}
	}
	return -1
}
func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func (l *Lifecycle) CreateBackup(ctx context.Context, token, operation string, target Binding) (Backup, error) {
	ctx, cancel := context.WithTimeout(ctx, l.broker.config.Timeout)
	defer cancel()
	if l.archive == nil || !validLifecycleOperation(operation) {
		return Backup{}, Invalid
	}
	if e := l.broker.authorize(ctx, token, target, "manage"); e != nil {
		return Backup{}, e
	}
	s, e := l.store.LoadLifecycle(ctx)
	if e != nil {
		return Backup{}, storeError(e)
	}
	i := backupIndex(s, operation)
	if i < 0 {
		if len(s.Backups) >= 32 {
			return Backup{}, Exhausted
		}
		for _, b := range s.Backups {
			if b.Backup.Phase == "pending" {
				return Backup{}, Conflict
			}
		}
		rows, err := l.store.List(ctx)
		if err != nil {
			return Backup{}, storeError(err)
		}
		entry := BackupEntry{Backup: Backup{OperationID: operation, Binding: target, ArchiveID: l.archive.ID(), Phase: "pending"}, Pending: []Record{}}
		for _, r := range rows {
			if r.Binding != target {
				continue
			}
			plaintext, err := l.broker.open(ctx, r)
			clear(plaintext)
			if err != nil {
				return Backup{}, err
			}
			entry.Pending = append(entry.Pending, r)
			if !contains(entry.Backup.Keys, r.KeyVersion) {
				entry.Backup.Keys = append(entry.Backup.Keys, r.KeyVersion)
			}
		}
		raw, err := json.Marshal(entry.Pending)
		if err != nil {
			return Backup{}, Invalid
		}
		entry.Backup.Digest = digest(raw)
		identity, _ := json.Marshal(struct {
			Operation, Archive string
			Binding            Binding
		}{operation, l.archive.ID(), target})
		entry.Backup.StorageID = digest(identity)
		s.Backups = append(s.Backups, entry)
		i = len(s.Backups) - 1
		if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
			return Backup{}, storeError(e)
		}
	}
	entry := s.Backups[i]
	b := entry.Backup
	if b.Binding != target || b.ArchiveID != l.archive.ID() {
		return Backup{}, Denied
	}
	if b.Phase == "disposing" || b.Phase == "disposed" {
		return Backup{}, Conflict
	}
	if b.Phase == "available" {
		if e = l.archive.Check(ctx, b.StorageID, b.Digest); e != nil {
			return Backup{}, Unavailable
		}
		return b, nil
	}
	raw, e := json.Marshal(entry.Pending)
	if e != nil || digest(raw) != b.Digest {
		return Backup{}, Unavailable
	}
	if e = l.broker.authorize(ctx, token, target, "manage"); e != nil {
		return Backup{}, e
	}
	actual, e := l.archive.Put(ctx, b.StorageID, raw)
	if e != nil || actual != b.Digest {
		return Backup{}, Unavailable
	}
	b.Phase = "available"
	s.Backups[i] = BackupEntry{Backup: b}
	if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
		return Backup{}, storeError(e)
	}
	return b, nil
}
func (l *Lifecycle) DisposeBackup(ctx context.Context, token, operation string, target Binding) (Backup, error) {
	ctx, cancel := context.WithTimeout(ctx, l.broker.config.Timeout)
	defer cancel()
	if l.archive == nil || !validLifecycleOperation(operation) {
		return Backup{}, Invalid
	}
	if e := l.broker.authorize(ctx, token, target, "manage"); e != nil {
		return Backup{}, e
	}
	s, e := l.store.LoadLifecycle(ctx)
	if e != nil {
		return Backup{}, storeError(e)
	}
	i := backupIndex(s, operation)
	if i < 0 {
		return Backup{}, Missing
	}
	b := s.Backups[i].Backup
	if b.Binding != target || b.ArchiveID != l.archive.ID() {
		return Backup{}, Denied
	}
	if b.Phase == "pending" {
		return Backup{}, Conflict
	}
	if b.Phase == "disposed" {
		return b, nil
	}
	if b.Phase == "available" {
		b.Phase = "disposing"
		s.Backups[i].Backup = b
		if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
			return Backup{}, storeError(e)
		}
	}
	if e = l.broker.authorize(ctx, token, target, "manage"); e != nil {
		return Backup{}, e
	}
	if e = l.archive.Delete(ctx, b.StorageID, b.Digest); e != nil {
		return Backup{}, Unavailable
	}
	b.Phase = "disposed"
	s.Backups[i].Backup = b
	if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
		return Backup{}, storeError(e)
	}
	return b, nil
}

// RetireKey first commits a database fence that excludes old ciphertext and
// live backup references. The material deletion can then be retried safely.
func (l *Lifecycle) RetireKey(ctx context.Context, token, version string, target Binding) error {
	ctx, cancel := context.WithTimeout(ctx, l.broker.config.Timeout)
	defer cancel()
	if !uniqueVersions([]string{version}) {
		return Invalid
	}
	if e := l.broker.authorize(ctx, token, target, "manage"); e != nil {
		return e
	}
	s, e := l.store.LoadLifecycle(ctx)
	if e != nil {
		return storeError(e)
	}
	idx := -1
	for i, r := range s.Retirements {
		if r.KeyVersion == version {
			if r.Binding != target {
				return Denied
			}
			if r.Phase == "completed" {
				return nil
			}
			idx = i
		}
	}
	if idx < 0 {
		for _, renewal := range s.Renewals {
			if renewal.Original != nil && renewal.Original.KeyVersion == version {
				return Denied
			}
		}

		permitted := false
		for _, r := range s.Rotations {
			if r.Phase != "completed" {
				return Conflict
			}
			if r.Binding == target && contains(r.SourceKeys, version) && r.KeyVersion != version {
				permitted = true
			}
		}
		if !permitted {
			return Denied
		}
		for _, b := range s.Backups {
			if b.Backup.Phase != "disposed" && contains(b.Backup.Keys, version) {
				return Denied
			}
		}
		if len(s.Retirements) >= 32 {
			return Exhausted
		}
		s.Retirements = append(s.Retirements, Retirement{KeyVersion: version, Binding: target, Phase: "pending"})
		idx = len(s.Retirements) - 1
		if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
			return storeError(e)
		}
	}
	if e = l.broker.authorize(ctx, token, target, "manage"); e != nil {
		return e
	}
	if e = l.keys.Retire(ctx, version); e != nil {
		return KeyUnavailable
	}
	s.Retirements[idx].Phase = "completed"
	if e = l.save(ctx, token, target, &s, nil, 0); e != nil {
		return storeError(e)
	}
	return nil
}
