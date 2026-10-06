//go:build fault && darwin

package fault_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/egressio"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G5、G10、G11、R7、V4
func TestStoppedBackupRestoresUnknownFileWithOriginalManagedRoot(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "backup-original", "CREATE", "", []byte("original published bytes"))
	recorder := egressio.NewNativeFileRecorder(nil, nil)
	ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{FailBefore: "readback.object", Error: syscall.EIO, Recorder: recorder})
	receipt, err := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, receipt, err)
	original, err := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
	if err != nil || original.Effect.Outcome != "UNKNOWN" || original.Execution == nil {
		t.Fatalf("actual published unknown: %v %v", original, err)
	}
	raw := n.observation(t, a)
	if raw.FileEvidence == nil || !raw.FileEvidence.Published || raw.FileEvidence.ReadbackVerified || raw.FileEvidence.ErrorCode == "" {
		t.Fatalf("actual lost readback: %v", raw)
	}
	publications := 0
	for _, event := range recorder.Events() {
		if event.Kind == "rename" {
			publications++
		}
	}
	if publications != 1 {
		t.Fatalf("actual original publications=%d", publications)
	}
	pointer := readNativePointer(t, n.root)
	namespace := snapshotBackupFileNamespace(t, n.root)
	source, err := n.h.Budget.QueryBillingSource(n.ctx, n.caller, original.Execution.Send.Ref)
	if err != nil || source == nil || source.Amount == nil || *source.Amount != 0 {
		t.Fatalf("original FILE zero-fee responsibility: %v %v", source, err)
	}
	budget, err := n.h.Budget.QueryBudget(n.ctx, n.caller, a.TaskId)
	if err != nil {
		t.Fatal(err)
	}
	backupDir := t.TempDir()
	if _, err = n.h.CloseAndBackup(n.ctx, backupDir); err != nil {
		t.Fatal(err)
	}
	restoredHarness, err := assembly.RestoreBackupWithOptions(n.ctx, backupDir, t.TempDir(), "u", "d", assembly.Options{FileRoots: map[string]string{"documents": n.root}})
	if err != nil {
		t.Fatal(err)
	}
	n.h = restoredHarness
	restored, err := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
	if err != nil || !proto.Equal(restored, original) {
		t.Fatalf("restore changed original UNKNOWN/attempt/key/Execution: %v %v", restored, err)
	}
	restoredRaw := n.observation(t, a)
	if !proto.Equal(restoredRaw, raw) {
		t.Fatal("restore rewrote original failed readback")
	}
	restoredSource, err := n.h.Budget.QueryBillingSource(n.ctx, n.caller, original.Execution.Send.Ref)
	if err != nil || !proto.Equal(restoredSource, source) {
		t.Fatalf("restore changed fee source: %v %v", restoredSource, err)
	}
	restoredBudget, err := n.h.Budget.QueryBudget(n.ctx, n.caller, a.TaskId)
	if err != nil || !proto.Equal(restoredBudget, budget) {
		t.Fatalf("restore changed fees: %v %v", restoredBudget, err)
	}
	assertBackupFileNamespace(t, n.root, namespace)
	replay, err := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	if err != nil || !proto.Equal(replay, receipt) {
		t.Fatalf("restored original command replay: %v %v", replay, err)
	}
	assertBackupFileNamespace(t, n.root, namespace)
	// 显式独立查询验证选项确实连接原根；恢复本身没有自动查询或再次发布。
	reads := egressio.NewNativeFileRecorder(nil, nil)
	n.ctx = egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: reads})
	plan := requestNativeQuery(t, n, "backup-original-query", a)
	if plan.State != "COMPLETED" || plan.CheckCount != 1 {
		t.Fatalf("original root unavailable to explicit query: %v", plan)
	}
	after, err := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
	if err != nil || after.Effect.Outcome != "APPLIED" || !proto.Equal(after.Execution, original.Execution) {
		t.Fatalf("query replaced original execution: %v %v", after, err)
	}
	for _, event := range reads.Events() {
		if event.Kind == "write" || event.Kind == "rename" {
			t.Fatalf("query republished native bytes: %v", event)
		}
	}
	if !proto.Equal(pointer, readNativePointer(t, n.root)) {
		t.Fatal("restore/query rolled back or replaced original pointer")
	}
	assertBackupFileNamespace(t, n.root, namespace)
}

type backupFileEntry struct {
	info fs.FileInfo
	data []byte
}

func snapshotBackupFileNamespace(t *testing.T, root string) map[string]backupFileEntry {
	t.Helper()
	result := map[string]backupFileEntry{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var data []byte
		if !entry.IsDir() {
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[name] = backupFileEntry{info: info, data: data}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func assertBackupFileNamespace(t *testing.T, root string, before map[string]backupFileEntry) {
	t.Helper()
	after := snapshotBackupFileNamespace(t, root)
	if len(after) != len(before) {
		t.Fatalf("native namespace count changed: %d -> %d", len(before), len(after))
	}
	for path, old := range before {
		current, ok := after[path]
		if !ok || !os.SameFile(old.info, current.info) || !bytes.Equal(old.data, current.data) {
			t.Fatalf("original native name/identity/bytes changed: %s", path)
		}
	}
}
