// Package harness 提供保留原命令身份的 Go 客户端。
package harness

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"

	"github.com/ruipengliu/lerna/api"
)

type Entry struct {
	IdentityScope      string       `json:"identity_scope"`
	SchemaDigest       string       `json:"schema_digest"`
	MethodSchemaDigest string       `json:"method_schema_digest,omitempty"`
	Command            api.Command  `json:"command"`
	Digest             string       `json:"digest"`
	Receipt            *api.Receipt `json:"receipt,omitempty"`
}
type Journal interface {
	Save(context.Context, Entry) error
	Read(context.Context, string) (Entry, error)
	Pending(context.Context, int) ([]Entry, bool, error)
}
type FileJournal struct {
	root  *os.Root
	lock  *os.File
	mu    sync.Mutex
	scope string
}

const journalDirectoryLimit = 4096

func OpenJournal(path, identityScope string) (*FileJournal, error) {
	if identityScope == "" {
		return nil, api.E("invalid_request", "identity_scope_required")
	}
	if e := os.MkdirAll(path, 0700); e != nil {
		return nil, e
	}
	r, e := os.OpenRoot(path)
	if e != nil {
		return nil, e
	}
	lock, e := r.OpenFile(".journal.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		r.Close()
		return nil, e
	}
	return &FileJournal{root: r, lock: lock, scope: identityScope}, nil
}
func (j *FileJournal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return errors.Join(j.lock.Close(), j.root.Close())
}
func (j *FileJournal) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := syscall.Flock(int(j.lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return api.E("overloaded", "journal_in_use")
	}
	return nil
}
func (j *FileJournal) release() { _ = syscall.Flock(int(j.lock.Fd()), syscall.LOCK_UN) }
func (j *FileJournal) read(id string) (Entry, error) {
	if !api.ValidID(id) {
		return Entry{}, api.E("invalid_request", "invalid_command_id")
	}
	f, e := j.root.Open(id + ".json")
	if e != nil {
		return Entry{}, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if e != nil {
		return Entry{}, e
	}
	var entry Entry
	if e = api.DecodeLimit(b, &entry, 1<<20); e != nil {
		return Entry{}, e
	}
	if entry.IdentityScope != j.scope {
		return Entry{}, api.E("forbidden", "journal_identity_mismatch")
	}
	d, e := api.Digest(entry.Command)
	if e != nil || d != entry.Digest {
		return Entry{}, api.E("invalid_request", "journal_digest_mismatch")
	}
	return entry, nil
}
func (j *FileJournal) Save(ctx context.Context, entry Entry) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if entry.IdentityScope != j.scope || !api.ValidID(entry.Command.CommandID) {
		return api.E("forbidden", "journal_identity_mismatch")
	}
	digest, e := api.Digest(entry.Command)
	if e != nil || digest != entry.Digest {
		return api.E("invalid_request", "journal_digest_mismatch")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if e = j.acquire(ctx); e != nil {
		return e
	}
	defer j.release()
	old, e := j.read(entry.Command.CommandID)
	if e == nil {
		if old.Digest != entry.Digest || old.SchemaDigest != entry.SchemaDigest || old.MethodSchemaDigest != entry.MethodSchemaDigest {
			return api.E("idempotency_conflict", "journal_command_changed")
		}
		if old.Receipt != nil && old.Receipt.Stage != "accepted" && !api.Equal(old.Receipt, entry.Receipt) {
			return api.E("idempotency_conflict", "journal_decision_changed")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	} else if e = j.admitNew(); e != nil {
		return e
	}
	name := entry.Command.CommandID + ".json"
	tmp := entry.Command.CommandID + "." + api.NewID("write") + ".tmp"
	f, e := j.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	b := api.Raw(entry)
	if len(b) > 1<<20 {
		f.Close()
		j.root.Remove(tmp)
		return api.E("invalid_request", "journal_entry_too_large")
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		j.root.Remove(tmp)
		return e
	}
	if e = j.root.Rename(tmp, name); e != nil {
		j.root.Remove(tmp)
		return e
	}
	dir, e := j.root.Open(".")
	if e != nil {
		return e
	}
	e = dir.Sync()
	closeErr = dir.Close()
	if e == nil {
		e = closeErr
	}
	return e
}

// 在原 flock 内限制新责任；已存在 ID 的核对、回执更新与重传保留原容量。
func (j *FileJournal) admitNew() error {
	dir, err := j.root.Open(".")
	if err != nil {
		return err
	}
	names, err := dir.ReadDir(journalDirectoryLimit)
	if err == io.EOF {
		err = nil
	}
	if err = errors.Join(err, dir.Close()); err != nil {
		return err
	}
	if len(names) >= journalDirectoryLimit {
		return api.E("overloaded", "journal_capacity")
	}
	return nil
}
func (j *FileJournal) Read(ctx context.Context, id string) (Entry, error) {
	if e := ctx.Err(); e != nil {
		return Entry{}, e
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if e := j.acquire(ctx); e != nil {
		return Entry{}, e
	}
	defer j.release()
	return j.read(id)
}
func (j *FileJournal) Pending(ctx context.Context, max int) ([]Entry, bool, error) {
	if max < 1 || max > 256 {
		return nil, false, api.E("invalid_request", "invalid_journal_limit")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if e := j.acquire(ctx); e != nil {
		return nil, false, e
	}
	defer j.release()
	dir, e := j.root.Open(".")
	if e != nil {
		return nil, false, e
	}
	defer dir.Close()
	names, e := dir.ReadDir(journalDirectoryLimit + 1)
	if e != nil && e != io.EOF {
		return nil, false, e
	}
	if len(names) > journalDirectoryLimit {
		return nil, false, api.E("overloaded", "journal_scan_limit")
	}
	sort.Slice(names, func(i, k int) bool { return names[i].Name() < names[k].Name() })
	result := []Entry{}
	for _, name := range names {
		if e := ctx.Err(); e != nil {
			return nil, false, e
		}
		if name.IsDir() || filepath.Ext(name.Name()) != ".json" {
			continue
		}
		entry, e := j.read(name.Name()[:len(name.Name())-5])
		if e != nil {
			return nil, false, e
		}
		if entry.Receipt == nil || entry.Receipt.Stage == "accepted" {
			if len(result) == max {
				return result, true, nil
			}
			result = append(result, entry)
		}
	}
	return result, false, nil
}

var _ Journal = (*FileJournal)(nil)
