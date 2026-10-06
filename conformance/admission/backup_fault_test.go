//go:build fault

package admission_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/backup"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

type stoppedProducerFacts struct {
	Settings sqlite.Settings
	Receipt  []byte
	Content  []byte
}

// 规则：G3、G11、R7、V4
func TestStoppedBackupCopiesActualQuiescentWALAndSHMBeforeAnySourceOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	factsPath := filepath.Join(t.TempDir(), "observed.json")
	child := exec.Command(os.Args[0], "-test.run=^TestStoppedWALBackupProducerChild$")
	child.Env = append(os.Environ(), "LERNA_STOPPED_BACKUP_SOURCE="+path, "LERNA_STOPPED_BACKUP_FACTS="+factsPath)
	output, e := child.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(e, &exited) || exited.ExitCode() != 86 {
		t.Fatalf("actual stopped child %v %s", e, output)
	}
	// 只有该子进程写入；已退出且绝不恢复原来源，父进程也不在原文件组上打开 SQLite。
	stopped := stoppedFileSet(t, path)
	if len(stopped["-wal"]) == 0 || len(stopped["-shm"]) == 0 {
		t.Fatalf("producer did not retain actual WAL/SHM %v", stopped)
	}
	data, e := os.ReadFile(factsPath)
	if e != nil {
		t.Fatal(e)
	}
	var facts stoppedProducerFacts
	if e = json.Unmarshal(data, &facts); e != nil {
		t.Fatal(e)
	}
	s := facts.Settings
	directory := t.TempDir()
	m, e := backup.CreateStopped(context.Background(), path, directory, backup.Manifest{
		UserID: "u", DomainID: "d", FormatVersion: s.FormatVersion, ContractVersion: s.ContractVersion,
		FormatDigest: s.FormatDigest, ImplementationProfile: s.ImplementationProfile,
		Environment: backup.Environment{Platform: s.Platform, SQLiteVersion: s.SQLiteVersion, SQLiteSourceID: s.SQLiteSourceID, SQLiteCompileOptionsHash: s.SQLiteCompileOptionsHash, JournalMode: s.JournalMode, Synchronous: s.Synchronous, FullFSync: s.FullFSync, DurabilityProfile: s.DurabilityProfile, PowerLossQualified: s.PowerLossQualified},
	})
	if e != nil || !m.Files[1].Present || !m.Files[2].Present || m.Files[1].Size != int64(len(stopped["-wal"])) || m.Files[2].Size != int64(len(stopped["-shm"])) {
		t.Fatalf("actual sidecars omitted %v %v", m, e)
	}
	for _, file := range m.Files {
		if !file.Present {
			continue
		}
		copied, err := os.ReadFile(filepath.Join(directory, file.Name))
		if err != nil || !bytes.Equal(copied, stopped[file.Name[len("state.db"):]]) {
			t.Fatalf("copied actual sidecar differs %v %v", file, err)
		}
	}
	t.Logf("actual stopped file group %+v; platform %+v", m.Files, m.Environment)
	h, e := assembly.RestoreBackup(context.Background(), directory, t.TempDir(), "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	q, e := h.Durable.QueryReceipt(context.Background(), caller, header("backup-wal-goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := proto.MarshalOptions{Deterministic: true}.Marshal(q)
	if e != nil || !bytes.Equal(encoded, facts.Receipt) {
		t.Fatalf("WAL owner receipt lost %v %v", q, e)
	}
	body, e := h.Content.Read(context.Background(), caller, q.Receipt.InputRef)
	if e != nil {
		t.Fatal(e)
	}
	encoded, e = proto.MarshalOptions{Deterministic: true}.Marshal(body)
	if e != nil || !bytes.Equal(encoded, facts.Content) {
		t.Fatalf("WAL original content lost %v %v", body, e)
	}
	assertStoppedFileSet(t, path, stopped)
	// 这是清单中确实存在且承载已确认事实的 WAL，不从清洁关闭 fixture 构造缺失声明。
	if e = os.Remove(filepath.Join(directory, "state.db-wal")); e != nil {
		t.Fatal(e)
	}
	destination := t.TempDir()
	missing, e := assembly.RestoreBackup(context.Background(), directory, destination, "u", "d")
	if missing != nil {
		if closeErr := missing.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	if e == nil {
		t.Fatal("missing actual necessary WAL accepted")
	}
	entries, e := os.ReadDir(destination)
	if e != nil || len(entries) != 0 {
		t.Fatalf("missing actual WAL reached copy/Open %v %v", entries, e)
	}
	assertStoppedFileSet(t, path, stopped)
}

// 规则：G3、G11、V4
func TestStoppedWALBackupProducerChild(t *testing.T) {
	path := os.Getenv("LERNA_STOPPED_BACKUP_SOURCE")
	if path == "" {
		t.Skip("isolated stopped producer only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	c := &v1.SubmitGoalCommand{Identity: header("backup-wal-goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "keep the original accepted WAL history"}
	if _, e = h.Sessions.SubmitGoal(ctx, caller, c); e != nil {
		t.Fatal(e)
	}
	if e = h.Sessions.ProcessPending(ctx, caller); e != nil {
		t.Fatal(e)
	}
	q, e := h.Durable.QueryReceipt(ctx, caller, c.Identity)
	if e != nil || q.GetReceipt().GetTaskRef() == nil {
		t.Fatalf("actual durable producer %v %v", q, e)
	}
	body, e := h.Content.Read(ctx, caller, q.Receipt.InputRef)
	if e != nil {
		t.Fatal(e)
	}
	facts := stoppedProducerFacts{Settings: h.StorageSettings()}
	facts.Receipt, e = proto.MarshalOptions{Deterministic: true}.Marshal(q)
	if e != nil {
		t.Fatal(e)
	}
	facts.Content, e = proto.MarshalOptions{Deterministic: true}.Marshal(body)
	if e != nil {
		t.Fatal(e)
	}
	data, e := json.Marshal(facts)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(os.Getenv("LERNA_STOPPED_BACKUP_FACTS"), data, 0600); e != nil {
		t.Fatal(e)
	}
	// 不调用 Close；OS 关闭全部写者，留下真实 WAL/SHM，父进程等待退出后复制。
	os.Exit(86)
}
