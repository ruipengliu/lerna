package runner_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/development"
	target "github.com/ruipengliu/lerna/adapters/execution"
	device "github.com/ruipengliu/lerna/adapters/executor"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

func channelJobFact(t *testing.T, ctx context.Context, db *sql.DB, scope runtime.Scope, id string) (state string, epoch uint64) {
	t.Helper()
	if err := db.QueryRowContext(ctx, "SELECT state,lease_epoch FROM runtime_jobs WHERE tenant_id=$1 AND owner_id=$2 AND job_id=$3", scope.TenantID, scope.OwnerID, id).Scan(&state, &epoch); err != nil {
		t.Fatal(err)
	}
	return
}

func startClassifiedChannelWorkers(t *testing.T, ctx context.Context, root string, c development.Config, store runtime.Store, scope runtime.Scope) (*channelRoleProcess, *channelRoleProcess) {
	t.Helper()
	// 责任夹具使用公开 runtime.Raise；处理者是两个真实领域 handler，未替换业务方法。
	var index, impact, excluded api.Job
	status, err := store.Within(ctx, scope, []string{"memory", "execution"}, func(tx runtime.Tx) error {
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		index, err = tx.Raise(ctx, "memory.index", scope.OwnerID, scope.Ref(scope.OwnerID, 1), now)
		if err != nil {
			return err
		}
		impact, err = tx.Raise(ctx, "memory.source_impact", scope.OwnerID, scope.Ref(scope.OwnerID, 1), now)
		if err != nil {
			return err
		}
		// 准确未准入的 execution 责任只用于证明类别不被领取，不生成 Operation 或出口。
		operation := api.NewID("operation")
		excluded, err = tx.Raise(ctx, "execution.run", operation, scope.Ref(operation, 1), now)
		return err
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("original classified responsibility fixture: %s %v", status, err)
	}
	db, err := sql.Open("pgx", os.Getenv("HARNESS_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	waitDone := func(id string) {
		t.Helper()
		for deadline := time.Now().Add(70 * time.Second); time.Now().Before(deadline); {
			state, _ := channelJobFact(t, ctx, db, scope, id)
			if state == "done" {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
		t.Fatal("actual classified worker did not finish original job")
	}
	indexConfig := c
	indexConfig.WorkerPool = &development.ClassifiedWorkerConfig{PoolID: api.NewID("pool"), JobKinds: []string{"memory.index"}, Concurrency: 1}
	indexPath := filepath.Join(root, "index-worker.json")
	if err = development.SaveConfig(indexPath, indexConfig); err != nil {
		t.Fatal(err)
	}
	indexProcess := startChannelRole(t, ctx, "worker", indexPath)
	waitDone(index.JobID)
	if state, epoch := channelJobFact(t, ctx, db, scope, impact.JobID); state != "ready" || epoch != 0 {
		t.Fatal("first pool claimed a different category")
	}
	impactConfig := c
	impactConfig.WorkerPool = &development.ClassifiedWorkerConfig{PoolID: api.NewID("pool"), JobKinds: []string{"memory.source_impact"}, Concurrency: 1}
	impactPath := filepath.Join(root, "impact-worker.json")
	if err = development.SaveConfig(impactPath, impactConfig); err != nil {
		t.Fatal(err)
	}
	impactProcess := startChannelRole(t, ctx, "worker", impactPath)
	waitDone(impact.JobID)
	if state, epoch := channelJobFact(t, ctx, db, scope, excluded.JobID); state != "ready" || epoch != 0 {
		t.Fatal("classified pools claimed an execution responsibility")
	}
	for _, dir := range []string{"files", "phones"} {
		if err = os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	files, err := target.NewManagedFiles(filepath.Join(root, "files"))
	if err != nil {
		t.Fatal("cloud gateway/applications/classified pools stole file target lock", err)
	}
	if err = files.Close(); err != nil {
		t.Fatal(err)
	}
	phones, err := target.NewSimulatedPhones(filepath.Join(root, "phones"), nil)
	if err != nil {
		t.Fatal("cloud gateway/applications/classified pools stole phone target lock", err)
	}
	if err = phones.Close(); err != nil {
		t.Fatal(err)
	}
	indexState, indexEpoch := channelJobFact(t, ctx, db, scope, index.JobID)
	impactState, impactEpoch := channelJobFact(t, ctx, db, scope, impact.JobID)
	t.Logf("PUBLIC_CLASSIFIED_WORKER_EVIDENCE %s", api.Raw(struct {
		IndexPool, ImpactPool             string
		IndexJob, ImpactJob               api.Job
		ExcludedExecutionJob              api.Job
		IndexState, ImpactState           string
		IndexLeaseEpoch, ImpactLeaseEpoch uint64
		TargetLocksAvailable              bool
	}{indexConfig.WorkerPool.PoolID, impactConfig.WorkerPool.PoolID, index, impact, excluded, indexState, impactState, indexEpoch, impactEpoch, true}))
	return indexProcess, impactProcess
}

func startIndependentChannelExecutor(t *testing.T, ctx context.Context, root string, c development.Config, store runtime.Store, scope runtime.Scope, tlsFiles development.EndpointTLSFiles) (*device.Client, *channelRoleProcess) {
	t.Helper()
	keys, err := platform.NewDevelopmentKey(scope.TenantID, scope.OwnerID, []string{"grant_lease", "grant_use", "control", "executor_admission"})
	if err != nil {
		t.Fatal(err)
	}
	var jwk struct {
		Kty string `json:"kty"`
		Crv string `json:"crv"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}
	if err = api.Decode(platform.PublicJWK(keys.Keys["development-es256"].Public), &jwk); err != nil {
		t.Fatal(err)
	}
	peerFile := filepath.Join(root, ".executor-peer-token")
	if err = os.WriteFile(peerFile, []byte("finite-static-channel-fixture-peer-without-user-credentials"), 0600); err != nil {
		t.Fatal(err)
	}
	capability := target.FileWriteCapability().Ref
	binding := scope.Ref(api.NewID("binding"), 1)
	lock := api.ComponentRef{ComponentID: api.NewID("component"), Version: "1.0.0", Digest: api.Hash([]byte("channel-independent-file-fixture-lock"))}
	dc := device.Config{Development: true, TenantID: scope.TenantID, OwnerID: api.NewID("executor"), InstanceID: api.NewID("instance"), DatabasePath: filepath.Join(root, "executor.sqlite"), DataRoot: root, SigningKeyFile: filepath.Join(root, ".executor-signing-key.pem"), PeerTokenFile: peerFile, Authority: device.TrustedAuthority{KeyID: "development-es256", OwnerID: scope.OwnerID, PublicX: jwk.X, PublicY: jwk.Y}, Bindings: []device.Binding{{CapabilityRef: capability, BindingRef: binding, InstallLockRef: lock, Resources: []string{"managed-files"}, Actions: []string{"file.write"}}}, GRPCAddr: freeChannelAddress(t), TLSCertificateFile: tlsFiles.CertificateFile, TLSKeyFile: tlsFiles.KeyFile}
	configPath := filepath.Join(root, "executor.json")
	if err = device.SaveConfig(configPath, dc); err != nil {
		t.Fatal(err)
	}
	_, diagnostic, err := run(t, binary(t, "executor"), "setup", "--config", configPath)
	if err != nil {
		t.Fatalf("actual independent SQLite setup: %v %s", err, diagnostic)
	}
	dc, err = device.LoadConfig(configPath)
	if err != nil || dc.DatabaseID == scope.DatabaseID {
		t.Fatal("device did not retain its independent SQLite identity", err)
	}
	process := startChannelRole(t, ctx, "executor", configPath)
	client, err := device.Dial(ctx, device.RemoteConfig{DatabaseID: dc.DatabaseID, Endpoint: "grpcs://" + dc.GRPCAddr, OwnerID: dc.OwnerID, InstanceID: dc.InstanceID, AuthorityID: scope.OwnerID, TenantID: scope.TenantID, TLSCAFile: tlsFiles.CAFile, PeerTokenFile: peerFile, JournalRoot: filepath.Join(root, "cloud-executor-journal")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	operationID := api.NewID("operation")
	statusRef := api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte("not-staged-readiness-reference")), MediaType: "text/plain", ByteLength: 30}
	ready := false
	for deadline := time.Now().Add(70 * time.Second); time.Now().Before(deadline); {
		query := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: dc.OwnerID, QueryID: api.NewID("query"), Method: "executor.content.status", TargetID: statusRef.ContentID, Payload: api.Raw(device.ContentID{ContentRef: statusRef})}
		if _, err = client.SDK.Query(ctx, query); api.IsCode(err, "not_found") {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("actual independent executor TLS not ready", err)
	}
	if _, err = client.Get(ctx, operationID); !api.IsCode(err, "forbidden") {
		t.Fatal("unadmitted cloud object was exposed on the device", err)
	}
	if files, err := target.NewManagedFiles(filepath.Join(root, "files")); err == nil {
		files.Close()
		t.Fatal("actual executor process did not own its physical target lock")
	} else if !api.IsCode(err, "invalid_state") {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(root, "files", "reports"), 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cut := api.Time(now.Add(2 * time.Minute))
	bytes := []byte("public channel and classified pools; independent executor physical bytes\n")
	ref := func(body []byte, media string) api.ContentRef {
		return api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(body), MediaType: media, ByteLength: uint64(len(body))}
	}
	dataRef := ref(bytes, "text/plain")
	arguments := api.Raw(target.FileWriteArguments{Path: "reports/channel-executor.txt", ExpectedVersion: "absent", ContentRef: dataRef})
	argumentRef := ref(arguments, "application/json")
	intent := execution.ExecutionIntent{OperationID: operationID, TaskRef: scope.Ref(api.NewID("task"), 1), GoalRevision: 1, ControlRevision: 1, AdmissionSourceKind: "decision", AdmissionSourceRef: scope.Ref(api.NewID("admission"), 1), SourcePosition: "0", AdmissionPurpose: "goal_action", CapabilityRef: capability, BindingRef: binding, InstallLockRef: lock, ArgumentsRef: argumentRef, ProcessedSourceRefs: []api.ContentRef{dataRef}, DisclosedSourceRefs: []api.ContentRef{}, ResourceRefs: []api.ObjectRef{}, RequirementRefs: []api.RequirementRef{}, CostBound: []api.Amount{{Unit: "USD", Value: "1"}}, ExecutorID: dc.OwnerID, Deadline: cut, TaskDeadline: cut, LogicalStepKey: "channel-file-save"}
	intentBytes := api.Raw(intent)
	intentRef := ref(intentBytes, "application/json")
	executionHash, err := api.Digest(intent)
	if err != nil {
		t.Fatal(err)
	}
	admissionHash := api.Hash(api.Raw(struct {
		TaskRef api.ObjectRef
		Intent  execution.ExecutionIntent
	}{intent.TaskRef, intent}))
	principal := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service", "orchestrator"}}
	user := runtime.Auth{TenantID: scope.TenantID, SubjectID: c.SubjectID, CredentialGeneration: 1, Roles: []string{"grant_authority"}}
	grantID, leaseID := api.NewID("grant"), api.NewID("lease")
	proof := device.Proof{Keys: keys, SigningKeyID: "development-es256"}
	gov := governance.New(store, governance.Options{Proof: proof})
	policyValues := memory.PolicyValues{Subjects: []string{scope.OwnerID, c.SubjectID}, Purposes: []string{"execution_intent", "execution_arguments", "managed_file_write", "managed_file_read"}, Locations: []string{"cloud", "device"}, RetainUntil: cut, Continuous: true, IndependentDerived: false}
	policyDigest, err := api.Digest(policyValues)
	if err != nil {
		t.Fatal(err)
	}
	policy := memory.Policy{PolicyRef: api.ComponentRef{ComponentID: api.NewID("component"), Version: "1.0.0", Digest: policyDigest}, Revision: 1, State: "active", Values: policyValues}
	permission := func(r api.ContentRef, purposes []string, sources []api.ContentRef) device.ContentPermission {
		return device.ContentPermission{ContentRef: r, Purposes: purposes, ProcessedSources: sources, DisclosedSources: []api.ContentRef{}, RetainUntil: cut, SourcePolicy: &policy, SubjectRefs: []api.ObjectRef{principal.Ref(scope.OwnerID), user.Ref(scope.OwnerID)}}
	}
	b := device.AdmissionBundle{BundleID: api.NewID("bundle"), Revision: 1, AuthorityID: scope.OwnerID, EndpointID: dc.OwnerID, DeviceDatabaseID: dc.DatabaseID, InstanceID: dc.InstanceID, OriginalCommandID: api.NewID("command"), AdmissionHash: admissionHash, ExecutionHash: executionHash, IntentRef: intentRef, ReservationRef: scope.Ref(api.NewID("reservation"), 1), Intent: intent, Principal: device.PrincipalOf(principal), LeaseRef: scope.Ref(leaseID, 1), UseRefs: []api.ObjectRef{scope.Ref(leaseID, 1)}, IssuedAt: api.Time(now), StartBefore: cut, Contents: []device.ContentPermission{permission(intentRef, []string{"execution_intent"}, []api.ContentRef{dataRef}), permission(argumentRef, []string{"execution_arguments"}, []api.ContentRef{dataRef}), permission(dataRef, []string{"managed_file_write", "managed_file_read"}, []api.ContentRef{})}}
	status, err := store.Within(ctx, scope, []string{"governance", device.Namespace}, func(tx runtime.Tx) error {
		// 明确受信预批准夹具；此条不冒充公开 Task 决策、用户 Confirmation 或来源登记。
		if err := gov.ProvisionGrantTx(ctx, tx, user, api.Grant{GrantID: grantID, OwnerID: scope.OwnerID, Revision: 1, SubjectRef: user.Ref(scope.OwnerID), Resources: []string{"managed-files"}, Actions: []string{"file.write"}, Purposes: []string{"save"}, Recipients: []string{dc.OwnerID}, Locations: []string{"device"}, Mode: "once", State: "active", NotBefore: api.Time(now.Add(-time.Second)), ExpiresAt: cut, Limits: intent.CostBound}); err != nil {
			return err
		}
		use := governance.UseRequest{UseID: leaseID, SubjectRef: user.Ref(scope.OwnerID), TargetRef: api.ObjectRef{TenantID: scope.TenantID, OwnerID: dc.OwnerID, ObjectID: operationID, Revision: 1}, TargetKind: "operation", IntentHash: admissionHash, Resources: []string{"managed-files"}, Actions: []string{"file.write"}, Recipient: dc.OwnerID, Location: "device", Purposes: []string{"save"}, RequestedUnits: intent.CostBound, StartBefore: cut, GrantRefs: []api.ObjectRef{scope.Ref(grantID, 1)}}
		var err error
		b.Lease, err = gov.AllocateLeaseTx(ctx, tx, user, governance.LeaseAllocate{LeaseID: leaseID, EndpointID: dc.OwnerID, InstanceID: dc.InstanceID, Scope: use, Limits: intent.CostBound, ExpiresAt: cut, CostMode: "strict"})
		if err != nil {
			return err
		}
		b.IssuedAt, b.StartBefore = b.Lease.IssuedAt, b.Lease.ExpiresAt
		b, err = device.SealAdmissionTx(ctx, tx, proof, b)
		return err
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("actual finite cloud reservation: %s %v", status, err)
	}
	bodies := map[string][]byte{dataRef.ContentID: bytes, argumentRef.ContentID: arguments, intentRef.ContentID: intentBytes}
	if err = client.Prepare(ctx, b, func(_ context.Context, p device.ContentPermission) ([]byte, error) {
		return bodies[p.ContentRef.ContentID], nil
	}); err != nil {
		t.Fatal(err)
	}
	now = time.Now()
	control := api.ControlSnapshot{TaskID: intent.TaskRef.ObjectID, OrchestratorID: scope.OwnerID, GoalRevision: 1, ControlRevision: 1, Status: "active", Control: "running", WindowID: api.NewID("window"), IssuedAt: api.Time(now), StartBefore: api.Time(now.Add(5 * time.Second))}
	controlDigest, err := api.Digest(control)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := keys.Sign("development-es256", platform.ProofClaims{TenantID: scope.TenantID, Issuer: scope.OwnerID, Audience: scope.OwnerID, Purpose: "control", ObjectRef: intent.TaskRef, Digest: controlDigest, ControlRevision: 1, WindowID: control.WindowID, IssuedAt: control.IssuedAt, StartBefore: control.StartBefore})
	if err != nil {
		t.Fatal(err)
	}
	control.ProofRef = ref([]byte(compact), "application/jose")
	invoke := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: dc.OwnerID, CommandID: b.OriginalCommandID, Method: "execution.invoke", TargetID: operationID, ExpiresAt: cut, Payload: api.Raw(execution.InvokeInput{OperationID: operationID, TaskRef: intent.TaskRef, GoalRevision: 1, ControlRevision: 1, CapabilityRef: capability, BindingRef: binding, IntentRef: intentRef, IntentHash: executionHash, UseRefs: b.UseRefs, Deadline: cut, ControlSnapshot: control, ReservationRef: b.ReservationRef})}
	receipt, err := client.Dispatch(ctx, invoke, device.ControlDelivery{Snapshot: control, Compact: compact})
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("actual independent invoke: %+v %v", receipt, err)
	}
	var view execution.OperationView
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		view, err = client.Get(ctx, operationID)
		if err == nil && view.Operation.ExecutionState == "closed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || view.Operation.Effect != "applied" || len(view.Attempts.Items) != 1 || view.Operation.MayApplyLater != false {
		t.Fatalf("actual independent original attempt: %+v %v", view, err)
	}
	actual, err := os.ReadFile(filepath.Join(root, "files", "reports", "channel-executor.txt"))
	if err != nil || string(actual) != string(bytes) {
		t.Fatal("independent physical read-back did not match exact bytes", err)
	}
	report, err := client.LeaseUsage(ctx, leaseID)
	if err != nil || !report.Report.Usage.UsageFinal {
		t.Fatal("original independent lease usage did not close", err)
	}
	binaryBytes, err := os.ReadFile(binary(t, "executor"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("PUBLIC_CLASSIFIED_EXECUTOR_EVIDENCE %s", api.Raw(struct {
		CloudDatabaseID, DeviceDatabaseID, ExecutorID, OperationID, CommandID, OriginalTTL, AttemptID, BinarySHA256, BytesHash string
		TaskRef                                                                                                                api.ObjectRef
		LeaseRef                                                                                                               api.ObjectRef
		Usage                                                                                                                  api.UsageSnapshot
		PreapprovedAuthorityFixture, PublicCloudTask                                                                           bool
	}{scope.DatabaseID, dc.DatabaseID, dc.OwnerID, operationID, invoke.CommandID, invoke.ExpiresAt, view.Attempts.Items[0].AttemptID, api.Hash(binaryBytes), api.Hash(actual), intent.TaskRef, b.LeaseRef, report.Report.Usage, true, false}))
	return client, process
}
