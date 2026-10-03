package wasi

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/ruipengliu/lerna/api"
)

const maxJournals = 10000

// 每个原 Attempt 的物理入口至多一次。日志在入口前耐久，恢复从不执行用户模块。
type runRecord struct {
	AttemptID       string       `json:"attempt_id"`
	BindingDigest   string       `json:"binding_digest"`
	Phase           string       `json:"phase"`
	PID             int          `json:"pid"`
	ProcessStart    string       `json:"process_start"`
	SpawnCount      uint64       `json:"spawn_count"`
	SpawnCountKnown bool         `json:"spawn_count_known"`
	Result          WorkerResult `json:"result"`
	Usage           []api.Amount `json:"usage"`
	UsageFinal      bool         `json:"usage_final"`
}

type Receipt struct {
	AttemptID       string           `json:"attempt_id"`
	OperationID     string           `json:"operation_id"`
	BindingDigest   string           `json:"binding_digest"`
	InstallLock     api.ComponentRef `json:"install_lock"`
	Phase           string           `json:"phase"`
	SpawnCount      uint64           `json:"spawn_count"`
	SpawnCountKnown bool             `json:"spawn_count_known"`
	Reason          string           `json:"reason"`
	OutputHash      string           `json:"output_hash"`
	ActuallyExited  bool             `json:"actually_exited"`
	Usage           []api.Amount     `json:"usage"`
	UsageFinal      bool             `json:"usage_final"`
}

func (r *Runtime) openJournal() error {
	dir, err := r.root.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := dir.ReadDir(maxJournals + 102)
	closeErr := dir.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(entries) > maxJournals+100 {
		return api.E("overloaded", "wasi_journal_capacity_exhausted")
	}
	for _, entry := range entries {
		if entry.Name() == "ownership.lock" || entry.Name() == "manifest.json" {
			continue
		}
		if !entry.Type().IsRegular() {
			return api.E("invalid_state", "wasi_journal_entry_not_regular")
		}
		if strings.HasSuffix(entry.Name(), ".json") {
			if !api.ValidID(strings.TrimSuffix(entry.Name(), ".json")) {
				return api.E("invalid_state", "wasi_journal_identity_invalid")
			}
			r.journalCount++
		} else if !strings.Contains(entry.Name(), ".tmp.") {
			return api.E("invalid_state", "wasi_journal_entry_unknown")
		}
	}
	var old Manifest
	err = r.readJSON("manifest.json", &old)
	if err == nil {
		if !api.Equal(old, r.manifest) {
			return api.E("unsupported", "wasi_runtime_lock_changed")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if r.journalCount != 0 {
		return api.E("invalid_state", "wasi_manifest_missing")
	}
	return r.writeJSON("manifest.json", r.manifest)
}

func (r *Runtime) readJSON(name string, into any) error {
	f, err := openPrivateFile(r.root, name, os.O_RDONLY)
	if err != nil {
		return err
	}
	b, readErr := io.ReadAll(io.LimitReader(f, api.MaxJSONBytes+1))
	closeErr := f.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	return api.Decode(b, into)
}

func (r *Runtime) writeJSON(name string, value any) error {
	b := api.Raw(value)
	if _, err := api.ParseJSON(b); err != nil {
		return err
	}
	tmp := name + ".tmp." + api.NewID("write")
	f, err := openPrivateFile(r.root, tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer r.root.Remove(tmp)
	_, writeErr := f.Write(b)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err = r.root.Rename(tmp, name); err != nil {
		return err
	}
	if r.cfg.JournalFault != nil {
		if err = r.cfg.JournalFault(AfterJournalRename, name); err != nil {
			return err
		}
	}
	dir, err := r.root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func (r *Runtime) record(attempt, digest string) (runRecord, error) {
	r.journalMu.Lock()
	defer r.journalMu.Unlock()
	var record runRecord
	if err := r.readJSON(attempt+".json", &record); err != nil {
		return record, err
	}
	if record.AttemptID != attempt || record.BindingDigest != digest || record.SpawnCount > 1 || record.Phase != "started" && record.Phase != "finished" && record.Phase != "lost" || record.Result.Protocol != WorkerProtocol || api.ValidateAmounts(record.Usage) != nil {
		return runRecord{}, api.E("invalid_state", "wasi_journal_binding_changed")
	}
	return record, nil
}
func (r *Runtime) createRecord(record runRecord) error {
	r.journalMu.Lock()
	defer r.journalMu.Unlock()
	var old runRecord
	err := r.readJSON(record.AttemptID+".json", &old)
	if err == nil {
		return api.E("invalid_state", "wasi_attempt_already_started")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if r.journalCount >= maxJournals {
		return api.E("overloaded", "wasi_journal_capacity_exhausted")
	}
	// rename 后的目录同步失败可能已经留下原责任；写入前保守占额，未知不释放。
	// 即使明确早期失败，本进程也可提前耗尽；重开只沿原目录实际日志重新计数。
	r.journalCount++
	if err = r.writeJSON(record.AttemptID+".json", record); err != nil {
		return err
	}
	return nil
}
func (r *Runtime) updateRecord(record runRecord) error {
	r.journalMu.Lock()
	defer r.journalMu.Unlock()
	var old runRecord
	if err := r.readJSON(record.AttemptID+".json", &old); err != nil {
		return err
	}
	if old.AttemptID != record.AttemptID || old.BindingDigest != record.BindingDigest || old.Phase != "started" {
		return api.E("invalid_state", "wasi_journal_not_mutable")
	}
	return r.writeJSON(record.AttemptID+".json", record)
}
