package admission_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/backup"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G5、G10、G11、R7、V4
func TestLatestStoppedBackupPreservesUnknownAndOriginalSendAcrossRestore(t *testing.T) {
	target := simulator.NewBillingTarget(3)
	target.Target = simulator.New("queryable")
	target.Target.SetBehavior("accept-and-delay")
	target.DropReceipt(true)
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" {
		t.Fatalf("original unknown %v %v", original, e)
	}
	receipt, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("admit").Identity)
	if e != nil {
		t.Fatal(e)
	}
	body, e := f.h.Content.Read(f.ctx, f.caller, f.parameters)
	if e != nil {
		t.Fatal(e)
	}
	settings := f.h.StorageSettings()
	backupDir := t.TempDir()
	manifest, e := f.h.CloseAndBackup(f.ctx, backupDir)
	if e != nil {
		t.Fatal(e)
	}
	// 本 fixture 只有这个宿主；关闭后原来源永不恢复，独立目标保持原历史。
	stopped := stoppedFileSet(t, f.path)
	if manifest.UserID != "u" || manifest.DomainID != "d" || manifest.FormatDigest != settings.FormatDigest || manifest.Environment.Platform != settings.Platform || manifest.Environment.SQLiteSourceID != settings.SQLiteSourceID || len(manifest.Files) != 4 {
		t.Fatalf("actual identity/environment missing %v", manifest)
	}
	for _, file := range manifest.Files {
		suffix := file.Name[len("state.db"):]
		data, present := stopped[suffix]
		if file.Present != present || file.Size != int64(len(data)) || present && file.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
			t.Fatalf("manifest differs from actual stopped source %v", file)
		}
		if present {
			copied, err := os.ReadFile(filepath.Join(backupDir, file.Name))
			if err != nil || !bytes.Equal(copied, data) {
				t.Fatalf("copy differs from actual source %s %v", file.Name, err)
			}
		}
	}
	t.Logf("actual stopped environment %+v; present files %+v", manifest.Environment, manifest.Files)
	f.h, e = assembly.RestoreBackup(f.ctx, backupDir, t.TempDir(), "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	assertStoppedFileSet(t, f.path, stopped)
	restored, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(original, restored) {
		t.Fatalf("original responsibility changed %v %v", restored, e)
	}
	again, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, receipt.Receipt.Identity)
	if e != nil || !proto.Equal(receipt, again) {
		t.Fatalf("original receipt changed %v %v", again, e)
	}
	restoredBody, e := f.h.Content.Read(f.ctx, f.caller, f.parameters)
	if e != nil || !proto.Equal(body, restoredBody) {
		t.Fatalf("original content changed %v %v", restoredBody, e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || b.Reserved != 30 || b.Settled != 0 {
		t.Fatalf("original per-send fee responsibility changed %v %v", b, e)
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 1 || requests[0].Method != "POST" || len(effects) != 0 || len(target.Bills()) != 1 {
		t.Fatalf("restore added a business send or reset target %v %v", requests, effects)
	}
	// 原供应商账单迟到，不把费用结清误作效果确定。
	bill := stageBill(t, f, target.Bills()[0], "backup-late-bill")
	importCommand := &v1.ImportBillCommand{Header: header("backup-import-bill"), SendRef: original.Execution.Send.Ref, EvidenceRef: bill}
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, importCommand)
	accepted(t, r, e)
	firstBillReceipt := proto.Clone(r).(*v1.CommandReceipt)
	stillUnknown, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || stillUnknown.Effect.Outcome != "UNKNOWN" || !proto.Equal(original.Execution, stillUnknown.Execution) {
		t.Fatalf("late bill changed original effect/send %v %v", stillUnknown, e)
	}
	target.Target.ReleasePending()
	target.Target.SetBehavior("")
	target.DropReceipt(false)
	query := reconciliationCommand(f, a, cap, grant)
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, query)
	accepted(t, r, e)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	resolved, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || resolved.Effect.Outcome != "APPLIED" || !proto.Equal(original.Execution, resolved.Execution) {
		t.Fatalf("late original effect lost or resent %v %v", resolved, e)
	}
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, importCommand)
	if e != nil || !proto.Equal(firstBillReceipt, r) {
		t.Fatalf("original bill receipt replay changed %v %v", r, e)
	}
	duplicate := proto.Clone(importCommand).(*v1.ImportBillCommand)
	duplicate.Header = header("backup-duplicate-native-bill")
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, duplicate)
	accepted(t, r, e)
	b, e = f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	requests, effects = target.Target.Snapshot()
	if e != nil || b.Settled != 6 || b.Reserved != 0 || len(requests) != 2 || requests[0].Method != "POST" || requests[1].Method != "GET" || len(effects) != 1 || len(target.Bills()) != 2 {
		t.Fatalf("late per-send bill dedupe/target history %v %v %v %v", b, e, requests, effects)
	}
	assertStoppedFileSet(t, f.path, stopped)
}

func stoppedFileSet(t *testing.T, path string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte)
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		data, e := os.ReadFile(path + suffix)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		result[suffix] = data
	}
	return result
}

func assertStoppedFileSet(t *testing.T, path string, before map[string][]byte) {
	t.Helper()
	after := stoppedFileSet(t, path)
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		old, present := before[suffix]
		current, currentPresent := after[suffix]
		if present != currentPresent || !bytes.Equal(old, current) {
			t.Fatalf("stopped source changed %q", suffix)
		}
	}
}

// 规则：G3、G11、V4
func TestBackupManifestRejectsAmbiguousFieldsBeforeCreatingRestoreFiles(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	directory := t.TempDir()
	if _, e := f.h.CloseAndBackup(f.ctx, directory); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(directory, "manifest.json")
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	// JSON 默认的后一字段覆盖行为不能把未知清单版本掩盖成受支持版本。
	data = []byte(strings.Replace(string(data), `"version": "lerna.m1.stopped-backup.v1"`, `"version": "future-v2", "version": "lerna.m1.stopped-backup.v1"`, 1))
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	destination := t.TempDir()
	restored, e := assembly.RestoreBackup(f.ctx, directory, destination, "u", "d")
	if restored != nil {
		if closeErr := restored.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	if e == nil {
		t.Error("ambiguous manifest accepted")
	}
	entries, e := os.ReadDir(destination)
	if e != nil || len(entries) != 0 || f.calls.Load() != 0 {
		t.Fatalf("invalid manifest reached copy/Open %v %v calls=%d", entries, e, f.calls.Load())
	}
}

// 规则：G2、G3、G10、G11、完成-7、V4
func TestStoppedBackupKeepsFixedResultBytesWhileOriginalLateBillSettles(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	target.WithholdBill(true)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	scopeRequirement(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	proposal := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	begin := &v1.BeginCompletionCommand{Header: header("backup-fixed-result"), TaskId: f.task.Name, ProposalRef: proposal}
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, begin)
	accepted(t, r, e)
	originalReceipt := proto.Clone(r).(*v1.CommandReceipt)
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || result.UsageSnapshot.Reserved != 30 {
		t.Fatalf("actual fixed result missing %v %v", result, e)
	}
	frozen, e := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	if e != nil {
		t.Fatal(e)
	}
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	directory := t.TempDir()
	if _, e = f.h.CloseAndBackup(f.ctx, directory); e != nil {
		t.Fatal(e)
	}
	stopped := stoppedFileSet(t, f.path)
	f.h, e = assembly.RestoreBackup(f.ctx, directory, t.TempDir(), "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatalf("fixed-result restore added a business send %v %v", requests, effects)
	}
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, begin)
	if e != nil || !proto.Equal(originalReceipt, r) {
		t.Fatalf("original completion receipt changed %v %v", r, e)
	}
	bill := stageBill(t, f, target.Bills()[0], "backup-fixed-late-statement")
	command := &v1.ImportBillCommand{Header: header("backup-fixed-late-import"), SendRef: original.Execution.Send.Ref, EvidenceRef: bill}
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, command)
	accepted(t, r, e)
	command.Header = header("backup-fixed-duplicate-bill")
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, command)
	accepted(t, r, e)
	after, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	afterBytes, e := proto.MarshalOptions{Deterministic: true}.Marshal(after)
	if e != nil || !bytes.Equal(frozen, afterBytes) {
		t.Fatalf("late bill rewrote frozen result bytes %v %v", after, e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	requests, effects = target.Target.Snapshot()
	if e != nil || b.Settled != 25 || b.Reserved != 0 || len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatalf("late bill dedupe changed actual cost or target %v %v %v %v", b, e, requests, effects)
	}
	assertStoppedFileSet(t, f.path, stopped)
}

// 规则：G3、G11、V4
func TestBadStoppedBackupManifestAndNonemptyRestoreAreRejectedBeforeOpen(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	directory := t.TempDir()
	if _, e := f.h.CloseAndBackup(f.ctx, directory); e != nil {
		t.Fatal(e)
	}
	stopped := stoppedFileSet(t, f.path)
	for _, kind := range []string{"changed-main", "missing-main", "wrong-size", "wrong-sha", "missing-wal", "undeclared-shm", "future-manifest", "future-format", "future-contract", "wrong-format-digest", "wrong-user", "unknown-field", "duplicate-nested-field", "extra-file", "duplicate-file-name", "nonempty-restore"} {
		t.Run(kind, func(t *testing.T) {
			copyDir := t.TempDir()
			entries, e := os.ReadDir(directory)
			if e != nil {
				t.Fatal(e)
			}
			for _, entry := range entries {
				data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(copyDir, entry.Name()), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			manifestPath := filepath.Join(copyDir, "manifest.json")
			data, e := os.ReadFile(manifestPath)
			if e != nil {
				t.Fatal(e)
			}
			var m backup.Manifest
			if e = json.Unmarshal(data, &m); e != nil {
				t.Fatal(e)
			}
			destination := t.TempDir()
			switch kind {
			case "changed-main":
				main := filepath.Join(copyDir, "state.db")
				body, err := os.ReadFile(main)
				if err != nil {
					t.Fatal(err)
				}
				body[0] ^= 1
				e = os.WriteFile(main, body, 0600)
			case "missing-main":
				e = os.Remove(filepath.Join(copyDir, "state.db"))
			case "wrong-size":
				m.Files[0].Size++
			case "wrong-sha":
				m.Files[0].SHA256 = strings.Repeat("0", 64)
			case "missing-wal":
				m.Files[1].Present = true
				m.Files[1].Size = 1
				m.Files[1].SHA256 = strings.Repeat("0", 64)
			case "undeclared-shm":
				e = os.WriteFile(filepath.Join(copyDir, "state.db-shm"), []byte("undeclared"), 0600)
			case "future-manifest":
				m.Version = "future-v2"
			case "future-format":
				m.FormatVersion = 2
			case "future-contract":
				m.ContractVersion = 2
			case "wrong-format-digest":
				m.FormatDigest = strings.Repeat("0", 64)
			case "wrong-user":
				m.UserID = "other"
			case "extra-file":
				e = os.WriteFile(filepath.Join(copyDir, "extra.db"), []byte("unlisted"), 0600)
			case "duplicate-file-name":
				m.Files[1].Name = m.Files[0].Name
			case "nonempty-restore":
				e = os.WriteFile(filepath.Join(destination, "existing"), []byte("must remain"), 0600)
			}
			if e != nil {
				t.Fatal(e)
			}
			data, e = json.Marshal(m)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "unknown-field" {
				data = append([]byte(`{"unknown":true,`), data[1:]...)
			}
			if kind == "duplicate-nested-field" {
				data = []byte(strings.Replace(string(data), `"journal_mode":"wal"`, `"journal_mode":"delete","journal_mode":"wal"`, 1))
			}
			if e = os.WriteFile(manifestPath, data, 0600); e != nil {
				t.Fatal(e)
			}
			restored, e := assembly.RestoreBackup(f.ctx, copyDir, destination, "u", "d")
			if restored != nil {
				if closeErr := restored.Close(); closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			if e == nil {
				t.Fatal("invalid backup or nonempty target accepted")
			}
			entries, e = os.ReadDir(destination)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "nonempty-restore" {
				body, err := os.ReadFile(filepath.Join(destination, "existing"))
				if err != nil || string(body) != "must remain" || len(entries) != 1 {
					t.Fatalf("existing target overwritten %v %v", entries, err)
				}
			} else if len(entries) != 0 {
				t.Fatalf("bad manifest reached copy/Open %v", entries)
			}
			assertStoppedFileSet(t, f.path, stopped)
			if f.calls.Load() != 0 {
				t.Fatal("backup verification called target")
			}
		})
	}
}

// 规则：G1、G3、G10、G11、开始-5、V4
func TestStoppedBackupKeepsOpaqueEffectUnknownAndRefusesResend(t *testing.T) {
	target := simulator.NewBillingTarget(3)
	target.Target = simulator.New("opaque")
	target.DropReceipt(true)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	capability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	capability.Ref, capability.ApprovedBy = nil, nil
	capability.MaxSends = 2
	capability.AdapterRef.Name.LocalId = "simulator-opaque"
	r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("backup-opaque-capability"), Capability: capability})
	accepted(t, r, e)
	f.capability = r.ResultRef
	a, start := prepareStart(t, f)
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" {
		t.Fatalf("opaque original %v %v", original, e)
	}
	directory := t.TempDir()
	if _, e = f.h.CloseAndBackup(f.ctx, directory); e != nil {
		t.Fatal(e)
	}
	stopped := stoppedFileSet(t, f.path)
	f.h, e = assembly.RestoreBackup(f.ctx, directory, t.TempDir(), "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("backup-opaque-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "restored-worker"})
	accepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatal("original opaque responsibility missing")
	}
	r, e = f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("backup-opaque-resend"), OperationId: a.OperationId, PreviousSendRef: original.Execution.Send.Ref, Claim: claim.Jobs[0]})
	if e != nil || r.GetError().GetCode() != "RESEND_UNSAFE" {
		t.Fatalf("restored opaque resend was not refused %v %v", r, e)
	}
	bill := stageBill(t, f, target.Bills()[0], "backup-opaque-late-bill")
	r, e = f.h.Budget.ImportBill(f.ctx, f.caller, &v1.ImportBillCommand{Header: header("backup-opaque-import"), SendRef: original.Execution.Send.Ref, EvidenceRef: bill})
	accepted(t, r, e)
	after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(original, after) {
		t.Fatalf("restore/resend refusal/late bill changed opaque unknown %v %v", after, e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	requests, effects := target.Target.Snapshot()
	if e != nil || b.Settled != 3 || b.Reserved != 0 || len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatalf("opaque original external history/fee changed %v %v %v %v", b, e, requests, effects)
	}
	assertStoppedFileSet(t, f.path, stopped)
}
