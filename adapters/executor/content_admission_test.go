package executor

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// seam 为公开 Host/SDK/TLS、实际 SQLite 设备和目标文件；两种云端库仅持久
// 保存本片明确预置的受信 admission/once lease，不冒充公开 Task 的新授权流程。
func cachedAdmissionAuthority(t *testing.T, f *deviceFixture, driver string) runtime.Store {
	t.Helper()
	var st runtime.Store
	var err error
	if driver == "postgres" {
		dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("PostgreSQL 未配置，不算该库通过")
		}
		pg, e := postgres.Open(f.ctx, dsn)
		if e == nil {
			e = pg.Migrate(f.ctx)
		}
		st, err = pg, e
	} else {
		sql, e := sqlite.Open(filepath.Join(t.TempDir(), "authority.db"))
		if e == nil {
			e = sql.Migrate(f.ctx)
		}
		st, err = sql, e
	}
	if err != nil {
		if st != nil {
			if closeErr := st.Close(); closeErr != nil {
				t.Error(closeErr)
			}
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	return st
}

func sealCachedAdmission(t *testing.T, f *deviceFixture, st runtime.Store) {
	t.Helper()
	allocation := f.b.Lease
	allocation.AllocationDigest, allocation.Proof = "", ""
	var err error
	f.b.Lease.AllocationDigest, err = api.Digest(allocation)
	if err != nil {
		t.Fatal(err)
	}
	f.b.Lease.Proof, err = (Proof{Keys: f.keys, SigningKeyID: "development-es256"}).SignLocal(governance.ProofStatement{TenantID: f.b.Principal.TenantID, IssuerID: f.b.AuthorityID, AudienceID: f.b.EndpointID, Purpose: "grant_lease", ObjectRef: f.b.LeaseRef, Digest: f.b.Lease.AllocationDigest, IssuedAt: f.b.Lease.IssuedAt, StartBefore: f.b.Lease.ExpiresAt})
	if err != nil {
		t.Fatal(err)
	}
	scope := runtime.Scope{TenantID: f.b.Principal.TenantID, OwnerID: f.b.AuthorityID, DatabaseID: st.ID()}
	status, err := st.Within(f.ctx, scope, []string{Namespace}, func(tx runtime.Tx) error {
		var e error
		f.b, e = SealAdmissionTx(f.ctx, tx, Proof{Keys: f.keys, SigningKeyID: "development-es256"}, f.b)
		return e
	})
	if status != runtime.Committed || err != nil {
		t.Fatalf("原受信 admission 未封存: %s %v", status, err)
	}
}

func allowCachedDeviceReadBinding(t *testing.T, f *deviceFixture) api.ObjectRef {
	t.Helper()
	cfg := f.h.Config
	binding := cfg.Bindings[0]
	binding.CapabilityRef = target.FileReadCapability().Ref
	binding.BindingRef = f.h.Scope.Ref(api.NewID("binding"), 1)
	binding.Actions = []string{"file.read"}
	cfg.Bindings = append(cfg.Bindings, binding)
	if err := f.h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err := Open(f.ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	f.h = h
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	})
	return binding.BindingRef
}

func cachedReadAdmission(t *testing.T, f *deviceFixture, binding api.ObjectRef, readPurpose bool) {
	t.Helper()
	old := f.b
	if err := api.Decode(api.Raw(old), &f.b); err != nil {
		t.Fatal(err)
	}
	b := &f.b
	b.BundleID, b.OriginalCommandID = api.NewID("bundle"), api.NewID("command")
	b.Intent.OperationID, b.Intent.AdmissionSourceRef.ObjectID = api.NewID("operation"), api.NewID("admission")
	b.Intent.LogicalStepKey = "independent_readback"
	b.Intent.CapabilityRef, b.Intent.BindingRef = target.FileReadCapability().Ref, binding
	args := api.Raw(target.FileReadArguments{Path: "reports/result.txt"})
	b.Intent.ArgumentsRef = api.ContentRef{TenantID: b.Principal.TenantID, OwnerID: b.AuthorityID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(args), ByteLength: uint64(len(args)), MediaType: "application/json"}
	raw := api.Raw(b.Intent)
	b.IntentRef = api.ContentRef{TenantID: b.Principal.TenantID, OwnerID: b.AuthorityID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(raw), ByteLength: uint64(len(raw)), MediaType: "application/json"}
	var err error
	b.ExecutionHash, err = api.Digest(b.Intent)
	if err != nil {
		t.Fatal(err)
	}
	b.AdmissionHash = api.Hash([]byte(b.Intent.OperationID + "/trusted-readback-admission"))
	b.ReservationRef.ObjectID, b.Lease.LeaseID = api.NewID("reservation"), api.NewID("lease")
	b.LeaseRef.ObjectID = b.Lease.LeaseID
	b.UseRefs = []api.ObjectRef{b.LeaseRef}
	b.Lease.Scope.UseID = b.Lease.LeaseID
	b.Lease.Scope.TargetRef.ObjectID = b.Intent.OperationID
	b.Lease.Scope.IntentHash = b.AdmissionHash
	b.Lease.Scope.Actions, b.Lease.Scope.Purposes = []string{"file.read"}, []string{"independent_readback"}
	b.Contents[0].ContentRef, b.Contents[1].ContentRef = b.IntentRef, b.Intent.ArgumentsRef
	if readPurpose {
		b.Contents[2].Purposes = []string{"managed_file_read"}
	} else {
		b.Contents[2].Purposes = []string{"managed_file_write"}
	}
	f.raw[contentKey(b.IntentRef)], f.raw[contentKey(b.Intent.ArgumentsRef)] = raw, args
}

func dispatchCachedAdmission(t *testing.T, f *deviceFixture, client *Client, ctx context.Context) api.Command {
	t.Helper()
	if err := client.Prepare(ctx, f.b, func(_ context.Context, permission ContentPermission) ([]byte, error) {
		return f.raw[contentKey(permission.ContentRef)], nil
	}); err != nil {
		t.Fatal(err)
	}
	window := f.control(t, "active", "running", f.b.Intent.ControlRevision)
	var delivery ControlDelivery
	if _, err := f.h.Store.Read(ctx, f.h.Scope, Namespace+".controls", window.WindowID, 0, &delivery); err != nil {
		t.Fatal(err)
	}
	original := frozenInvoke(f, window)
	if _, err := client.Dispatch(ctx, original, delivery); err != nil {
		t.Fatal(err)
	}
	return original
}

func awaitCachedOperation(t *testing.T, ctx context.Context, client *Client, operationID string) execution.OperationView {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		view, err := client.Get(ctx, operationID)
		if err != nil {
			t.Fatal(err)
		}
		if view.NewAttemptsClosed && view.ActuallyStopped && view.Operation.UsageFinal {
			return view
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("原 operation 未在有界观察内关闭")
	return execution.OperationView{}
}

func readCachedResult(t *testing.T, f *deviceFixture, client *Client, ctx context.Context, ref api.ContentRef) []byte {
	t.Helper()
	r := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: api.ObjectRef{TenantID: f.b.Principal.TenantID, OwnerID: f.b.AuthorityID, ObjectID: api.NewID("intent"), Revision: 1}, HolderRef: f.b.Principal.Auth().Ref(f.b.AuthorityID), Purpose: "execution_result", Location: "cloud", RetainUntil: api.Time(time.Now().Add(10 * time.Second))}
	key := f.h.Keys.Keys["device-es256"]
	port := &SourceClient{Client: client, Keys: &platform.Keyring{Keys: map[string]platform.RegisteredKey{"device-es256": {TenantID: f.b.Principal.TenantID, Issuer: ref.OwnerID, Public: key.Public, Purposes: []string{"executor_content"}}}}}
	proof, err := port.RegisterCopy(ctx, runtime.Scope{TenantID: r.HolderRef.TenantID, OwnerID: r.HolderRef.OwnerID}, f.b.Principal.Auth(), r)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := port.Read(ctx, runtime.Scope{TenantID: r.HolderRef.TenantID, OwnerID: r.HolderRef.OwnerID}, f.b.Principal.Auth(), r, proof)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTLSCachedBytesUseTheOriginalCurrentReadAdmission(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			f := newDeviceFixture(t)
			binding := allowCachedDeviceReadBinding(t, f)
			f.b.Contents[2].Purposes = []string{"managed_file_write"}
			freezeSourcePolicies(t, f)
			st := cachedAdmissionAuthority(t, f, driver)
			sealCachedAdmission(t, f, st)
			client, ctx := startTLSHostFor(t, f, 90*time.Second)
			writeBundle := f.b
			writeCommand := dispatchCachedAdmission(t, f, client, ctx)
			write := awaitCachedOperation(t, ctx, client, writeBundle.Intent.OperationID)
			if write.Operation.Effect != "applied" || len(write.Attempts.Items) != 1 {
				t.Fatalf("原写入没有真实完成: %+v", write)
			}
			actual, err := os.ReadFile(filepath.Join(f.root, "files", "reports", "result.txt"))
			if err != nil || string(actual) != "independent cloud-admitted file bytes\n" {
				t.Fatalf("独立原目标字节不符: %v", err)
			}
			cachedReadAdmission(t, f, binding, true)
			sealCachedAdmission(t, f, st)
			readCommand := dispatchCachedAdmission(t, f, client, ctx)
			read := awaitCachedOperation(t, ctx, client, f.b.Intent.OperationID)
			t.Logf("CACHE_READ_OBSERVATION %s", api.Raw(struct {
				Scope          runtime.Scope   `json:"device_scope"`
				WriteCommand   string          `json:"write_command"`
				ReadCommand    string          `json:"read_command"`
				WriteBundle    string          `json:"write_bundle"`
				ReadBundle     string          `json:"read_bundle"`
				WriteOperation string          `json:"write_operation"`
				ReadOperation  string          `json:"read_operation"`
				Effect         string          `json:"read_effect"`
				AttemptCount   int             `json:"read_attempt_count"`
				ResultRef      *api.ContentRef `json:"read_result"`
			}{f.h.Scope, writeCommand.CommandID, readCommand.CommandID, writeBundle.BundleID, f.b.BundleID, writeBundle.Intent.OperationID, f.b.Intent.OperationID, read.Operation.Effect, len(read.Attempts.Items), read.Operation.ResultRef}))
			if read.Operation.Effect != "not_applied" || read.Operation.ResultRef == nil || len(read.Attempts.Items) != 1 {
				t.Fatalf("当前原 read admission 没有读取已缓存的准确来源: %+v", read)
			}
			var result target.FileReadResult
			if err := api.Decode(readCachedResult(t, f, client, ctx, *read.Operation.ResultRef), &result); err != nil {
				t.Fatal(err)
			}
			bytes, err := base64.StdEncoding.Strict().DecodeString(result.DataBase64)
			if err != nil || string(bytes) != "independent cloud-admitted file bytes\n" || result.Version != api.Hash(actual) || result.Path != "reports/result.txt" {
				t.Fatalf("独立读回不是原准确文件: %v", err)
			}
			// 原已出版结果不属于输入 Contents；核对必须沿原 Attempt 读回它，
			// 而不是因新输入门禁丢失已知结果或开启第二次物理读取。
			reconcile := commandFor(f.b.EndpointID, api.NewID("command"), "execution.reconcile", f.b.Intent.OperationID, f.b.Intent.Deadline, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
			if receipt, err := client.Send(ctx, reconcile); err != nil || receipt.Stage != "applied" {
				t.Fatalf("原读回恢复未接纳: %+v %v", receipt, err)
			}
			var reconciled execution.OperationView
			for until := time.Now().Add(10 * time.Second); time.Now().Before(until); {
				reconciled, err = client.Get(ctx, f.b.Intent.OperationID)
				if err != nil {
					t.Fatal(err)
				}
				if len(reconciled.Attempts.Items) == 1 && reconciled.Attempts.Items[0].FactRevision > read.Attempts.Items[0].FactRevision {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(20 * time.Millisecond):
				}
			}
			if len(reconciled.Attempts.Items) != 1 || reconciled.Attempts.Items[0].AttemptID != read.Attempts.Items[0].AttemptID || reconciled.Attempts.Items[0].FactRevision <= read.Attempts.Items[0].FactRevision || reconciled.Operation.Effect != "not_applied" || !reconciled.Operation.UsageFinal || !reconciled.ActuallyStopped {
				t.Fatalf("原已出版读回结果无法恢复: %+v", reconciled)
			}
			if receipt, err := client.Send(ctx, writeCommand); err != nil || receipt.Stage != "applied" {
				t.Fatalf("原写入 receipt 丢失: %+v %v", receipt, err)
			}
			if receipt, err := client.Send(ctx, readCommand); err != nil || receipt.Stage != "applied" {
				t.Fatalf("原读回 receipt 丢失: %+v %v", receipt, err)
			}
			old, err := client.Get(ctx, writeBundle.Intent.OperationID)
			if err != nil || len(old.Attempts.Items) != 1 || old.Attempts.Items[0].AttemptID != write.Attempts.Items[0].AttemptID {
				t.Fatal("读回改变了原写入身份")
			}
			t.Logf("CACHE_ADMISSION_EVIDENCE %s", api.Raw(struct {
				WriteBundle    string         `json:"write_bundle"`
				ReadBundle     string         `json:"read_bundle"`
				WriteOperation string         `json:"write_operation"`
				ReadOperation  string         `json:"read_operation"`
				WriteAttempt   string         `json:"write_attempt"`
				ReadAttempt    string         `json:"read_attempt"`
				ContentRef     api.ContentRef `json:"cached_content"`
			}{writeBundle.BundleID, f.b.BundleID, writeBundle.Intent.OperationID, f.b.Intent.OperationID, write.Attempts.Items[0].AttemptID, read.Attempts.Items[0].AttemptID, writeBundle.Contents[2].ContentRef}))
		})
	}
}

func TestTLSCachedReadRechecksOriginalSubjectRevocationAndRetainsUsage(t *testing.T) {
	f := newDeviceFixture(t)
	binding := allowCachedDeviceReadBinding(t, f)
	f.b.Contents[2].Purposes = []string{"managed_file_write"}
	freezeSourcePolicies(t, f)
	st := cachedAdmissionAuthority(t, f, "sqlite")
	sealCachedAdmission(t, f, st)
	client, ctx := startTLSHostFor(t, f, 90*time.Second)
	writeBundle := f.b
	dispatchCachedAdmission(t, f, client, ctx)
	write := awaitCachedOperation(t, ctx, client, f.b.Intent.OperationID)
	if write.Operation.Effect != "applied" || len(write.Attempts.Items) != 1 {
		t.Fatal("原真实写入未完成")
	}
	cachedReadAdmission(t, f, binding, true)
	sealCachedAdmission(t, f, st)
	if err := client.Prepare(ctx, f.b, func(_ context.Context, p ContentPermission) ([]byte, error) {
		return f.raw[contentKey(p.ContentRef)], nil
	}); err != nil {
		t.Fatal(err)
	}
	revocation := Revocation{AuthorityID: f.b.AuthorityID, EndpointID: f.b.EndpointID, ObjectRef: f.b.Lease.Scope.SubjectRef, Kind: "subject", IssuedAt: api.Time(time.Now().Add(-time.Second)), StartBefore: api.Time(time.Now().Add(time.Minute))}
	digest, err := api.Digest(revocation)
	if err != nil {
		t.Fatal(err)
	}
	revocation.Proof, err = f.keys.Sign("development-es256", revocationClaims(revocation, digest))
	if err != nil {
		t.Fatal(err)
	}
	command := commandFor(f.b.EndpointID, api.NewID("command"), "executor.revocation.install", revocation.ObjectRef.ObjectID, revocation.StartBefore, revocation)
	if receipt, err := client.Send(ctx, command); err != nil || receipt.Stage != "applied" {
		t.Fatalf("准确原主体撤权未提交: %+v %v", receipt, err)
	}
	window := f.control(t, "active", "running", f.b.Intent.ControlRevision)
	var delivery ControlDelivery
	if _, err = f.h.Store.Read(ctx, f.h.Scope, Namespace+".controls", window.WindowID, 0, &delivery); err != nil {
		t.Fatal(err)
	}
	original := frozenInvoke(f, window)
	if _, err = client.Dispatch(ctx, original, delivery); err != nil {
		t.Fatal(err)
	}
	read := awaitCachedOperation(t, ctx, client, f.b.Intent.OperationID)
	if read.Operation.Effect != "not_started" || len(read.Attempts.Items) != 0 || read.Operation.ResultRef != nil {
		t.Fatalf("缓存或旧阳性 admission 绕过当前撤权: %+v", read)
	}
	usage, err := client.Usage(ctx, read.Operation.OperationID)
	if err != nil || !usage.SpendingClosed || !usage.UsageFinal || len(usage.Cumulative) != 1 || usage.Cumulative[0].Value != "0" {
		t.Fatalf("当前撤权删除了原最低账务: %+v %v", usage, err)
	}
	old, err := client.Get(ctx, writeBundle.Intent.OperationID)
	if err != nil || old.Operation.Effect != "applied" || len(old.Attempts.Items) != 1 || old.Attempts.Items[0].AttemptID != write.Attempts.Items[0].AttemptID {
		t.Fatal("当前撤权抹去原实际写入")
	}
	t.Logf("CACHE_REVOCATION_EVIDENCE operation=%s original_command=%s original_subject=%s generation=%d usage_revision=%d", read.Operation.OperationID, original.CommandID, revocation.ObjectRef.ObjectID, revocation.ObjectRef.Revision, usage.UsageRevision)
}

func TestTLSCachedPermissionOfAnotherOperationCannotAuthorizeCurrentRead(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			f := newDeviceFixture(t)
			binding := allowCachedDeviceReadBinding(t, f)
			// 首次缓存虽明确授予 read，新的原 read 操作没有该用途。
			// 唯一正确结果是拒绝；不能拿缓存、其它 bundle 或原成功写入补权。
			freezeSourcePolicies(t, f)
			st := cachedAdmissionAuthority(t, f, driver)
			sealCachedAdmission(t, f, st)
			client, ctx := startTLSHostFor(t, f, 90*time.Second)
			writeBundle := f.b
			dispatchCachedAdmission(t, f, client, ctx)
			write := awaitCachedOperation(t, ctx, client, f.b.Intent.OperationID)
			if write.Operation.Effect != "applied" || len(write.Attempts.Items) != 1 {
				t.Fatal("原真实 write 未完成")
			}
			cachedReadAdmission(t, f, binding, false)
			sealCachedAdmission(t, f, st)
			original := dispatchCachedAdmission(t, f, client, ctx)
			read := awaitCachedOperation(t, ctx, client, f.b.Intent.OperationID)
			if read.Operation.Effect != "not_started" || len(read.Attempts.Items) != 0 || read.Operation.ResultRef != nil {
				t.Fatalf("其它原操作的许可授权了本次缺用途的 read: %+v", read)
			}
			if receipt, err := client.Send(ctx, original); err != nil || receipt.Stage != "applied" {
				t.Fatalf("原接纳回执不应改写成执行成功: %+v %v", receipt, err)
			}
			old, err := client.Get(ctx, writeBundle.Intent.OperationID)
			if err != nil || old.Operation.Effect != "applied" || len(old.Attempts.Items) != 1 || old.Attempts.Items[0].AttemptID != write.Attempts.Items[0].AttemptID {
				t.Fatal("拒绝 read 改写了原 write")
			}
			usage, err := client.Usage(ctx, read.Operation.OperationID)
			if err != nil || !usage.SpendingClosed || !usage.UsageFinal || len(usage.Cumulative) != 1 || usage.Cumulative[0].Value != "0" {
				t.Fatalf("缺用途原操作最低账务丢失: %+v %v", usage, err)
			}
			t.Logf("CACHE_PURPOSE_REFUSAL original_operation=%s original_command=%s original_usage_revision=%d physical_attempt_count=%d", read.Operation.OperationID, original.CommandID, usage.UsageRevision, len(read.Attempts.Items))
		})
	}
}
