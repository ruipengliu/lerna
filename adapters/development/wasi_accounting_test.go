package development

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/wasi"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/runtime"
)

// 观察端口只控制测试收束时机；事实仍来自原 App worker、公开方法和真实日志。
type wasiWithdrawalObserver struct {
	BeforeCancellation func(context.Context, *App, string, api.ContentRef) error
	AfterCancellation  func(context.Context, *App, string, api.ContentRef) error
}

func TestOriginalKnownWASICPUInvoiceRecoversAfterClosedCodeWithoutTargetOwnership(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("requires actual PostgreSQL")
			}
			var before execution.OperationView
			var nativeHash string
			var invoke execution.InvokeInput
			observer := wasiWithdrawalObserver{
				BeforeCancellation: func(ctx context.Context, a *App, taskID string, code api.ContentRef) error {
					facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
					if err != nil {
						return err
					}
					if len(facts.Operations) != 1 || !api.Equal(facts.Operations[0].Intent.CapabilityRef, execution.WASIRunCellCapability().Ref) {
						return fmt.Errorf("original Cell is not the unique admitted operation")
					}
					before, err = originalWASIInvoiceOperation(ctx, a, facts.Operations[0].Intent.OperationID)
					if err != nil {
						return err
					}
					command, err := a.Store.LookupCommand(ctx, a.Scope, facts.Operations[0].Intent.CommandID)
					if err != nil {
						return err
					}
					if err = api.Decode(command.Command.Payload, &invoke); err != nil {
						return err
					}
					if before.Operation.Effect != "applied" || !before.NewAttemptsClosed || !before.ActuallyStopped || !before.Operation.UsageFinal || len(before.Attempts.Items) != 1 || len(before.Operation.Usage) != 1 || before.Operation.Usage[0].Unit != "cpu_seconds" {
						return fmt.Errorf("closed Code must follow the original applied, exited, known-final Cell")
					}
					nativeHash = assertOriginalWASIJournal(t, a.Config.DataRoot, before)
					assertWASISourceClosed(t, ctx, a, code, "environment_code")
					t.Logf("WASI_MINIMUM_INVOICE_ORIGINAL scope=%s operation=%s revision=%d attempt=%s cpu=%s native_hash=%s", api.Raw(a.Scope), before.Operation.OperationID, before.Operation.Revision, before.Attempts.Items[0].AttemptID, before.Operation.Usage[0].Value, nativeHash)
					return nil
				},
				AfterCancellation: func(ctx context.Context, a *App, taskID string, code api.ContentRef) error {
					// 取消已关闭的操作可以幂等不升版；账单仍取原操作当前准确事实。
					actual, err := originalWASIInvoiceOperation(ctx, a, before.Operation.OperationID)
					if err != nil {
						return err
					}
					if !actual.NewAttemptsClosed || !actual.ActuallyStopped || !actual.Operation.UsageFinal || actual.Operation.OperationID != before.Operation.OperationID || !api.Equal(actual.Operation.Usage, before.Operation.Usage) || len(actual.Attempts.Items) != 1 || actual.Attempts.Items[0].AttemptID != before.Attempts.Items[0].AttemptID {
						return fmt.Errorf("original cancellation changed known final Cell facts")
					}
					gateway, err := OpenAppForRole(ctx, a.Config, false, "gateway")
					if err != nil {
						return err
					}
					defer func() {
						if err := gateway.Close(); err != nil {
							t.Error(err)
						}
					}()
					if gateway.OwnsTargets {
						return fmt.Errorf("minimum invoice recovery acquired physical targets")
					}
					if err = checkOriginalWASIInvoiceRefusals(t, ctx, gateway, actual, invoke); err != nil {
						return err
					}
					query := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: gateway.Scope.OwnerID, QueryID: api.NewID("query"), Method: "execution.usage.get", TargetID: before.Operation.OperationID, Payload: api.Raw(execution.OperationIDInput{OperationID: before.Operation.OperationID})}
					raw, err := gateway.Dispatcher.Query(ctx, gateway.ServiceAuth, api.Raw(query))
					if err != nil {
						return fmt.Errorf("original known CPU invoice is stranded by closed Code: %w", err)
					}
					var usage api.UsageSnapshot
					if err = api.Decode(raw, &usage); err != nil {
						return err
					}
					if !usage.SpendingClosed || !usage.UsageFinal || usage.SourceRef.ObjectID != before.Operation.OperationID || usage.UsageRevision != actual.Operation.Revision || !api.Equal(usage.Cumulative, before.Operation.Usage) || len(usage.ProofRefs) != 1 {
						return fmt.Errorf("minimum invoice changed original known CPU or version")
					}
					body, err := gateway.ReadContentBytes(ctx, gateway.Scope, gateway.ServiceAuth, usage.ProofRefs[0], "execution_usage_proof", "cloud")
					if api.IsCode(err, "forbidden") {
						var sourceError *api.Error
						if !errors.As(err, &sourceError) || sourceError.Reason != "source_closed" {
							return err
						}
						// 已出版的普通旧proof保持原正文权限；最低费用恢复只需原权威账务。
						t.Logf("WASI_LEGACY_INVOICE_BODY_STILL_CLOSED operation=%s usage_revision=%d proof=%s cpu=%s", before.Operation.OperationID, usage.UsageRevision, api.Raw(usage.ProofRefs[0]), usage.Cumulative[0].Value)
					} else if err != nil {
						return fmt.Errorf("metadata-only original invoice cannot be read: %w", err)
					} else {
						var proof execution.UsageProof
						if err = api.Decode(body, &proof); err != nil {
							return err
						}
						if !api.Equal(proof.OperationRef, usage.SourceRef) || !api.Equal(proof.Cumulative, usage.Cumulative) || !proof.SpendingClosed || !proof.UsageFinal || proof.PhysicalCountMin != 1 || proof.PhysicalCountMax != 1 || proof.SendStartedCount != 1 || len(proof.Attempts) != 1 || proof.Attempts[0].AttemptID != before.Attempts.Items[0].AttemptID || !api.Equal(proof.Attempts[0].EvidenceRefs, before.Attempts.Items[0].EvidenceRefs) {
							return fmt.Errorf("minimum invoice lost original physical/metering evidence")
						}
					}
					for _, denied := range []struct {
						auth    runtime.Auth
						purpose string
					}{{gateway.UserAuth, "execution_usage_proof"}, {gateway.ServiceAuth, "task.context"}} {
						_, deniedErr := gateway.Memory.Read(ctx, gateway.Scope, denied.auth, usage.ProofRefs[0], denied.purpose)
						if !api.IsCode(deniedErr, "forbidden") {
							return fmt.Errorf("minimum invoice widened ordinary Content authority: %v", deniedErr)
						}
					}
					assertWASISourceClosed(t, ctx, gateway, code, "environment_code")
					again, err := gateway.Dispatcher.Query(ctx, gateway.ServiceAuth, api.Raw(query))
					if err != nil || !api.Equal(again, raw) {
						return fmt.Errorf("same original usage query changed invoice: %w", err)
					}
					if err = checkAppliedWASIInvoiceAfterUploadWindow(ctx, gateway, query, raw); err != nil {
						return err
					}
					if afterHash := assertOriginalWASIJournal(t, gateway.Config.DataRoot, actual); afterHash != nativeHash {
						return fmt.Errorf("minimum invoice altered the original native journal")
					}
					t.Logf("WASI_ORIGINAL_AUTHORITY_USAGE_AVAILABLE task=%s operation=%s attempt=%s query=%s proof=%s cpu=%s target_owner=false", taskID, before.Operation.OperationID, before.Attempts.Items[0].AttemptID, query.QueryID, api.Raw(usage.ProofRefs[0]), usage.Cumulative[0].Value)
					return nil
				},
			}
			runConfiguredWASITask(t, driver, "withdraw", observer)
		})
	}
}

// 通过 Execution 的消费方出版端口检验伪造输入；原事实只从公开视图读取。
// native 故障发生在已实际退出的唯一原 Attempt，随后恢复准确原字节。
func checkOriginalWASIInvoiceRefusals(t *testing.T, ctx context.Context, a *App, actual execution.OperationView, invoke execution.InvokeInput) error {
	t.Helper()
	proof := execution.UsageProof{OperationRef: a.Scope.Ref(actual.Operation.OperationID, actual.Operation.Revision), IntentHash: invoke.IntentHash, UsageRevision: actual.Operation.Revision, Cumulative: actual.Operation.Usage, SpendingClosed: true, UsageFinal: true, SendStartedCount: 1, PhysicalCountMin: 1, PhysicalCountMax: 1, Attempts: actual.Attempts.Items}
	publication := execution.Publication{ContentID: stableID("content", actual.Operation.OperationID+":usage:"+strconv.FormatUint(actual.Operation.Revision, 10)), MediaType: "application/json", Purpose: "execution_usage_proof", Location: "cloud", ProcessedSources: append([]api.ContentRef{invoke.IntentRef}, actual.Operation.EvidenceRefs...), DisclosedSources: []api.ContentRef{}}
	var content execution.ContentPort = executionContent{a}
	reject := func(label, code string, auth runtime.Auth, p execution.Publication, value execution.UsageProof) error {
		_, err := content.Publish(ctx, a.Scope, auth, p, api.Raw(value))
		if !api.IsCode(err, code) {
			return fmt.Errorf("%s must preserve original authority/fee facts: %v", label, err)
		}
		t.Logf("WASI_INVOICE_GUARD label=%s operation=%s attempt=%s refused=%s", label, actual.Operation.OperationID, actual.Attempts.Items[0].AttemptID, code)
		return nil
	}
	if err := reject("ordinary_actor", "forbidden", a.UserAuth, publication, proof); err != nil {
		return err
	}
	oldAuth := a.ServiceAuth
	oldAuth.CredentialGeneration++
	if err := reject("noncurrent_credential", "forbidden", oldAuth, publication, proof); err != nil {
		return err
	}
	for _, label := range []string{"cpu", "physical_count", "intent"} {
		var counterfeit execution.UsageProof
		if err := api.Decode(api.Raw(proof), &counterfeit); err != nil {
			return err
		}
		switch label {
		case "cpu":
			counterfeit.Cumulative = []api.Amount{{Unit: "cpu_seconds", Value: "1"}}
		case "physical_count":
			counterfeit.PhysicalCountMin, counterfeit.PhysicalCountMax = 2, 2
		case "intent":
			counterfeit.IntentHash = api.Hash([]byte("counterfeit original intent"))
		}
		if err := reject("changed_"+label, "forbidden", a.ServiceAuth, publication, counterfeit); err != nil {
			return err
		}
	}
	wrongSources := publication
	wrongSources.ProcessedSources = []api.ContentRef{invoke.IntentRef}
	if err := reject("missing_original_evidence", "revision_conflict", a.ServiceAuth, wrongSources, proof); err != nil {
		return err
	}
	if a.WASI == nil {
		return fmt.Errorf("original WASI metering profile missing")
	}
	root := filepath.Join(a.Config.DataRoot, "wasi")
	wrongScope := a.Scope
	wrongScope.DatabaseID = api.NewID("database")
	_, err := wasi.ReadOriginalReceipt(ctx, root, wrongScope, actual.Operation.OperationID, actual.Attempts.Items[0].AttemptID, a.WASI.ConfigRef, a.WASI.InstallLockRef)
	if !api.IsCode(err, "forbidden") {
		return fmt.Errorf("native receipt from another database must not authorize invoice: %v", err)
	}
	wrongLock := a.WASI.InstallLockRef
	wrongLock.Digest = api.Hash([]byte("different WASI installation"))
	_, err = wasi.ReadOriginalReceipt(ctx, root, a.Scope, actual.Operation.OperationID, actual.Attempts.Items[0].AttemptID, a.WASI.ConfigRef, wrongLock)
	if !api.IsCode(err, "forbidden") {
		return fmt.Errorf("native receipt from another installation must not authorize invoice: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = wasi.ReadOriginalReceipt(cancelled, root, a.Scope, actual.Operation.OperationID, actual.Attempts.Items[0].AttemptID, a.WASI.ConfigRef, a.WASI.InstallLockRef)
	if !errors.Is(err, context.Canceled) {
		return fmt.Errorf("passive invoice metering lost cancellation cause: %v", err)
	}
	path := filepath.Join(root, actual.Attempts.Items[0].AttemptID+".json")
	for _, fault := range []struct{ label, field, code string }{{"changed_native_binding", "binding_digest", "forbidden"}, {"unobserved_native_exit", "phase", "accounting_unknown"}} {
		err = withOriginalWASIJournalFault(path, func(original map[string]json.RawMessage) {
			if fault.field == "phase" {
				original[fault.field] = api.Raw("started")
				original["usage_final"] = api.Raw(false)
			} else {
				original[fault.field] = api.Raw(api.Hash([]byte("counterfeit native metering binding")))
			}
		}, func() error { return reject(fault.label, fault.code, a.ServiceAuth, publication, proof) })
		if err != nil {
			return err
		}
	}
	unchanged, err := originalWASIInvoiceOperation(ctx, a, actual.Operation.OperationID)
	if err != nil || !api.Equal(unchanged, actual) {
		return fmt.Errorf("rejected invoice changed original Operation/Attempt metadata: %w", err)
	}
	return nil
}

func withOriginalWASIJournalFault(path string, mutate func(map[string]json.RawMessage), consume func() error) (err error) {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	original, readErr := io.ReadAll(io.LimitReader(f, api.MaxJSONBytes+1))
	err = errors.Join(readErr, f.Close())
	if err != nil {
		return err
	}
	if len(original) > api.MaxJSONBytes {
		return fmt.Errorf("original native journal exceeded its private bound")
	}
	var value map[string]json.RawMessage
	if err = api.Decode(original, &value); err != nil {
		return err
	}
	mutate(value)
	changed, err := json.Marshal(value)
	if err != nil {
		return err
	}
	// 故障仅改真实已停止日志；无论断言结果都恢复准确原记录。
	defer func() { err = errors.Join(err, writeOriginalWASIJournal(path, original)) }()
	if err = writeOriginalWASIJournal(path, changed); err != nil {
		return err
	}
	return consume()
}

func writeOriginalWASIJournal(path string, value []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	n, writeErr := f.Write(value)
	if writeErr == nil && n != len(value) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, f.Sync(), f.Close())
}

func checkAppliedWASIInvoiceAfterUploadWindow(ctx context.Context, a *App, query api.Query, expected json.RawMessage) (err error) {
	// 时钟 seam 只推进当前 Tx 时间，准确原账本/字节/命令期限保持不变。
	// 已 applied 原 PUT 在冻结的 30min 上传窗之后仍只能恢复原回执。
	store, memoryStore, dispatcherStore := a.Store, a.Memory.Store, a.Dispatcher.Store
	bindings, ok := store.(runtime.QueryBindingStore)
	if !ok {
		return api.E("unsupported", "original_query_binding_store_required")
	}
	future := struct {
		modelInvoiceClockStore
		runtime.QueryBindingStore
	}{modelInvoiceClockStore{Store: store, offset: 31 * time.Minute}, bindings}
	a.Store, a.Memory.Store, a.Dispatcher.Store = future, future, future
	defer func() { a.Store, a.Memory.Store, a.Dispatcher.Store = store, memoryStore, dispatcherStore }()
	replayed, err := a.Dispatcher.Query(ctx, a.ServiceAuth, api.Raw(query))
	if err != nil {
		return fmt.Errorf("applied original WASI PUT failed after frozen upload window: %w", err)
	}
	if !api.Equal(replayed, expected) {
		return fmt.Errorf("applied original WASI PUT changed proof or fee after upload window")
	}
	return nil
}

func originalWASIInvoiceOperation(ctx context.Context, a *App, operationID string) (execution.OperationView, error) {
	raw, err := a.query(ctx, "execution.get", operationID, execution.OperationIDInput{OperationID: operationID})
	var view execution.OperationView
	if err == nil {
		err = api.Decode(raw, &view)
	}
	return view, err
}
