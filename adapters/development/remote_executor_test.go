package development

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/executor"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 公开 Cloud Task 原准入→真实 TLS→独立 SQLite/真实文件，不预置 Task/lease/use。
func TestConfiguredRemoteExecutorTaskReadsOriginalDeviceBytesAndSettlesOnce(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runRemoteExecutorTask(t, driver, false, false)
		})
	}
}

func TestConfiguredRemoteExecutorPublishesVerifiedTaskResultAndReopensOriginal(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runRemoteExecutorTask(t, driver, true, false)
		})
	}
}

func runRemoteExecutorTask(t *testing.T, driver string, complete, saveReport bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), reportFixtureTimeout)
	defer cancel()
	root := t.TempDir()
	keepFixture := os.Getenv("HARNESS_TEST_REMOTE_FIXTURE_ROOT")
	if keepFixture != "" {
		if !filepath.IsAbs(keepFixture) {
			t.Fatal("private fixture root must be absolute")
		}
		if err := os.MkdirAll(keepFixture, 0700); err != nil {
			t.Fatal(err)
		}
		var err error
		root, err = os.MkdirTemp(keepFixture, "remote-task-")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("preserved private remote fixture: %s", root)
	}
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	if err = SaveConfig(filepath.Join(root, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	cloud, err := OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	var authority struct {
		X   string `json:"x"`
		Y   string `json:"y"`
		KTY string `json:"kty"`
		CRV string `json:"crv"`
	}
	if err = api.Decode(platform.PublicJWK(cloud.Keys.Keys["development-es256"].Public), &authority); err != nil {
		t.Fatal(err)
	}
	if err = cloud.Close(); err != nil {
		t.Fatal(err)
	}
	deviceRoot := t.TempDir()
	if keepFixture != "" {
		deviceRoot = filepath.Join(root, "device")
		if err = os.MkdirAll(deviceRoot, 0700); err != nil {
			t.Fatal(err)
		}
	}
	ca, cert, key := remoteTestTLS(t, deviceRoot)
	deviceOwner, instance := api.NewID("owner"), api.NewID("instance")
	binding := api.ObjectRef{TenantID: cfg.TenantID, OwnerID: deviceOwner, ObjectID: api.NewID("binding"), Revision: 1}
	deviceBinding := executor.Binding{CapabilityRef: target.FileReadCapability().Ref, BindingRef: binding, InstallLockRef: component("builtin-install-lock"), Resources: []string{"managed-files"}, Actions: []string{"file.read"}}
	deviceBindings := []executor.Binding{deviceBinding}
	writeBinding := api.ObjectRef{TenantID: cfg.TenantID, OwnerID: deviceOwner, ObjectID: api.NewID("binding"), Revision: 1}
	if saveReport {
		deviceBindings = append(deviceBindings, executor.Binding{CapabilityRef: target.FileWriteCapability().Ref, BindingRef: writeBinding, InstallLockRef: deviceBinding.InstallLockRef, Resources: []string{"managed-files"}, Actions: []string{"file.write"}})
	}
	peerToken := filepath.Join(deviceRoot, "peer-token")
	if err = os.WriteFile(peerToken, []byte("synthetic-explicit-device-peer-token-for-contract-test"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	dc := executor.Config{Development: true, TenantID: cfg.TenantID, OwnerID: deviceOwner, InstanceID: instance, DatabasePath: filepath.Join(deviceRoot, "device.sqlite"), DataRoot: deviceRoot, SigningKeyFile: filepath.Join(deviceRoot, "device-key.pem"), PeerTokenFile: peerToken, Authority: executor.TrustedAuthority{KeyID: "development-es256", OwnerID: cfg.OwnerID, PublicX: authority.X, PublicY: authority.Y}, Bindings: deviceBindings, GRPCAddr: address, TLSCertificateFile: cert, TLSKeyFile: key, OutputSubjectRefs: []api.ObjectRef{{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, ObjectID: cfg.OwnerID, Revision: 1}, {TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, ObjectID: cfg.SubjectID, Revision: 1}}, OutputPurposes: []string{"execution_result", "content.read", "content.write", "task.context", "task.snapshot", "task.dispatch", "task.action", "brain.input", "brain.output", "task.evidence", "task.attach_evidence", "task.complete", "task.goal", "task.result", "result", "memory.save", "memory.read", "memory.query"}, OutputLocations: []string{"cloud", "device"}}
	if saveReport {
		// 后继参数确实派生自原设备写入事实；宿主读取与设备传送用途分别登记。
		dc.OutputPurposes = append(dc.OutputPurposes, "managed_file_read", "managed_file_write", "execution_arguments", "execution.arguments", "execution_intent")
	}
	device, err := executor.Open(ctx, dc, true)
	if err != nil {
		t.Fatal(err)
	}
	dc.DatabaseID = device.Scope.DatabaseID
	if err = privateFile(filepath.Join(deviceRoot, "config.json"), append(api.Raw(dc), '\n')); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if device != nil {
			if err := device.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	const original = "Actual independent device bytes; the cloud owns no target."
	goalBody := "knowledge contract"
	wantStatus := "failed"
	if complete {
		goalBody, wantStatus = original, "succeeded"
	}
	goalSpec := brain.GoalSpec{Kind: "answer", Body: goalBody}
	expectedPosts, expectedSpent := int32(3), "0.00072"
	if saveReport {
		goalSpec = brain.GoalSpec{Kind: "report", Title: "Device report", Body: "The original cloud Task saved this exact report on its paired device.", SavePath: "remote-report.md"}
		expectedPosts, expectedSpent = 4, "0.00096"
	}
	if err = os.WriteFile(filepath.Join(deviceRoot, "files", "remote-original.txt"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	deviceCtx, deviceCancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- device.Run(deviceCtx) }()
	defer func() {
		if done == nil {
			return
		}
		deviceCancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(10 * time.Second):
			t.Error("independent device did not actually exit")
		}
	}()
	roots := x509.NewCertPool()
	caBytes, err := os.ReadFile(ca)
	if err != nil || !roots.AppendCertsFromPEM(caBytes) {
		t.Fatal("paired TLS CA unavailable", err)
	}
	for {
		conn, e := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", address, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots})
		if e == nil {
			if e = conn.Close(); e != nil {
				t.Fatal(e)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("actual device TLS readiness", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	var public struct {
		X   string `json:"x"`
		Y   string `json:"y"`
		KTY string `json:"kty"`
		CRV string `json:"crv"`
	}
	if err = api.Decode(platform.PublicJWK(device.Keys.Keys["device-es256"].Public), &public); err != nil {
		t.Fatal(err)
	}
	cfg.RemoteExecutors = []RemoteExecutorConfig{{OwnerID: deviceOwner, DatabaseID: device.Scope.DatabaseID, InstanceID: instance, Endpoint: "grpcs://" + address, TLSCAFile: ca, PeerTokenFile: peerToken, PublicKeyID: "device-es256", PublicX: public.X, PublicY: public.Y, Bindings: deviceBindings}}
	scope := runtime.Scope{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, DatabaseID: cfg.DatabaseID}
	cfg.ActionBindings = []ActionBindingConfig{{CapabilityRef: target.FileReadCapability().Ref, BindingRef: binding, InstallLockRef: deviceBinding.InstallLockRef, Grant: api.Grant{GrantID: api.NewID("grant"), OwnerID: cfg.OwnerID, Revision: 1, SubjectRef: scope.Ref(cfg.OwnerID, 1), Resources: []string{"managed-files"}, Actions: []string{"file.read"}, Purposes: []string{"goal_action"}, Recipients: []string{deviceOwner}, Locations: []string{"device"}, Mode: "once", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: cfg.PolicyExpiresAt, Limits: []api.Amount{{Unit: "USD", Value: "1"}}}}}
	if saveReport {
		grant := cfg.ActionBindings[0].Grant
		grant.GrantID, grant.Actions = api.NewID("grant"), []string{"file.write"}
		cfg.ActionBindings = append(cfg.ActionBindings, ActionBindingConfig{CapabilityRef: target.FileWriteCapability().Ref, BindingRef: writeBinding, InstallLockRef: deviceBinding.InstallLockRef, Grant: grant})
	}
	var posts atomic.Int32
	var active atomic.Pointer[App]
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		post := posts.Add(1)
		var wire struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		var input struct {
			Snapshot  api.Snapshot `json:"snapshot"`
			Materials []struct {
				Ref  api.ContentRef `json:"ref"`
				Body string         `json:"body_utf8"`
			} `json:"materials"`
		}
		if json.NewDecoder(r.Body).Decode(&wire) != nil || len(wire.Messages) != 2 || json.Unmarshal([]byte(wire.Messages[1].Content), &input) != nil {
			t.Error("original model Snapshot missing")
			return
		}
		reply := knowledgeContractReply()
		if saveReport {
			reply = remoteReportModelReply(t, active.Load(), input.Snapshot, input.Materials, goalSpec, binding, writeBinding, post)
		} else if input.Snapshot.Purpose == "interpret_requirements" {
			reply = knowledgeRefinementReply(active.Load().ArtifactRule)
			if complete {
				reply = remoteRefinementReply(active.Load().ArtifactRule, goalBody)
			}
		} else if post == 2 {
			declared := false
			for n, cap := range input.Snapshot.CapabilityRefs {
				if api.Equal(cap, target.FileReadCapability().Ref) && n < len(input.Snapshot.BindingRefs) && api.Equal(input.Snapshot.BindingRefs[n], binding) {
					declared = true
				}
			}
			if !declared {
				t.Error("explicit device cap/binding not in current Snapshot")
				return
			}
			g := brain.Generated{Draft: brain.Draft{Kind: "act", ReasonLocalID: "reason", Actions: []brain.DraftAction{{LocalKey: "remote_read", CapabilityRef: target.FileReadCapability().Ref, BindingRef: binding, ArgumentsLocalID: "args"}}}, Contents: []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "Read the original fixed device; do not decide Task success.", DisclosedSources: []api.ContentRef{}}, {LocalID: "args", MediaType: "application/json", Body: string(api.Raw(target.FileReadArguments{Path: "remote-original.txt"})), DisclosedSources: []api.ContentRef{}}}}
			output := struct {
				Draft    brain.Draft              `json:"draft"`
				Contents []brain.GeneratedContent `json:"contents"`
			}{g.Draft, g.Contents}
			reply = api.Raw(map[string]any{"id": "remote-original-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(output))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}})
		} else if complete && post == 3 {
			var observed *target.FileReadResult
			var source api.ContentRef
			for _, material := range input.Materials {
				var read target.FileReadResult
				if material.Ref.OwnerID == deviceOwner && api.Decode([]byte(material.Body), &read) == nil && read.Path == "remote-original.txt" {
					observed, source = &read, material.Ref
				}
			}
			if observed == nil || observed.DataBase64 != base64.StdEncoding.EncodeToString([]byte(original)) {
				t.Error("model did not receive the original actual device output bytes")
				return
			}
			body, err := base64.StdEncoding.Strict().DecodeString(observed.DataBase64)
			if err != nil {
				t.Error(err)
				return
			}
			generated := brain.Generated{Draft: brain.Draft{Kind: "complete", ReasonLocalID: "reason", ArtifactLocalIDs: []string{"artifact"}}, Contents: []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "The original read bytes are proposed; Task checks the exact requirement independently.", DisclosedSources: []api.ContentRef{}}, {LocalID: "artifact", MediaType: "text/plain", Body: string(body), DisclosedSources: []api.ContentRef{source}}}}
			reply = api.Raw(map[string]any{"id": "remote-complete-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(map[string]any{"draft": generated.Draft, "contents": generated.Contents}))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}})
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(reply)
	}))
	defer model.Close()
	t.Setenv("HARNESS_CONTRACT_MODEL_CREDENTIAL", "synthetic-contract-token")
	cfg.Model = contractModelConfig(model.URL)
	if err = SaveConfig(filepath.Join(root, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	a, err := OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatalf("explicit remote public Task assembly unavailable: %v", err)
	}
	defer func() {
		if a != nil {
			if e := a.Close(); e != nil {
				t.Error(e)
			}
		}
	}()
	active.Store(a)
	var progressStore *remoteProgressStore
	if saveReport {
		progressStore = &remoteProgressStore{Store: a.Store, QueryBindingStore: a.Store.(runtime.QueryBindingStore)}
		a.Store = progressStore
	}
	goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", api.Raw(goalSpec), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	taskID := api.NewID("task")
	knowledgePublicCommand(t, ctx, a, "task.submit", taskID, task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}, RequirementCandidates: []api.RequirementCandidate{}}, nil)
	if err = privateFile(filepath.Join(root, "original-task.json"), api.Raw(struct {
		Scope  runtime.Scope `json:"scope"`
		TaskID string        `json:"task_id"`
	}{a.Scope, taskID})); err != nil {
		t.Fatal(err)
	}
	nextProgress := time.Now().Add(10 * time.Second)
	for {
		if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 300); err != nil {
			var refusal *api.Error
			// 原账单查询在短事务重新核源 head；实际 worker 会让同 Job
			// 沿原 Claim 到期恢复，不把这次已回滚冲突当成新行动或零费用。
			if errors.As(err, &refusal) && refusal.Code == "revision_conflict" && refusal.Reason == "device_usage_source_advanced" {
				t.Logf("original billing Job remains recoverable after source head advanced: %v", err)
			} else {
				if progressStore != nil {
					recordRemoteProgressRefusal(t, ctx, a, root, taskID, posts.Load(), progressStore, err)
				}
				t.Fatal("actual remote public Task progress", err)
			}
		}
		if progressStore != nil {
			if refusal := progressStore.firstRefusal(); refusal != nil {
				recordRemoteProgressRefusal(t, ctx, a, root, taskID, posts.Load(), progressStore, nil)
				t.Fatalf("original remote Task admission refused before independent readback: %s", refusal.Cause)
			}
		}
		got, e := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
		if e != nil {
			t.Fatal(e)
		}
		if time.Now().After(nextProgress) {
			t.Logf("public Task progress: status=%s requirements=%s accounting_open=%t posts=%d", got.Status, got.RequirementsState, got.AccountingOpen, posts.Load())
			progress, readErr := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
			t.Logf("public Task context facts: operations=%d checks=%d error=%v", len(progress.Operations), len(progress.Checks), readErr)
			for _, check := range progress.Checks {
				t.Logf("public condition result: id=%s verdict=%s applicability=%s", check.CheckID, check.Verdict, check.Applicability)
			}
			if readErr == nil && len(progress.Operations) == 1 {
				fact := progress.Operations[0]
				t.Logf("public remote fact: effect=%s closed=%t", fact.Fact.Effect, fact.Fact.Closed)
			}
			nextProgress = time.Now().Add(10 * time.Second)
		}
		if got.Status == wantStatus && !got.AccountingOpen {
			if posts.Load() != expectedPosts || got.Budget[0].Spent != expectedSpent || got.Budget[0].Reserved != "0" {
				t.Fatalf("original known ledger changed: posts=%d %+v", posts.Load(), got)
			}
			break
		}
		if complete && (got.Status == "failed" || got.Status == "cancelled") {
			t.Fatalf("original remote Task did not succeed: %+v", got)
		}
		select {
		case <-ctx.Done():
			t.Fatal("remote Task did not close", ctx.Err(), got)
		case <-time.After(20 * time.Millisecond):
		}
	}
	facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
	wantedOperations := 1
	if saveReport {
		wantedOperations = 2
	}
	if err != nil || len(facts.Operations) != wantedOperations {
		t.Fatalf("real remote fact missing: %v %+v", err, facts)
	}
	readIndex := -1
	for n, fact := range facts.Operations {
		if fact.Intent.ExecutorID != deviceOwner || !fact.Fact.Closed || fact.Fact.MayApplyLater {
			t.Fatalf("actual device effect did not close: %+v", fact)
		}
		if api.Equal(fact.Intent.CapabilityRef, target.FileReadCapability().Ref) {
			if fact.Fact.Effect != "not_applied" {
				t.Fatalf("original device read effect changed: %+v", fact)
			}
			readIndex = n
		} else if !saveReport || !api.Equal(fact.Intent.CapabilityRef, target.FileWriteCapability().Ref) || fact.Fact.Effect != "applied" {
			t.Fatalf("original device write not applied: %+v", fact)
		}
	}
	if readIndex < 0 {
		t.Fatal("actual independent device read missing")
	}
	op := facts.Operations[readIndex].Intent.OperationID
	client, err := executor.Dial(ctx, executor.RemoteConfig{DatabaseID: device.Scope.DatabaseID, Endpoint: "grpcs://" + address, OwnerID: deviceOwner, InstanceID: instance, AuthorityID: cfg.OwnerID, TenantID: cfg.TenantID, TLSCAFile: ca, PeerTokenFile: peerToken, JournalRoot: filepath.Join(root, "inspect-device-journal")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	}()
	view, err := client.Get(ctx, op)
	if err != nil || len(view.Attempts.Items) != 1 || !view.ActuallyStopped || view.Operation.ResultRef == nil {
		t.Fatalf("original device Attempt not actually final: %v %+v", err, view)
	}
	bytes, err := a.ReadContent(ctx, a.Scope, a.UserAuth, *view.Operation.ResultRef, "execution_result")
	var read target.FileReadResult
	wantedBytes := []byte(original)
	if saveReport {
		wantedBytes = []byte(originalRemoteReport)
	}
	if err != nil || api.Decode(bytes, &read) != nil || read.DataBase64 != base64.StdEncoding.EncodeToString(wantedBytes) {
		t.Fatalf("original foreign result bytes unavailable: %v %+v", err, read)
	}
	if saveReport {
		verifyRemoteReportTarget(t, ctx, a, device, client, facts, goalSpec, read)
	}
	if view.Operation.ResultRef.OwnerID != deviceOwner || view.Operation.TaskRef.TenantID != cfg.TenantID {
		t.Fatal("device original ownership was rewritten")
	}
	intent, err := a.Task.ReadOperationIntent(ctx, a.Store, a.Scope, a.UserAuth, op)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("remote_original_refs %s", api.Raw(struct {
		CloudScope       runtime.Scope   `json:"cloud_scope"`
		DeviceScope      runtime.Scope   `json:"device_scope"`
		TaskRef          api.ObjectRef   `json:"task_ref"`
		CommandID        string          `json:"command_id"`
		OperationID      string          `json:"operation_id"`
		AttemptID        string          `json:"attempt_id"`
		ControlWindowID  string          `json:"control_window_id"`
		UseRefs          []api.ObjectRef `json:"use_refs"`
		ReservationRef   api.ObjectRef   `json:"reservation_ref"`
		ResultRef        api.ContentRef  `json:"result_ref"`
		OriginalDeadline string          `json:"original_deadline"`
	}{a.Scope, device.Scope, intent.TaskRef, intent.CommandID, op, view.Attempts.Items[0].AttemptID, view.Attempts.Items[0].ControlWindowID, intent.UseIntentRefs, intent.ReservationRef, *view.Operation.ResultRef, intent.Deadline}))
	if !complete {
		verifyForeignFlowIsolation(t, ctx, a, *view.Operation.ResultRef)
	}
	if complete {
		var result task.ResultOutput
		for {
			result, err = a.Task.Result(ctx, a.Store, a.Scope, a.UserAuth, taskID, task.ResultInput{})
			if err != nil {
				t.Fatal(err)
			}
			if result.Publication == "published" {
				break
			}
			if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 300); err != nil {
				t.Fatal(err)
			}
			select {
			case <-ctx.Done():
				t.Fatal("original verified Result was not published", ctx.Err())
			case <-time.After(20 * time.Millisecond):
			}
		}
		if result.ContentRef == nil || result.Result.CompletionBasis != "verified" || len(result.Result.ConditionResults) != wantedOperations {
			t.Fatalf("device fact replaced verified Task Result: %+v", result)
		}
		for _, check := range result.Result.ConditionResults {
			if check.Verdict != "pass" || check.Basis != "verified" {
				t.Fatalf("original independent check not verified: %+v", check)
			}
		}
		published, e := a.ReadContent(ctx, a.Scope, a.UserAuth, *result.ContentRef, "task.result")
		var exported api.Result
		if e != nil || api.Decode(published, &exported) != nil || !api.Equal(exported, result.Result) {
			t.Fatalf("original Result bytes unavailable: %v", e)
		}
		t.Logf("remote_verified_result_refs %s", api.Raw(struct {
			TaskID     string         `json:"task_id"`
			ResultRef  api.ObjectRef  `json:"result_ref"`
			ContentRef api.ContentRef `json:"content_ref"`
		}{taskID, a.Scope.Ref(result.Result.ResultID, result.Result.Revision), *result.ContentRef}))
		if err = a.Close(); err != nil {
			t.Fatal(err)
		}
		a = nil
		a, err = OpenApp(ctx, cfg, false)
		if err != nil {
			t.Fatal(err)
		}
		active.Store(a)
		if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 500); err != nil {
			t.Fatal(err)
		}
		after, e := a.Task.Result(ctx, a.Store, a.Scope, a.UserAuth, taskID, task.ResultInput{})
		if e != nil || !api.Equal(after, result) || posts.Load() != expectedPosts {
			t.Fatalf("reopen replaced original Result or model call: %v %+v", e, after)
		}
		deviceCancel()
		select {
		case e := <-done:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("original device did not actually exit before reopening")
		}
		done = nil
		if err = device.Close(); err != nil {
			t.Fatal(err)
		}
		device, err = executor.Open(ctx, dc, false)
		if err != nil {
			t.Fatal(err)
		}
		reopenedCtx, reopenedCancel := context.WithCancel(ctx)
		defer reopenedCancel()
		deviceCancel = reopenedCancel
		done = make(chan error, 1)
		go func() { done <- device.Run(reopenedCtx) }()
		for {
			conn, e := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", address, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots})
			if e == nil {
				if e = conn.Close(); e != nil {
					t.Fatal(e)
				}
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("reopened original device TLS readiness", ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
		afterOp, e := client.Get(ctx, op)
		if e != nil || len(afterOp.Attempts.Items) != 1 || !api.Equal(afterOp.Operation.ResultRef, view.Operation.ResultRef) {
			t.Fatalf("reopen changed original device Attempt: %v", e)
		}
		if saveReport {
			verifyRemoteReportTarget(t, ctx, a, device, client, facts, goalSpec, read)
		}
	}
}

func remoteRefinementReply(rule api.ComponentRef, expected string) []byte {
	generated := brain.Generated{Draft: brain.Draft{Kind: "refine_requirements", ReasonLocalID: "reason", Requirements: []brain.DraftRequirement{{CandidateKey: "original_device_bytes", Kind: "quality", StatementLocalID: "statement", ParametersLocalID: "parameters", RuleRef: rule, Required: true}}}, Contents: []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "Preserve the exact original answer before device admission.", DisclosedSources: []api.ContentRef{}}, {LocalID: "statement", MediaType: "text/plain", Body: "The answer must equal the original actual device bytes.", DisclosedSources: []api.ContentRef{}}, {LocalID: "parameters", MediaType: "application/json", Body: string(api.Raw(brain.RuleParameters{Kind: "answer", ExpectedHash: api.Hash([]byte(expected)), ExpectedLength: uint64(len(expected))})), DisclosedSources: []api.ContentRef{}}}}
	return api.Raw(map[string]any{"id": "remote-refinement-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(map[string]any{"draft": generated.Draft, "contents": generated.Contents}))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}})
}

func remoteTestTLS(t *testing.T, root string) (string, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Explicit development device CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(root, "ca.pem"), filepath.Join(root, "tls.pem"), filepath.Join(root, "tls-key.pem")}
	for n, b := range [][]byte{pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: priv})} {
		if err = os.WriteFile(paths[n], b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return paths[0], paths[1], paths[2]
}
