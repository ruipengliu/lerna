//go:build darwin && cgo

package admission_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G3、G4、G5、G10、G11、R7、V4
func TestStoppedAPIBackupKeepsClosedUnknownAndOriginalNativeWorld(t *testing.T) {
	for _, outcome := range []string{"FAILED", "CANCELLED"} {
		t.Run(outcome, func(t *testing.T) {
			testAPIClosingOriginalHistory(t, outcome, false, func(f *fixture, options assembly.Options) { restoreStoppedNativeAPI(t, f, options) })
		})
	}
}

// 规则：G1、G3、G4、G5、G7、G10、G11、R7、V4
func TestStoppedHTTPSAPIBackupForwardsOriginalRootsForIndependentQuery(t *testing.T) {
	testAPIClosingOriginalHistory(t, "FAILED", true, func(f *fixture, options assembly.Options) { restoreStoppedNativeAPI(t, f, options) })
}

// restoreStoppedNativeAPI 停止唯一写者且永久不再打开原路径；目标和平台仓库仍是同一个外部世界。
func restoreStoppedNativeAPI(t *testing.T, f *fixture, options assembly.Options) {
	t.Helper()
	before, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil || before.Result == nil || len(before.Result.UnknownOperationRefs) != 1 {
		t.Fatalf("original CLOSED UNKNOWN: %v %v", before, e)
	}
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, before.Result.OperationRefs[0].Name)
	if e != nil || original.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("original native unknown: %v %v", original, e)
	}
	raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, original.Execution.Send.ObservationRef)
	if e != nil {
		t.Fatal(e)
	}
	plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, original.Ref.Name)
	if e != nil || plan.State != "PAUSED" || plan.CheckCount != 0 {
		t.Fatalf("original plan not stopped: %v %v", plan, e)
	}
	source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, original.Execution.Send.Ref)
	if e != nil || source.Status != "PENDING" {
		t.Fatalf("original fee source missing: %v %v", source, e)
	}
	identities := []*v1.CommandIdentity{header("goal").Identity, header("admit" + f.suffix).Identity, before.Closing.RequestedBy, original.Execution.Send.StartReceipt.Identity, plan.RequestedBy, before.ClosureIntents[0].Command.Header.Identity, {UserId: "u", IssuerId: "egress", TargetDomainId: "d/ledger", CommandId: "dispatch:" + original.Execution.Send.Ref.Name.LocalId}}
	if before.Closing.CancellationRef != nil {
		cancellation, e := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
		if e != nil {
			t.Fatal(e)
		}
		identities = append(identities, cancellation.ControlIdentity)
	}
	receipts := make([]*v1.ReceiptQuery, len(identities))
	for i, id := range identities {
		receipts[i] = originalAPIReceipt(t, f, id)
	}
	fixed, e := proto.Marshal(before.Result)
	if e != nil {
		t.Fatal(e)
	}
	sourcePath := f.path
	backupDirectory, restoreDirectory := t.TempDir(), t.TempDir()
	manifest, e := f.h.CloseAndBackup(f.ctx, backupDirectory)
	if e != nil {
		t.Fatal(e)
	}
	sourceFiles := apiStoppedPhysicalFiles(t, sourcePath)
	t.Cleanup(func() {
		after := apiStoppedPhysicalFiles(t, sourcePath)
		if len(after) != len(sourceFiles) {
			t.Error("stopped source file presence changed")
		}
		for name, digest := range sourceFiles {
			if after[name] != digest {
				t.Errorf("stopped original source changed: %s", name)
			}
		}
	})
	next, e := assembly.RestoreBackupWithOptions(f.ctx, backupDirectory, restoreDirectory, "u", "d", options)
	if e != nil {
		t.Fatal("same original native world restore failed", e)
	}
	f.h = next
	// 后续真实重启只打开恢复目录；绝不再打开已永久停机的原数据库。
	f.path = filepath.Join(restoreDirectory, "state.db")
	for i, id := range identities {
		if !proto.Equal(originalAPIReceipt(t, f, id), receipts[i]) {
			t.Errorf("restore changed original admission/start/dispatch/close/plan/ACK receipt: %s", id.CommandId)
		}
	}
	restored, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	body, e := proto.Marshal(restored.Result)
	if e != nil || string(body) != string(fixed) || !proto.Equal(restored.Closing, before.Closing) || !proto.Equal(restored.ClosureIntents[0], before.ClosureIntents[0]) || !proto.Equal(restored.ClosureSeals[0], before.ClosureSeals[0]) || !proto.Equal(restored.ExecutionFollowups[0], before.ExecutionFollowups[0]) || !proto.Equal(restored.SettlementFollowups[0], before.SettlementFollowups[0]) {
		t.Fatal("restore rewrote fixed result/source ACK/seals/followup identities", e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, original.Ref.Name)
	if e != nil || !proto.Equal(op, original) {
		t.Fatalf("restore changed original execution/binding: %v %v", op, e)
	}
	observed, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, raw.Ref)
	if e != nil || !proto.Equal(observed, raw) {
		t.Fatal("restore rewrote original lost receipt evidence", e)
	}
	actualPlan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, original.Ref.Name)
	if e != nil || !proto.Equal(actualPlan, plan) {
		t.Fatal("restore replaced or advanced paused original plan", e)
	}
	fee, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, original.Execution.Send.Ref)
	if e != nil || !proto.Equal(fee, source) {
		t.Fatal("restore changed original account/send fee source", e)
	}
	summary, e := json.Marshal(manifest)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("actual_stopped_source_never_reopened=true restore_empty_directory=true original_Keychain_and_TLS_roots_forwarded=true fixed_Result_bytes=%d original_target_after_restore=POST1_GET0_effect0_bill1 manifest=%s", len(fixed), summary)
}

func apiStoppedPhysicalFiles(t *testing.T, path string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		body, e := os.ReadFile(path + suffix)
		if os.IsNotExist(e) {
			result[suffix] = "ABSENT"
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		digest := sha256.Sum256(body)
		result[suffix] = hex.EncodeToString(digest[:])
	}
	return result
}
