package executor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	rt "github.com/ruipengliu/lerna/runtime"
)

var processBuild sync.Once
var processBinary, processBinaryRoot string
var processBuildError error

func TestMain(m *testing.M) {
	code := m.Run()
	if processBinaryRoot != "" {
		_ = os.RemoveAll(processBinaryRoot)
	}
	os.Exit(code)
}
func executorBinary(t *testing.T) string {
	t.Helper()
	processBuild.Do(func() {
		processBinaryRoot, processBuildError = os.MkdirTemp("", "harness-independent-executor-*")
		if processBuildError != nil {
			return
		}
		processBinary = filepath.Join(processBinaryRoot, "executor")
		_, file, _, _ := runtime.Caller(0)
		root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		build := exec.CommandContext(ctx, "go", "build", "-o", processBinary, "./cmd/executor")
		build.Dir = root
		output, err := build.CombinedOutput()
		if err != nil {
			processBuildError = errors.Join(err, errors.New(string(output)))
		}
	})
	if processBuildError != nil {
		t.Fatal(processBuildError)
	}
	return processBinary
}
func startDeviceProcess(t *testing.T, f *deviceFixture) (*Client, func(), context.Context) {
	t.Helper()
	binary := executorBinary(t)
	ca, cert, key := deviceTLSFiles(t, f.root)
	c := f.h.Config
	c.TLSCertificateFile = cert
	c.TLSKeyFile = key
	c.GRPCAddr = reserveAddress(t)
	configPath := filepath.Join(f.root, "device.json")
	if err := SaveConfig(configPath, c); err != nil {
		t.Fatal(err)
	}
	if err := f.h.Close(); err != nil {
		t.Fatal(err)
	}
	process := exec.Command(binary, "serve", "--config", configPath)
	logFile, err := os.Create(filepath.Join(f.root, "process.log"))
	if err != nil {
		t.Fatal(err)
	}
	process.Stdout = logFile
	process.Stderr = logFile
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	var stopped sync.Once
	stop := func() {
		stopped.Do(func() {
			if err := process.Process.Signal(syscall.SIGTERM); err != nil {
				t.Error(err)
			}
			select {
			case err := <-done:
				if err != nil {
					body, _ := os.ReadFile(logFile.Name())
					t.Errorf("device process exit: %v %s", err, body)
				}
			case <-time.After(8 * time.Second):
				_ = process.Process.Kill()
				<-done
				t.Error("independent device did not join on SIGTERM")
			}
			if err := logFile.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(stop)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	client, err := Dial(ctx, RemoteConfig{DatabaseID: f.b.DeviceDatabaseID, Endpoint: "grpcs://" + c.GRPCAddr, OwnerID: f.b.EndpointID, InstanceID: f.b.InstanceID, AuthorityID: f.b.AuthorityID, TenantID: f.b.Principal.TenantID, TLSCAFile: ca, PeerTokenFile: c.PeerTokenFile, JournalRoot: filepath.Join(f.root, "cloud-outbound")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		var out ContentOutput
		err = client.query(ctx, "executor.content.status", f.b.IntentRef.ContentID, ContentID{f.b.IntentRef}, &out)
		if api.IsCode(err, "not_found") {
			return client, stop, ctx
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("independent device not ready: %v", err)
	return nil, nil, nil
}
func makeControlDelivery(t *testing.T, f *deviceFixture) ControlDelivery {
	t.Helper()
	now := time.Now()
	c := api.ControlSnapshot{TaskID: f.b.Intent.TaskRef.ObjectID, OrchestratorID: f.b.AuthorityID, GoalRevision: f.b.Intent.GoalRevision, ControlRevision: f.b.Intent.ControlRevision, Status: "active", Control: "running", WindowID: api.NewID("window"), IssuedAt: api.Time(now), StartBefore: api.Time(now.Add(5 * time.Second))}
	digest, _ := api.Digest(c)
	claims := controlClaims(c, digest)
	claims.TenantID = f.b.Principal.TenantID
	claims.ObjectRef.TenantID = f.b.Principal.TenantID
	token, err := f.keys.Sign("development-es256", claims)
	if err != nil {
		t.Fatal(err)
	}
	c.ProofRef = api.ContentRef{TenantID: f.b.Principal.TenantID, OwnerID: f.b.AuthorityID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte(token)), MediaType: "application/jose", ByteLength: uint64(len(token))}
	return ControlDelivery{c, token}
}
func TestIndependentExecutorCLIRealSQLiteTLSAndSIGTERM(t *testing.T) {
	f := newDeviceFixture(t)
	client, stop, ctx := startDeviceProcess(t, f)
	if err := client.Prepare(ctx, f.b, func(_ context.Context, p ContentPermission) ([]byte, error) {
		return f.raw[contentKey(p.ContentRef)], nil
	}); err != nil {
		t.Fatal(err)
	}
	delivery := makeControlDelivery(t, f)
	original := frozenInvoke(f, delivery.Snapshot)
	if err := applied(client.Dispatch(ctx, original, delivery)); err != nil {
		t.Fatal(err)
	}
	view := waitApplied(t, ctx, client, f.b.Intent.OperationID)
	if len(view.Attempts.Items) != 1 {
		t.Fatal("physical attempt count")
	}
	actual, err := os.ReadFile(filepath.Join(f.root, "files", "reports", "result.txt"))
	if err != nil || string(actual) != "independent cloud-admitted file bytes\n" {
		t.Fatalf("actual device bytes: %q %v", actual, err)
	}
	report, err := client.LeaseUsage(ctx, f.b.Lease.LeaseID)
	if err != nil || !report.Report.Usage.UsageFinal {
		t.Fatalf("original late usage: %+v %v", report, err)
	}
	stop()
	_, err = client.Get(ctx, f.b.Intent.OperationID)
	if !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("stopped endpoint: %v", err)
	}
	binary, _ := os.ReadFile(processBinary)
	t.Logf("EXECUTOR_PROCESS_EVIDENCE %s", api.Raw(struct {
		DeviceID         string            `json:"device_id"`
		DatabaseID       string            `json:"database_id"`
		TaskRef          api.ObjectRef     `json:"task_ref"`
		OperationID      string            `json:"operation_id"`
		CommandID        string            `json:"command_id"`
		AttemptID        string            `json:"attempt_id"`
		LeaseRef         api.ObjectRef     `json:"lease_ref"`
		LeaseUsage       api.UsageSnapshot `json:"lease_usage"`
		BinarySHA256     string            `json:"binary_sha256"`
		Stopped          bool              `json:"sigterm_joined"`
		CloudTaskFixture bool              `json:"cloud_task_fixture"`
	}{f.b.EndpointID, f.h.Config.DatabaseID, f.b.Intent.TaskRef, f.b.Intent.OperationID, original.CommandID, view.Attempts.Items[0].AttemptID, f.b.LeaseRef, report.Report.Usage, api.Hash(binary), true, true}))
}
func TestActualPostgresAuthorityReservesOnceAndSettlesIndependentDeviceLedger(t *testing.T) {
	dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("actual PostgreSQL authority not configured")
	}
	ctx := context.Background()
	cloud, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cloud.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = cloud.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	f := newDeviceFixture(t)
	scope := rt.Scope{TenantID: f.b.Principal.TenantID, OwnerID: f.b.AuthorityID, DatabaseID: cloud.ID()}
	user := rt.Auth{TenantID: scope.TenantID, SubjectID: f.b.Lease.Scope.SubjectRef.ObjectID, CredentialGeneration: 1, Roles: []string{"grant_authority"}}
	gov := governance.New(cloud, governance.Options{Proof: Proof{Keys: f.keys, SigningKeyID: "development-es256"}})
	grantRef := f.b.Lease.GrantRefs[0]
	grant := api.Grant{GrantID: grantRef.ObjectID, OwnerID: scope.OwnerID, Revision: 1, SubjectRef: user.Ref(scope.OwnerID), Resources: f.b.Lease.Scope.Resources, Actions: f.b.Lease.Scope.Actions, Purposes: f.b.Lease.Scope.Purposes, Recipients: []string{f.b.EndpointID}, Locations: []string{"device"}, Mode: "once", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: f.b.Lease.ExpiresAt, Limits: f.b.Lease.Limits}
	// 此夹具预置受信批准的原Grant；真实Use/Lease/once/结算走domain同库用例。
	status, err := cloud.Within(ctx, scope, []string{"governance"}, func(tx rt.Tx) error {
		if err := tx.Create(ctx, "governance/grants", grant.GrantID, "", grant); err != nil {
			return err
		}
		return tx.Create(ctx, "governance/grant_usage", grant.GrantID, "", governance.GrantUsage{GrantID: grant.GrantID, Revision: 1, Spent: []api.Amount{}, Reserved: []api.Amount{}})
	})
	if err != nil || status != rt.Committed {
		t.Fatalf("approved grant fixture: %s %v", status, err)
	}
	status, err = cloud.Within(ctx, scope, []string{"governance", Namespace}, func(tx rt.Tx) error {
		lease, err := gov.AllocateLeaseTx(ctx, tx, user, governance.LeaseAllocate{LeaseID: f.b.Lease.LeaseID, EndpointID: f.b.EndpointID, InstanceID: f.b.InstanceID, Scope: f.b.Lease.Scope, Limits: f.b.Lease.Limits, ExpiresAt: f.b.Lease.ExpiresAt, CostMode: "strict"})
		if err != nil {
			return err
		}
		f.b.Lease = lease
		f.b.IssuedAt = lease.IssuedAt
		f.b.StartBefore = lease.ExpiresAt
		f.b, err = SealAdmissionTx(ctx, tx, Proof{Keys: f.keys, SigningKeyID: "development-es256"}, f.b)
		return err
	})
	if err != nil || status != rt.Committed {
		t.Fatalf("cloud original reservation: %s %v", status, err)
	}
	var before governance.GrantUsage
	if _, err = cloud.Read(ctx, scope, "governance/grant_usage", grant.GrantID, 0, &before); err != nil {
		t.Fatal(err)
	}
	if !before.OnceConsumed || len(before.Reserved) != 1 || before.Reserved[0].Value != "1" {
		t.Fatalf("single cloud reservation: %+v", before)
	}
	client, stop, runCtx := startDeviceProcess(t, f)
	if err = client.Prepare(runCtx, f.b, func(_ context.Context, p ContentPermission) ([]byte, error) {
		return f.raw[contentKey(p.ContentRef)], nil
	}); err != nil {
		t.Fatal(err)
	}
	delivery := makeControlDelivery(t, f)
	original := frozenInvoke(f, delivery.Snapshot)
	if err = applied(client.Dispatch(runCtx, original, delivery)); err != nil {
		t.Fatal(err)
	}
	view := waitApplied(t, runCtx, client, f.b.Intent.OperationID)
	report, err := client.LeaseUsage(runCtx, f.b.Lease.LeaseID)
	if err != nil {
		t.Fatal(err)
	}
	devicePublic := f.h.Keys.Keys["device-es256"]
	devicePublic.Private = nil
	f.keys.Keys["device-es256"] = devicePublic
	if err = VerifyLeaseReport(f.keys, report); err != nil {
		t.Fatal(err)
	}
	status, err = cloud.Within(ctx, scope, []string{"governance"}, func(tx rt.Tx) error { _, err := gov.ApplyLeaseReportTx(ctx, tx, report.Report); return err })
	if err != nil || status != rt.Committed {
		t.Fatalf("cloud original ledger reconciliation: %s %v", status, err)
	}
	var after governance.GrantUsage
	if _, err = cloud.Read(ctx, scope, "governance/grant_usage", grant.GrantID, 0, &after); err != nil {
		t.Fatal(err)
	}
	if !after.OnceConsumed || (len(after.Reserved) != 1 || after.Reserved[0].Value != "0") || len(after.Spent) != 1 || after.Spent[0].Value != "0" {
		t.Fatalf("settled original reserve: %+v", after)
	}
	status, err = cloud.Within(ctx, scope, []string{"governance"}, func(tx rt.Tx) error { _, err := gov.ApplyLeaseReportTx(ctx, tx, report.Report); return err })
	if err != nil || status != rt.Committed {
		t.Fatal(err)
	}
	var replay governance.GrantUsage
	if _, err = cloud.Read(ctx, scope, "governance/grant_usage", grant.GrantID, 0, &replay); err != nil || !api.Equal(after, replay) {
		t.Fatal("original lease report replay double charged")
	}
	stop()
	var cloudLease governance.GrantLease
	if _, err = cloud.Read(ctx, scope, "governance/leases", f.b.Lease.LeaseID, 0, &cloudLease); err != nil {
		t.Fatal(err)
	}
	if cloudLease.State != "reconciled" || !cloudLease.SpendingClosed || !cloudLease.UsageFinal {
		t.Fatalf("cloud lease closure: %+v", cloudLease)
	}
	t.Logf("EXECUTOR_POSTGRES_EVIDENCE %s", api.Raw(struct {
		CloudDatabaseID  string                `json:"cloud_database_id"`
		DeviceDatabaseID string                `json:"device_database_id"`
		TaskRef          api.ObjectRef         `json:"task_ref"`
		OperationID      string                `json:"operation_id"`
		AttemptID        string                `json:"attempt_id"`
		GrantRef         api.ObjectRef         `json:"grant_ref"`
		LeaseRef         api.ObjectRef         `json:"lease_ref"`
		Report           SignedLeaseReport     `json:"report"`
		Before           governance.GrantUsage `json:"before"`
		After            governance.GrantUsage `json:"after"`
		CloudTaskFixture bool                  `json:"cloud_task_fixture"`
	}{scope.DatabaseID, f.h.Config.DatabaseID, f.b.Intent.TaskRef, f.b.Intent.OperationID, view.Attempts.Items[0].AttemptID, grantRef, f.b.LeaseRef, report, before, after, true}))
}
