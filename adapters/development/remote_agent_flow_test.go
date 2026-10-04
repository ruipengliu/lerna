package development

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type configuredAgentEndpoint struct {
	app      *App
	config   Config
	server   *httptest.Server
	live     atomic.Bool
	stop     context.CancelFunc
	done     chan error
	requests atomic.Int64
}

// 显式验收目录保留失败时的原数据库和journal。既有目录不能被重置为新身份。
func configuredAgentDataRoot(t *testing.T, index int) string {
	t.Helper()
	root := os.Getenv("HARNESS_TEST_REMOTE_AGENT_EVIDENCE_ROOT")
	if root == "" {
		return t.TempDir()
	}
	if !filepath.IsAbs(root) || index < 0 || index > 1 {
		t.Fatal("remote Agent evidence root must be absolute and bounded")
	}
	name := api.Hash([]byte(t.Name()))[len("sha256:"):]
	path := filepath.Join(root, name, []string{"parent", "child"}[index])
	if index == 0 {
		if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func (e *configuredAgentEndpoint) persistPrivateConfiguration(t *testing.T) {
	t.Helper()
	if os.Getenv("HARNESS_TEST_REMOTE_AGENT_EVIDENCE_ROOT") == "" {
		return
	}
	if err := SaveConfig(filepath.Join(e.config.DataRoot, "active-config.json"), e.config); err != nil {
		t.Fatal(err)
	}
	for _, peer := range e.config.RemoteAgent.Peers {
		for _, ref := range []string{peer.InboundTokenEnvRef, peer.OutboundTokenEnvRef} {
			secret := strings.TrimSpace(os.Getenv(ref))
			if !remoteEnvRef.MatchString(ref) || secret == "" {
				t.Fatal("private peer credential reference unavailable")
			}
			path := filepath.Join(e.config.DataRoot, "peer-token-"+ref)
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if errors.Is(err, os.ErrExist) {
				original, readErr := os.ReadFile(path)
				if readErr != nil || api.Hash(original) != api.Hash([]byte(secret)) {
					t.Fatal("original private peer credential changed")
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.WriteString(secret)
			err = errors.Join(err, f.Sync(), f.Close())
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	dir, err := os.Open(e.config.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(dir.Sync(), dir.Close()); err != nil {
		t.Fatal(err)
	}
}

func (e *configuredAgentEndpoint) reopenOriginal(t *testing.T) {
	t.Helper()
	e.stopRun(t)
	if err := e.app.Close(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("HARNESS_TEST_REMOTE_AGENT_EVIDENCE_ROOT") != "" {
		var err error
		e.config, err = LoadConfig(filepath.Join(e.config.DataRoot, "active-config.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, peer := range e.config.RemoteAgent.Peers {
			for _, ref := range []string{peer.InboundTokenEnvRef, peer.OutboundTokenEnvRef} {
				secret, err := os.ReadFile(filepath.Join(e.config.DataRoot, "peer-token-"+ref))
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv(ref, string(secret))
			}
		}
	}
	requests := e.requests.Load()
	var err error
	e.app, err = OpenApp(context.Background(), e.config, false)
	if err != nil {
		t.Fatal(err)
	}
	if e.requests.Load() != requests {
		t.Fatal("original App reopen implicitly sent network requests")
	}
	e.startRun(t)
}

// 两个App.Run HTTPS、SQL库、签名和对象介质均为实际实现；外层只计数/注入故障。
func configuredAgentPair(t *testing.T) (*configuredAgentEndpoint, *configuredAgentEndpoint, collaboration.RemoteAgentProfile) {
	return configuredAgentPairWithParentDriver(t, "sqlite")
}

func configuredAgentPairWithParentDriver(t *testing.T, parentDriver string) (*configuredAgentEndpoint, *configuredAgentEndpoint, collaboration.RemoteAgentProfile) {
	t.Helper()
	return configuredAgentPairWithParentDriverContext(t, parentDriver, context.Background())
}

// 仅prepared夹具传入有界准备context；旧调用及App.Run生命周期保持原样。
func configuredAgentPairWithParentDriverContext(t *testing.T, parentDriver string, ctx context.Context) (*configuredAgentEndpoint, *configuredAgentEndpoint, collaboration.RemoteAgentProfile) {
	t.Helper()
	if parentDriver != "sqlite" && parentDriver != "postgres" {
		t.Fatal("configured Agent requires an explicit SQLite or PostgreSQL parent")
	}
	if parentDriver == "postgres" {
		dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("actual PostgreSQL DSN required for the independent parent authority")
		}
		t.Setenv("HARNESS_DATABASE_DSN", dsn)
	}
	tenant, user := api.NewID("tenant"), api.NewID("subject")
	endpoints := []*configuredAgentEndpoint{{}, {}}
	for i, e := range endpoints {
		root := configuredAgentDataRoot(t, i)
		var err error
		driver := "sqlite"
		if i == 0 {
			driver = parentDriver
		}
		e.config, err = InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
		if err != nil {
			t.Fatal(err)
		}
		e.config.TenantID, e.config.SubjectID, e.config.OwnerID = tenant, user, api.NewID("owner")
		keys, err := platform.OpenDevelopmentKey(e.config.KeyFile, tenant, e.config.OwnerID, []string{"agent_allocation", "agent_state", "foreign_content"})
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.MarshalPKIXPublicKey(keys.Keys["development-es256"].Public)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, "source-public.pem"), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
		e.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			e.requests.Add(1)
			if !e.live.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			forward := r.Clone(r.Context())
			forward.URL, _ = url.Parse("https://" + e.config.HTTPAddr + r.URL.RequestURI())
			forward.RequestURI = ""
			response, err := e.server.Client().Do(forward)
			if err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			defer response.Body.Close()
			for key, values := range response.Header {
				w.Header()[key] = values
			}
			w.WriteHeader(response.StatusCode)
			_, _ = io.Copy(w, response.Body)
		}))
		certFile, keyFile := filepath.Join(root, "source-ca.pem"), filepath.Join(root, "source-tls-key.pem")
		if err = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: e.server.Certificate().Raw}), 0600); err != nil {
			t.Fatal(err)
		}
		private, err := x509.MarshalPKCS8PrivateKey(e.server.TLS.Certificates[0].PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
			t.Fatal(err)
		}
		e.config.ForeignSourceTLS = &ForeignSourceTLSConfig{CertificateFile: certFile, KeyFile: keyFile}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		e.config.HTTPAddr = listener.Addr().String()
		if err = listener.Close(); err != nil {
			t.Fatal(err)
		}
		t.Setenv([]string{"HARNESS_TEST_AGENT_A_IN", "HARNESS_TEST_AGENT_B_IN"}[i], api.NewID("credential"))
		t.Cleanup(func() {
			e.server.Close()
			e.stopRun(t)
			if e.app != nil {
				if err := e.app.Close(); err != nil {
					t.Error(err)
				}
			}
		})
	}
	a, b := endpoints[0], endpoints[1]
	// 第一次真实安装即使用 App 相同的纯政策构造与当前 GoalSchema 摘要。
	// 其后仍与已安装两端完整 refs/配置强核，不能假定构造规则永远相同。
	answerSchema := component("goal-answer-schema")
	answerDigest, digestErr := api.Digest(brain.GoalSchema())
	if digestErr != nil {
		t.Fatal(digestErr)
	}
	answerSchema.Digest = answerDigest
	initialPolicy := builtinTaskPolicy(b.config, answerSchema)
	profile, err := collaboration.NewRemoteAgentProfile(api.NewID("agent"), "1.0.0", collaboration.RemoteAgentValues{ParentOwnerID: a.config.OwnerID, ReceiverID: b.config.OwnerID, AgentBindingRef: runtime.Scope{TenantID: tenant, OwnerID: a.config.OwnerID}.Ref(api.NewID("binding"), 1), PolicyRef: initialPolicy.PolicyRef, InstallLockRef: component("builtin-install-lock"), SubjectRefs: []api.ObjectRef{{TenantID: tenant, OwnerID: a.config.OwnerID, ObjectID: user, Revision: 1}}, PermissionRefs: []api.ObjectRef{}, CapabilityRefs: []api.ComponentRef{}, BindingRefs: []api.ObjectRef{}, ResourceRefs: []api.ComponentRef{}, BudgetLimits: []api.Amount{{Unit: "USD", Value: "3"}}, MaxDepth: 4, MaxInputs: 4, Location: "cloud", MaterialPurposes: []string{"task.goal", "task.submit", "content.write", "brain.input", "task.context", "task.delegate", "task.steer", "task.snapshot", "task.action", "execution.arguments", "brain.output", "task.attach_evidence", "task.complete", "task.input", "task.need_context", "task.accept_result"}})
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range endpoints {
		peer := endpoints[1-i]
		der, err := os.ReadFile(filepath.Join(peer.config.DataRoot, "source-public.pem"))
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(der)
		e.config.RemoteAgent = &RemoteAgentConfig{Profiles: []collaboration.RemoteAgentProfile{profile}, Peers: []RemoteAgentPeerConfig{{TenantID: tenant, OwnerID: peer.config.OwnerID, DatabaseID: peer.config.DatabaseID, Endpoint: peer.server.URL, CAFile: filepath.Join(peer.config.DataRoot, "source-ca.pem"), SigningKeyID: "development-es256", SigningPublicKeyFile: filepath.Join(peer.config.DataRoot, "source-public.pem"), SigningPublicKeyDigest: api.Hash(block.Bytes), InboundTokenEnvRef: []string{"HARNESS_TEST_AGENT_A_IN", "HARNESS_TEST_AGENT_B_IN"}[i], OutboundTokenEnvRef: []string{"HARNESS_TEST_AGENT_B_IN", "HARNESS_TEST_AGENT_A_IN"}[i], InboundSubjectRef: runtime.Scope{TenantID: tenant, OwnerID: e.config.OwnerID}.Ref(peer.config.OwnerID, 1)}}, SourceSubjectRefs: []api.ObjectRef{{TenantID: tenant, OwnerID: peer.config.OwnerID, ObjectID: user, Revision: 1}, {TenantID: tenant, OwnerID: peer.config.OwnerID, ObjectID: peer.config.OwnerID, Revision: 1}}}
		e.app, err = OpenApp(ctx, e.config, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	// 管理阶段读取实际 Task 政策/安装锁。完整原配置相等时无需重复构造两端；
	// 字段或摘要变化仍登记准确 profile 并按原路径重开，之后才有委派。
	originalProfile := profile
	values := profile.Values
	values.PolicyRef, values.InstallLockRef = b.app.TaskPolicy.PolicyRef, b.app.InstallLock
	profile, err = collaboration.NewRemoteAgentProfile(profile.ProfileRef.ComponentID, profile.ProfileRef.Version, values)
	if err != nil {
		t.Fatal(err)
	}
	originalBytes, err := api.Canonical(api.Raw(originalProfile))
	if err != nil {
		t.Fatal(err)
	}
	actualBytes, err := api.Canonical(api.Raw(profile))
	if err != nil {
		t.Fatal(err)
	}
	wantedProfiles, err := api.Canonical(api.Raw([]collaboration.RemoteAgentProfile{profile}))
	if err != nil {
		t.Fatal(err)
	}
	unchanged := originalProfile.ProfileRef == profile.ProfileRef && api.Equal(originalProfile, profile) && bytes.Equal(originalBytes, actualBytes)
	for _, e := range endpoints {
		configured, err := api.Canonical(api.Raw(e.config.RemoteAgent.Profiles))
		if err != nil {
			t.Fatal(err)
		}
		unchanged = unchanged && len(e.config.RemoteAgent.Profiles) == 1 && e.config.RemoteAgent.Profiles[0].ProfileRef == profile.ProfileRef && bytes.Equal(configured, wantedProfiles)
	}
	t.Logf("original paired profile equal=%v exact_ref=%s JCS=%s duplicate_reopens_skipped=%v", unchanged, profile.ProfileRef.Digest, api.Hash(actualBytes), unchanged)
	for _, e := range endpoints {
		if !unchanged {
			if err = e.app.Close(); err != nil {
				t.Fatal(err)
			}
			e.config.RemoteAgent.Profiles = []collaboration.RemoteAgentProfile{profile}
			e.app, err = OpenApp(ctx, e.config, false)
			if err != nil {
				t.Fatal(err)
			}
		}
		e.persistPrivateConfiguration(t)
	}
	if a.requests.Load() != 0 || b.requests.Load() != 0 {
		t.Fatal("constructing/reopening paired App issued network requests")
	}
	if a.app.Scope.DatabaseID == b.app.Scope.DatabaseID {
		t.Fatal("two owners must have independent databases")
	}
	a.startRun(t)
	b.startRun(t)
	return a, b, profile
}

func (e *configuredAgentEndpoint) startRun(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e.stop, e.done = cancel, make(chan error, 1)
	go func() { e.done <- e.app.Run(ctx, true, false) }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := e.server.Client().Get("https://" + e.config.HTTPAddr + "/health/ready")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				e.live.Store(true)
				return
			}
		}
		select {
		case err := <-e.done:
			t.Fatalf("actual App.Run stopped before HTTPS ready: %v", err)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("actual App.Run HTTPS did not become ready")
}

func (e *configuredAgentEndpoint) stopRun(t *testing.T) {
	t.Helper()
	e.live.Store(false)
	if e.stop == nil {
		return
	}
	e.stop()
	select {
	case err := <-e.done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("actual App.Run failed to exit")
	}
	e.stop = nil
}

func configuredAgentStep(ctx context.Context, t *testing.T, e *configuredAgentEndpoint, kinds ...string) bool {
	t.Helper()
	works, status, err := e.app.Store.Claim(ctx, e.app.Scope, api.NewID("boot"), kinds, 1, time.Minute)
	if err != nil || status != runtime.Committed {
		t.Fatalf("original job claim: %v %v", status, err)
	}
	if len(works) == 0 {
		return false
	}
	handler, ok := e.app.Registry.Job(works[0].Job.Kind)
	if !ok {
		t.Fatal("original job handler missing")
	}
	if err = handler(ctx, e.app.Store, e.app.Scope, works[0]); err != nil {
		if works[0].Job.Kind == task.JobDispatchDecision {
			view, viewErr := e.app.Brain.Get(ctx, e.app.Store, e.app.Scope, e.app.ServiceAuth, works[0].Job.SourceRef.ObjectID)
			t.Logf("original decision status=%s publication=%s cancelled=%v get_error=%v", view.Decision.Status, view.Publication, view.CancelRequested, viewErr)
			if viewErr == nil {
				actual, readErr := e.app.Task.Read(ctx, e.app.Store, e.app.Scope, e.app.ServiceAuth, view.TaskRef.ObjectID)
				t.Logf("original Task status=%s control=%s rev=%d goal=%d control_rev=%d task_read=%v", actual.Status, actual.Control, actual.Revision, actual.GoalRevision, actual.ControlRevision, readErr)
			}
		}
		t.Fatalf("original %s: %v", works[0].Job.Kind, err)
	}
	return true
}

func configuredAgentOriginalParent(ctx context.Context, t *testing.T, a *configuredAgentEndpoint) (api.ContentRef, api.Task) {
	t.Helper()
	goal, err := a.app.Publish(ctx, a.app.Scope, a.app.UserAuth, api.NewID("content"), "application/json", api.Raw(brain.GoalSpec{Kind: "report", Title: "Parent independently verifies", Body: "A child reply alone cannot complete this parent.", SavePath: "reports/parent.md"}), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	parentID := api.NewID("task")
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "task.submit", TargetID: parentID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.app.Scope.OwnerID, GoalRef: goal, PolicyRef: a.app.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}, RequirementCandidates: []api.RequirementCandidate{}})}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("original parent %+v %v", receipt, err)
	}
	var parent api.Task
	for {
		parent, err = a.app.Task.Read(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, parentID)
		if err != nil {
			t.Fatal(err)
		}
		if parent.RequirementsState == "ready" {
			var found bool
			status, readErr := a.app.Store.Within(ctx, a.app.Scope, []string{"task"}, func(tx runtime.Tx) error {
				_, actualFound, err := a.app.Task.LatestTaskSnapshotTx(ctx, tx, a.app.ServiceAuth, parentID)
				found = actualFound
				return err
			})
			if status != runtime.Committed || readErr != nil {
				t.Fatalf("actual original parent snapshot: %v %v", status, readErr)
			}
			if found {
				break
			}
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		if !configuredAgentStep(ctx, t, a, task.JobAdvance, task.JobDispatchDecision, task.JobCoverage, brain.JobAdvance, proofJob) {
			time.Sleep(20 * time.Millisecond)
		}
	}
	return goal, parent
}

func TestConfiguredRemoteAgentCreatesAndRecoversOriginalChild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	a, b, profile := configuredAgentPair(t)
	goal, parent := configuredAgentOriginalParent(ctx, t, a)
	var err error
	var receipt api.Receipt
	if a.requests.Load() != 0 || b.requests.Load() != 0 {
		t.Fatal("local parent Brain preparation contacted a remote Agent")
	}
	id := api.NewID("delegation")
	in := task.DelegateInput{DelegationID: id, ParentTaskRef: a.app.Scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: goal, InputRefs: []api.ContentRef{}, AgentBindingRef: profile.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{}, Budget: []api.Amount{{Unit: "USD", Value: "2"}}, Deadline: api.Time(time.Now().Add(3 * time.Minute)), PolicyRef: b.app.TaskPolicy.PolicyRef, ReceiverID: b.app.Scope.OwnerID}
	delegate := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "collaboration.delegate", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(in)}
	receipt, err = a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(delegate))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("public original delegation %+v %v", receipt, err)
	}
	if !configuredAgentStep(ctx, t, a, task.JobDelegation) {
		t.Fatal("original parent handoff job missing")
	}
	if !configuredAgentStep(ctx, t, b, collaboration.JobRemoteCreate) {
		t.Fatal("original receiver creation job missing")
	}
	d, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
	if err != nil {
		t.Fatal(err)
	}
	state, err := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
	if err != nil {
		t.Fatal(err)
	}
	if state.Task == nil || state.Task.OrchestratorID != b.app.Scope.OwnerID || state.Task.GoalRef != goal || state.Task.Status != "active" || state.SourceDatabaseID != b.app.Scope.DatabaseID {
		t.Fatalf("actual independent child %+v", state)
	}
	if state.Fact.GoalWorkClosed || state.ResultRef != nil {
		t.Fatalf("unverified child cannot establish completion %+v", state.Fact)
	}
	b.stopRun(t)
	if err = b.app.Close(); err != nil {
		t.Fatal(err)
	}
	b.app, err = OpenApp(ctx, b.config, false)
	if err != nil {
		t.Fatal(err)
	}
	b.startRun(t)
	again, err := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
	if err != nil || again.Task == nil || again.Task.TaskID != state.Task.TaskID || again.Task.GoalRef != goal {
		t.Fatalf("original child reopen %+v %v", again, err)
	}
	duplicate, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(delegate))
	if err != nil || !api.Equal(duplicate, receipt) {
		t.Fatalf("original parent receipt changed %+v %v", duplicate, err)
	}
	if !configuredAgentStep(ctx, t, b, task.JobAdvance) {
		t.Fatal("original child first advancement job missing after reopen")
	}
	var hasSnapshot bool
	status, err := b.app.Store.Within(ctx, b.app.Scope, []string{"task"}, func(tx runtime.Tx) error {
		_, hasSnapshot, err = b.app.Task.LatestTaskSnapshotTx(ctx, tx, b.app.ServiceAuth, state.Task.TaskID)
		return err
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("original child decision projection: %v %v", status, err)
	}
	if !hasSnapshot {
		t.Fatal("current parent preparation did not admit the original child decision")
	}
}
