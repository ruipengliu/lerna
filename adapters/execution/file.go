// Package execution 提供经明确目标合同约束的受控出口。
package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ruipengliu/lerna/api"
)

// ManagedFiles 只用于排除了旁路写者的稳定、单宿主受控根。
// os.Root 持有目录句柄；根重命名不会把请求重定向到另一目录。
type ManagedFiles struct {
	root      *os.Root
	ownerLock *os.File
	mu        sync.Mutex
	MaxBytes  int64
	Fault     func(string) error
}
type FileWrite struct {
	OperationID     string `json:"operation_id"`
	AttemptID       string `json:"attempt_id"`
	Path            string `json:"path"`
	ExpectedVersion string `json:"expected_version"`
	Data            []byte `json:"data"`
}
type FileObservation struct {
	Path       string `json:"path"`
	Version    string `json:"version"`
	Data       []byte `json:"data"`
	ObservedAt string `json:"observed_at"`
}
type FileReceipt struct {
	Effect          string `json:"effect"`
	MayApplyLater   bool   `json:"may_apply_later"`
	Version         string `json:"version"`
	JournalID       string `json:"journal_id"`
	DirectorySynced bool   `json:"directory_synced"`
}
type fileJournal struct {
	OperationID     string `json:"operation_id"`
	AttemptID       string `json:"attempt_id"`
	Path            string `json:"path"`
	ExpectedVersion string `json:"expected_version"`
	TempPath        string `json:"temp_path"`
	TempIdentity    string `json:"temp_identity"`
	AfterHash       string `json:"after_hash"`
	Phase           string `json:"phase"`
	DirectorySynced bool   `json:"directory_synced"`
}

func NewManagedFiles(root string) (*ManagedFiles, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	d := &ManagedFiles{root: r, MaxBytes: 8 << 20}
	if err = r.Mkdir(".harness", 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		r.Close()
		return nil, err
	}
	if err = r.Mkdir(".harness/journals", 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		r.Close()
		return nil, err
	}
	if err = d.safePath(".harness/journals", true); err != nil {
		r.Close()
		return nil, err
	}
	lock, err := r.OpenFile(".harness/owner.lock", os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		r.Close()
		return nil, err
	}
	if _, err = fileIdentity(lock); err == nil {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	if err != nil {
		lock.Close()
		r.Close()
		return nil, api.E("invalid_state", "file_owner_already_active")
	}
	d.ownerLock = lock
	if err = lock.Sync(); err == nil {
		err = d.syncDir(".harness/journals")
	}
	if err == nil {
		err = d.syncDir(".harness")
	}
	if err == nil {
		err = d.syncDir(".")
	}
	if err != nil {
		d.Close()
		return nil, api.E("unsupported", "directory_sync_not_verified")
	}
	return d, nil
}
func (d *ManagedFiles) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var err error
	if d.ownerLock != nil {
		err = d.ownerLock.Close()
		d.ownerLock = nil
	}
	return errors.Join(err, d.root.Close())
}
func normalizeFile(p string) (string, error) {
	if p == "" || strings.Contains(p, "\\") || strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") || path.Clean(p) != p || p == "." || strings.HasPrefix(p, "../") || p == ".." || strings.HasPrefix(p, ".harness/") || p == ".harness" {
		return "", api.E("forbidden", "path_outside_managed_root")
	}
	return p, nil
}
func fileIdentity(f *os.File) (string, error) {
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return "", api.E("unsupported", "file_identity_unavailable")
	}
	if s.Nlink != 1 {
		return "", api.E("forbidden", "hardlink_alias")
	}
	return fmt.Sprintf("%d:%d", s.Dev, s.Ino), nil
}
func (d *ManagedFiles) safePath(p string, dir bool) error {
	pieces := strings.Split(p, "/")
	for i := range pieces {
		prefix := strings.Join(pieces[:i+1], "/")
		st, err := d.root.Lstat(prefix)
		if errors.Is(err, fs.ErrNotExist) && i == len(pieces)-1 && !dir {
			return nil
		}
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return api.E("forbidden", "symlink_not_allowed")
		}
		if i < len(pieces)-1 || dir {
			if !st.IsDir() {
				return api.E("forbidden", "path_component_not_directory")
			}
		} else {
			if !st.Mode().IsRegular() {
				return api.E("forbidden", "target_not_regular")
			}
			s, ok := st.Sys().(*syscall.Stat_t)
			if !ok || s.Nlink != 1 {
				return api.E("forbidden", "hardlink_alias")
			}
		}
	}
	return nil
}
func (d *ManagedFiles) observe(p string) (FileObservation, error) {
	if err := d.safePath(p, false); err != nil {
		return FileObservation{}, err
	}
	f, err := d.root.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return FileObservation{Path: p, Version: "absent", Data: []byte{}, ObservedAt: api.Time(time.Now())}, nil
	}
	if err != nil {
		return FileObservation{}, err
	}
	defer f.Close()
	if _, err = fileIdentity(f); err != nil {
		return FileObservation{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, d.MaxBytes+1))
	if err != nil {
		return FileObservation{}, err
	}
	if int64(len(data)) > d.MaxBytes {
		return FileObservation{}, api.E("overloaded", "file_too_large")
	}
	return FileObservation{Path: p, Version: api.Hash(data), Data: data, ObservedAt: api.Time(time.Now())}, nil
}

// Read 每次打开真实介质，不使用写驱动缓存。
func (d *ManagedFiles) Read(ctx context.Context, p string) (FileObservation, error) {
	if err := ctx.Err(); err != nil {
		return FileObservation{}, err
	}
	p, err := normalizeFile(p)
	if err != nil {
		return FileObservation{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.observe(p)
}
func (d *ManagedFiles) syncDir(p string) error {
	f, err := d.root.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func (d *ManagedFiles) saveJournal(j fileJournal) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	name := ".harness/journals/" + j.AttemptID + ".json"
	tmp := name + ".next"
	f, err := d.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	if _, err = fileIdentity(f); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err == nil {
		err = closed
	}
	if err != nil {
		return err
	}
	if err = d.root.Rename(tmp, name); err != nil {
		return err
	}
	return d.syncDir(".harness/journals")
}
func (d *ManagedFiles) loadJournal(id string) (fileJournal, error) {
	var j fileJournal
	f, err := d.root.OpenFile(".harness/journals/"+id+".json", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return j, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil {
		return j, err
	}
	err = api.Decode(b, &j)
	return j, err
}
func (d *ManagedFiles) fault(stage string) error {
	if d.Fault != nil {
		return d.Fault(stage)
	}
	return nil
}
func (d *ManagedFiles) Write(ctx context.Context, q FileWrite) (FileReceipt, error) {
	if err := ctx.Err(); err != nil {
		return FileReceipt{}, err
	}
	p, err := normalizeFile(q.Path)
	if err != nil {
		return FileReceipt{}, err
	}
	if !api.ValidID(q.OperationID) || !api.ValidID(q.AttemptID) || q.ExpectedVersion == "" || int64(len(q.Data)) > d.MaxBytes {
		return FileReceipt{}, api.E("invalid_request", "invalid_file_write")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if old, err := d.loadJournal(q.AttemptID); err == nil {
		if old.OperationID != q.OperationID || old.Path != p || old.ExpectedVersion != q.ExpectedVersion || old.AfterHash != api.Hash(q.Data) {
			return FileReceipt{}, api.E("idempotency_conflict", "file_intent_changed")
		}
		return d.recoverLocked(old)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return FileReceipt{}, err
	}
	before, err := d.observe(p)
	if err != nil {
		return FileReceipt{}, err
	}
	if before.Version != q.ExpectedVersion {
		return FileReceipt{}, api.E("revision_conflict", "file_version_changed")
	}
	tmp := path.Join(path.Dir(p), ".harness-"+q.AttemptID+".tmp")
	f, err := d.root.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return FileReceipt{}, err
	}
	ident, err := fileIdentity(f)
	if err != nil {
		f.Close()
		return FileReceipt{}, err
	}
	j := fileJournal{OperationID: q.OperationID, AttemptID: q.AttemptID, Path: p, ExpectedVersion: q.ExpectedVersion, TempPath: tmp, TempIdentity: ident, AfterHash: api.Hash(q.Data), Phase: "prepared"}
	if err = d.saveJournal(j); err != nil {
		f.Close()
		return FileReceipt{}, err
	}
	if _, err = f.Write(q.Data); err == nil {
		err = f.Sync()
	}
	if err == nil {
		_, err = f.Seek(0, 0)
	}
	var actual []byte
	if err == nil {
		actual, err = io.ReadAll(io.LimitReader(f, d.MaxBytes+1))
	}
	if err == nil && api.Hash(actual) != j.AfterHash {
		err = api.E("invalid_state", "temp_bytes_changed")
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return FileReceipt{}, err
	}
	if err = d.syncDir(path.Dir(p)); err != nil {
		return FileReceipt{}, err
	}
	j.Phase = "temp_synced"
	if err = d.saveJournal(j); err != nil {
		return FileReceipt{}, err
	}
	if err = d.fault("temp_synced"); err != nil {
		return FileReceipt{}, err
	}
	before, err = d.observe(p)
	if err != nil {
		return FileReceipt{}, err
	}
	if before.Version != q.ExpectedVersion {
		return FileReceipt{}, api.E("revision_conflict", "file_version_changed")
	}
	if err = d.root.Rename(tmp, p); err != nil {
		return FileReceipt{}, err
	}
	if err = d.fault("renamed"); err != nil {
		return FileReceipt{}, err
	}
	if err = d.syncDir(path.Dir(p)); err != nil {
		return FileReceipt{}, err
	}
	j.Phase = "replaced"
	j.DirectorySynced = true
	if err = d.saveJournal(j); err != nil {
		return FileReceipt{}, err
	}
	if err = d.fault("replaced"); err != nil {
		return FileReceipt{}, err
	}
	return d.recoverLocked(j)
}

// Recover 无 journal 不推断成功；摘要必须与原临时 inode/保留阶段共同关联。
func (d *ManagedFiles) Recover(ctx context.Context, attemptID string) (FileReceipt, error) {
	if err := ctx.Err(); err != nil {
		return FileReceipt{}, err
	}
	if !api.ValidID(attemptID) {
		return FileReceipt{}, api.E("invalid_request", "invalid_attempt_id")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	j, err := d.loadJournal(attemptID)
	if err != nil {
		return FileReceipt{}, err
	}
	return d.recoverLocked(j)
}
func (d *ManagedFiles) recoverLocked(j fileJournal) (FileReceipt, error) {
	result := FileReceipt{Effect: "unknown", MayApplyLater: true, Version: j.AfterHash, JournalID: j.AttemptID, DirectorySynced: j.DirectorySynced}
	if j.Phase == "verified" || j.Phase == "replaced" {
		result.Effect = "applied"
		result.MayApplyLater = false
		return result, nil
	}
	f, err := d.root.OpenFile(j.Path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err == nil {
		ident, e := fileIdentity(f)
		var b []byte
		if e == nil {
			b, e = io.ReadAll(io.LimitReader(f, d.MaxBytes+1))
		}
		f.Close()
		if e == nil && ident == j.TempIdentity && api.Hash(b) == j.AfterHash {
			if e = d.syncDir(path.Dir(j.Path)); e != nil {
				return result, e
			}
			j.Phase = "verified"
			j.DirectorySynced = true
			if e = d.saveJournal(j); e != nil {
				return result, e
			}
			result.Effect = "applied"
			result.MayApplyLater = false
			result.DirectorySynced = true
			return result, nil
		}
	}
	// 尚存准确临时文件且前版本未变，只证明这次驱动未替换。恢复不自动 rename。
	f, err = d.root.OpenFile(j.TempPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err == nil {
		ident, e := fileIdentity(f)
		f.Close()
		before, e2 := d.observe(j.Path)
		if e == nil && e2 == nil && ident == j.TempIdentity && before.Version == j.ExpectedVersion {
			result.Effect = "not_applied"
			result.MayApplyLater = false
			return result, nil
		}
	}
	return result, nil
}
