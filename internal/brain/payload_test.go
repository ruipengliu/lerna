package brain_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/runtime"
)

func TestLargeProviderReplySurvivesBrainRestartWithSinglePhysicalCall(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openBrainStore(t, filepath.Join(root, "brain.sqlite"))
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service"}}
	content := &fileContents{root: root, paused: true}
	report := strings.Repeat("a", 180<<10)
	var posts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("X-Harness-Call-ID") == "" {
			t.Error("physical call must carry the original CallID")
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(largeProviderReply(report))
	}))
	t.Cleanup(server.Close)
	cfg := brainProviderConfig(store, scope, server.URL)
	service, registry, provider := newBrainProvider(t, cfg, content)
	snapshot := providerSnapshot(t, scope, content, provider, []byte("Produce the declared report draft."))
	command := acceptProviderDecision(t, store, scope, auth, registry, content, snapshot)
	// Pause only the object-store boundary after the original response and known
	// actual fee have committed. No Brain state or model output is inserted.
	view := drainBrainUntil(t, store, scope, auth, service, registry, command.TargetID, "publishing")
	if posts.Load() != 1 || view.Decision.PhysicalRequestCount != 1 || !view.Decision.SendStarted || !view.Decision.UsageFinal || !api.Equal(view.Decision.Usage, []api.Amount{{Unit: "USD", Value: "0.00024"}}) {
		t.Fatalf("large original reply lost send/fee facts: %+v posts=%d", view, posts.Load())
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openBrainStore(t, filepath.Join(root, "brain.sqlite"))
	if reopened.ID() != scope.DatabaseID {
		t.Fatal("reopen changed the responsible database")
	}
	cfg.Store = reopened
	content.paused = false
	service, registry, _ = newBrainProvider(t, cfg, content)
	completed := drainBrainUntil(t, reopened, scope, auth, service, registry, command.TargetID, "completed")
	if completed.CallID != view.CallID || posts.Load() != 1 || completed.Decision.PhysicalRequestCount != 1 || !completed.Decision.UsageFinal || completed.Decision.ProposalRef == nil {
		t.Fatalf("restart must recover the original single physical call: %+v posts=%d", completed, posts.Load())
	}
	proposalBytes, err := content.Read(ctx, scope, auth, *completed.Decision.ProposalRef, "brain.output")
	if err != nil {
		t.Fatal(err)
	}
	var proposal brain.Proposal
	if err = api.Decode(proposalBytes, &proposal); err != nil || proposal.Kind != "complete" || len(proposal.ArtifactRefs) != 1 {
		t.Fatalf("actual large reply must publish a closed proposal: %v", err)
	}
	actual, err := content.Read(ctx, scope, auth, proposal.ArtifactRefs[0], "brain.output")
	if err != nil || string(actual) != report || proposal.ArtifactRefs[0].ByteLength != 180<<10 {
		t.Fatalf("large output lost original bytes: length=%d %v", len(actual), err)
	}
	usage, err := service.Usage(ctx, reopened, scope, scope.Ref(command.TargetID, 1))
	if err != nil || !usage.SpendingClosed || !usage.UsageFinal || !api.Equal(usage.Cumulative, []api.Amount{{Unit: "USD", Value: "0.00024"}}) || len(usage.ProofRefs) != 1 {
		t.Fatalf("known original fee must survive hydration and restart: %+v %v", usage, err)
	}
	dispatch := runtime.Dispatcher{Store: reopened, OwnerID: scope.OwnerID, Registry: registry}
	duplicate, err := dispatch.Command(ctx, auth, api.Raw(command))
	if err != nil || duplicate.Stage != "applied" || posts.Load() != 1 {
		t.Fatalf("original command recovery must not send again: %+v %v", duplicate, err)
	}
}

func TestLargeEncodingRecoversOriginalProviderLookupAfterUnknownCommit(t *testing.T) {
	root := t.TempDir()
	var armed atomic.Bool
	store := openBrainStore(t, filepath.Join(root, "brain.sqlite"), sqlite.WithCommitFault(func(phase sqlite.CommitPhase) error {
		if phase == sqlite.AfterCommit && armed.CompareAndSwap(true, false) {
			return errors.New("original provider reply committed; acknowledgement lost")
		}
		return nil
	}))
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service"}}
	content := &fileContents{root: root}
	var posts atomic.Int64
	requests := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		requests <- body
		armed.Store(true)
		_, _ = w.Write(largeProviderReply("original reply after a large sealed input"))
	}))
	t.Cleanup(server.Close)
	cfg := brainProviderConfig(store, scope, server.URL)
	service, registry, provider := newBrainProvider(t, cfg, content)
	snapshot := providerSnapshot(t, scope, content, provider, []byte(strings.Repeat("b", 180<<10)))
	command := acceptProviderDecision(t, store, scope, auth, registry, content, snapshot)
	unknown := drainBrainUntil(t, store, scope, auth, service, registry, command.TargetID, "provider_result_unknown")
	request := <-requests
	if len(request) < 180<<10 || posts.Load() != 1 || unknown.Decision.PhysicalRequestCount != 1 || !unknown.Decision.SendStarted || unknown.Decision.UsageFinal {
		t.Fatalf("lost original reply must retain one sealed physical request: %+v bytes=%d posts=%d", unknown, len(request), posts.Load())
	}
	original, err := provider.Call(context.Background(), unknown.CallID)
	if err != nil || original.Status != "received" || original.ProviderResponseID != "original-provider-reply" || original.EncodingDigest != api.Hash(request) {
		t.Fatalf("actual original provider reply must have committed: %+v %v", original, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	// 原输入介质不可读时仍只能使用原完整编码核原 CallID；不能重取材料组装新请求。
	if err = os.Remove(filepath.Join(root, snapshot.GoalRef.ContentID+".blob")); err != nil {
		t.Fatal(err)
	}
	reopened := openBrainStore(t, filepath.Join(root, "brain.sqlite"))
	cfg.Store = reopened
	service, registry, _ = newBrainProvider(t, cfg, content)
	completed := drainBrainUntil(t, reopened, scope, auth, service, registry, command.TargetID, "completed")
	if completed.CallID != unknown.CallID || completed.Decision.PhysicalRequestCount != 1 || posts.Load() != 1 || !completed.Decision.UsageFinal || !api.Equal(completed.Decision.Usage, []api.Amount{{Unit: "USD", Value: "0.00024"}}) {
		t.Fatalf("restart/Lookup lost the original reply or actual fee: %+v posts=%d", completed, posts.Load())
	}
	usage, err := service.Usage(context.Background(), reopened, scope, scope.Ref(command.TargetID, 1))
	if err != nil || !usage.SpendingClosed || !usage.UsageFinal || !api.Equal(usage.Cumulative, []api.Amount{{Unit: "USD", Value: "0.00024"}}) {
		t.Fatalf("original actual fee after Lookup: %+v %v", usage, err)
	}
}

func TestLegacyInlineDecisionRecoversOriginalLookupWithoutReencoding(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "brain.sqlite")
	var armed atomic.Bool
	store := openBrainStore(t, path, sqlite.WithCommitFault(func(phase sqlite.CommitPhase) error {
		if phase == sqlite.AfterCommit && armed.CompareAndSwap(true, false) {
			return errors.New("legacy original provider acknowledgement lost")
		}
		return nil
	}))
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service"}}
	content := &fileContents{root: root}
	var posts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		armed.Store(true)
		_, _ = w.Write(largeProviderReply("original small response from an inline decision"))
	}))
	t.Cleanup(server.Close)
	cfg := brainProviderConfig(store, scope, server.URL)
	service, registry, provider := newBrainProvider(t, cfg, content)
	snapshot := providerSnapshot(t, scope, content, provider, []byte("original inline goal"))
	command := acceptProviderDecision(t, store, scope, auth, registry, content, snapshot)
	original := drainBrainUntil(t, store, scope, auth, service, registry, command.TargetID, "provider_result_unknown")
	// 迁移夹具只把真实保存的复合值写回历史 inline 格式；业务验证仍走公开接口。
	installLegacyInlineBrainRecord(t, path, scope, command.TargetID)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, snapshot.GoalRef.ContentID+".blob")); err != nil {
		t.Fatal(err)
	}
	reopened := openBrainStore(t, path)
	cfg.Store = reopened
	service, registry, _ = newBrainProvider(t, cfg, content)
	completed := drainBrainUntil(t, reopened, scope, auth, service, registry, command.TargetID, "completed")
	if completed.CallID != original.CallID || posts.Load() != 1 || completed.Decision.PhysicalRequestCount != 1 || !completed.Decision.UsageFinal {
		t.Fatalf("historical inline state must resume the original physical call: %+v posts=%d", completed, posts.Load())
	}
	usage, err := service.Usage(context.Background(), reopened, scope, scope.Ref(command.TargetID, 1))
	if err != nil || !usage.SpendingClosed || !usage.UsageFinal || !api.Equal(usage.Cumulative, []api.Amount{{Unit: "USD", Value: "0.00024"}}) {
		t.Fatalf("inline original fees must survive migration: %+v %v", usage, err)
	}
}

func installLegacyInlineBrainRecord(t *testing.T, path string, scope runtime.Scope, decisionID string) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw []byte
	if err = db.QueryRow("SELECT data FROM runtime_records WHERE tenant_id=? AND owner_id=? AND namespace='brain.decisions' AND object_id=?", scope.TenantID, scope.OwnerID, decisionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var record map[string]json.RawMessage
	if err = json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Chunks []api.ObjectRef `json:"chunks"`
	}
	if err = json.Unmarshal(record["payload"], &manifest); err != nil {
		t.Fatal(err)
	}
	var body []byte
	for _, ref := range manifest.Chunks {
		if err = db.QueryRow("SELECT data FROM runtime_versions WHERE tenant_id=? AND owner_id=? AND namespace='brain.payloads' AND object_id=? AND revision=?", scope.TenantID, scope.OwnerID, ref.ObjectID, ref.Revision).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var chunk struct {
			Body []byte `json:"body"`
		}
		if err = json.Unmarshal(raw, &chunk); err != nil {
			t.Fatal(err)
		}
		body = append(body, chunk.Body...)
	}
	var payload map[string]json.RawMessage
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	for field, value := range payload {
		record[field] = value
	}
	delete(record, "payload")
	raw, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE runtime_records SET data=? WHERE tenant_id=? AND owner_id=? AND namespace='brain.decisions' AND object_id=?", raw, scope.TenantID, scope.OwnerID, decisionID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec("UPDATE runtime_versions SET data=? WHERE tenant_id=? AND owner_id=? AND namespace='brain.decisions' AND object_id=? AND revision=(SELECT revision FROM runtime_records WHERE tenant_id=? AND owner_id=? AND namespace='brain.decisions' AND object_id=?)", raw, scope.TenantID, scope.OwnerID, decisionID, scope.TenantID, scope.OwnerID, decisionID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestChunkedBrainResumeIsAtomicAcrossLostGeneratedCommit(t *testing.T) {
	for _, phase := range []sqlite.CommitPhase{sqlite.BeforeCommit, sqlite.AfterCommit} {
		t.Run(string(phase), func(t *testing.T) {
			root := t.TempDir()
			var armed atomic.Bool
			var commits atomic.Int64
			injected := errors.New("generated payload commit fault")
			store := openBrainStore(t, filepath.Join(root, "brain.sqlite"), sqlite.WithCommitFault(func(at sqlite.CommitPhase) error {
				if at == phase && armed.Load() && commits.Add(1) == 2 {
					return injected // 第一次是 Provider 原回复，第二次是 Brain 载荷+主头+Job。
				}
				return nil
			}))
			scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
			auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service"}}
			content := &fileContents{root: root}
			var posts atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				armed.Store(true)
				_, _ = w.Write(largeProviderReply(strings.Repeat("c", 180<<10)))
			}))
			t.Cleanup(server.Close)
			cfg := brainProviderConfig(store, scope, server.URL)
			service, registry, provider := newBrainProvider(t, cfg, content)
			snapshot := providerSnapshot(t, scope, content, provider, []byte("Original goal with commit fault"))
			command := acceptProviderDecision(t, store, scope, auth, registry, content, snapshot)
			claimed := finiteLeaseStore{store}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			for {
				err := runtime.Drain(ctx, claimed, scope, registry, 30)
				if err != nil {
					if phase == sqlite.BeforeCommit && !errors.Is(err, injected) || phase == sqlite.AfterCommit && !errors.Is(err, runtime.ErrCommitUnknown) {
						t.Fatalf("unexpected commit fault result: %v", err)
					}
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("original generated commit did not reach the real fault point")
				case <-time.After(10 * time.Millisecond):
				}
			}
			before, err := service.Get(context.Background(), store, scope, auth, command.TargetID)
			wantPhase := "send_started"
			if phase == sqlite.AfterCommit {
				wantPhase = "publishing"
			}
			if err != nil || before.Publication != wantPhase || before.Decision.PhysicalRequestCount != 1 || posts.Load() != 1 {
				t.Fatalf("main phase must agree with complete original payload after fault: %+v %v", before, err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := openBrainStore(t, filepath.Join(root, "brain.sqlite"))
			cfg.Store = reopened
			service, registry, _ = newBrainProvider(t, cfg, content)
			completed := drainBrainUntil(t, finiteLeaseStore{reopened}, scope, auth, service, registry, command.TargetID, "completed")
			if completed.CallID != before.CallID || posts.Load() != 1 || completed.Decision.PhysicalRequestCount != 1 || !completed.Decision.UsageFinal || !api.Equal(completed.Decision.Usage, []api.Amount{{Unit: "USD", Value: "0.00024"}}) {
				t.Fatalf("rollback or unknown commit must recover original reply/fees with one POST: %+v posts=%d", completed, posts.Load())
			}
		})
	}
}

func TestCorruptStoredBrainPayloadCannotBeDecodedOrRebuilt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "brain.sqlite")
	store := openBrainStore(t, path)
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service"}}
	content := &fileContents{root: root, paused: true}
	var posts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(largeProviderReply(strings.Repeat("d", 180<<10)))
	}))
	t.Cleanup(server.Close)
	service, registry, provider := newBrainProvider(t, brainProviderConfig(store, scope, server.URL), content)
	snapshot := providerSnapshot(t, scope, content, provider, []byte("Original goal with a corrupt stored chunk"))
	command := acceptProviderDecision(t, store, scope, auth, registry, content, snapshot)
	original := drainBrainUntil(t, store, scope, auth, service, registry, command.TargetID, "publishing")
	// 仅故障设置使用真实 SQL 改一个原分片字节，不改变主头/费用/摘要/长度。
	// 验证只观察公开 Brain/Usage 边界，不能用材料重新组装这个原载荷。
	restore := corruptStoredBrainChunk(t, path, scope, command.TargetID)
	if _, err := service.Get(context.Background(), store, scope, auth, command.TargetID); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("same-length changed bytes must fail the original aggregate hash: %v", err)
	}
	if _, err := service.Usage(context.Background(), store, scope, scope.Ref(command.TargetID, 1)); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("Usage must not hydrate a changed original aggregate: %v", err)
	}
	if posts.Load() != 1 {
		t.Fatal("corrupt original payload triggered another physical call")
	}
	restore()
	after, err := service.Get(context.Background(), store, scope, auth, command.TargetID)
	if err != nil || !api.Equal(after, original) || posts.Load() != 1 {
		t.Fatalf("stored metadata/known fees/original call were lost by blocked hydration: %+v %v", after, err)
	}
	content.paused = false
	completed := drainBrainUntil(t, store, scope, auth, service, registry, command.TargetID, "completed")
	if completed.CallID != original.CallID || completed.Decision.PhysicalRequestCount != 1 || posts.Load() != 1 || !completed.Decision.UsageFinal {
		t.Fatalf("restored exact original bytes must resume without another call: %+v", completed)
	}
}

// 真实数据库完整性故障是测试夹具，私有格式只用于定位并翻转故障点。
func corruptStoredBrainChunk(t *testing.T, path string, scope runtime.Scope, decisionID string) func() {
	t.Helper()
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var raw []byte
	if err = db.QueryRow("SELECT data FROM runtime_records WHERE tenant_id=? AND owner_id=? AND namespace='brain.decisions' AND object_id=?", scope.TenantID, scope.OwnerID, decisionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var header struct {
		Payload struct {
			Chunks []api.ObjectRef `json:"chunks"`
		} `json:"payload"`
	}
	if err = json.Unmarshal(raw, &header); err != nil || len(header.Payload.Chunks) == 0 {
		t.Fatalf("fault requires an existing exact chunk: %v", err)
	}
	ref := header.Payload.Chunks[0]
	if err = db.QueryRow("SELECT data FROM runtime_versions WHERE tenant_id=? AND owner_id=? AND namespace='brain.payloads' AND object_id=? AND revision=?", scope.TenantID, scope.OwnerID, ref.ObjectID, ref.Revision).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), raw...)
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	var body []byte
	if err = json.Unmarshal(fields["body"], &body); err != nil || len(body) < 2 {
		t.Fatalf("fault requires nonempty original bytes: %v", err)
	}
	body[len(body)/2] ^= 1
	fields["body"] = api.Raw(body)
	write := func(raw []byte) {
		t.Helper()
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err = tx.Exec("UPDATE runtime_records SET data=? WHERE tenant_id=? AND owner_id=? AND namespace='brain.payloads' AND object_id=?", raw, scope.TenantID, scope.OwnerID, ref.ObjectID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("UPDATE runtime_versions SET data=? WHERE tenant_id=? AND owner_id=? AND namespace='brain.payloads' AND object_id=? AND revision=?", raw, scope.TenantID, scope.OwnerID, ref.ObjectID, ref.Revision); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	write(api.Raw(fields))
	return func() { write(original) }
}

// 宿主领取租期是外部调度边界；真实 DB 的 Guard/过期重领保持不变。
type finiteLeaseStore struct{ runtime.Store }

func (s finiteLeaseStore) Claim(ctx context.Context, scope runtime.Scope, holder string, kinds []string, limit int, _ time.Duration) ([]runtime.Work, runtime.CommitStatus, error) {
	return s.Store.Claim(ctx, scope, holder, kinds, limit, 5*time.Second)
}

func openBrainStore(t *testing.T, path string, options ...sqlite.Option) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(path, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func brainProviderConfig(store runtime.Store, scope runtime.Scope, endpoint string) providers.OpenAIConfig {
	return providers.OpenAIConfig{Store: store, Scope: scope, Endpoint: endpoint, Model: "brain-payload-contract", Receiver: "contract-provider", Location: "cloud", APIKey: "contract-test-credential", CredentialID: "contract-credential-v1", AllowHTTPForLoopback: true, Tokenizer: providers.UTF8UpperBound{}, InputUSDPerMillion: "2", OutputUSDPerMillion: "4", CachedInputUSDPerMillion: "1", BillingFinal: true, Profile: brain.Profile{Ref: api.ComponentRef{ComponentID: "profile_b090dd82602414a5c0b14f1234567890", Version: "1"}, ContextLimit: 500000, MaxInputTokens: 490000, MaxOutputTokens: 1000, SafetyMargin: 64, MaxInputBytes: api.MaxJSONBytes, RequestTimeout: 5 * time.Second}, MaxResponseBytes: api.MaxJSONBytes, MaxConcurrent: 2}
}

func newBrainProvider(t *testing.T, cfg providers.OpenAIConfig, content brain.Content) (*brain.Service, *runtime.Registry, *providers.OpenAI) {
	t.Helper()
	provider, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	service, err := brain.New(brain.Config{Profiles: []brain.Profile{provider.Profile()}, Content: content, Engine: provider, Gate: gate{}})
	if err != nil {
		t.Fatal(err)
	}
	registry := runtime.NewRegistry()
	if err = service.Register(registry); err != nil {
		t.Fatal(err)
	}
	return service, registry, provider
}

func largeProviderReply(report string) []byte {
	model := map[string]any{"draft": map[string]any{"kind": "complete", "reason_local_id": "reason", "artifact_local_ids": []string{"report"}}, "contents": []any{map[string]any{"local_id": "reason", "media_type": "text/plain", "body": "A proposal requires independent verification.", "disclosed_sources": []any{}}, map[string]any{"local_id": "report", "media_type": "text/plain", "body": report, "disclosed_sources": []any{}}}}
	return api.Raw(map[string]any{"id": "original-provider-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(model))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}})
}

func providerSnapshot(t *testing.T, scope runtime.Scope, content *fileContents, provider *providers.OpenAI, goal []byte) api.Snapshot {
	t.Helper()
	goalRef, err := content.put(scope, api.NewID("goal"), "text/plain", goal)
	if err != nil {
		t.Fatal(err)
	}
	profile := provider.Profile().Ref
	requirement := api.Requirement{RequirementID: api.NewID("requirement"), Revision: 1, Kind: "quality", StatementRef: goalRef, SourceRefs: []api.SourceEvidence{{ContentRef: goalRef, SourceKind: "user_input"}}, Origin: "derived", RuleRef: profile, Required: true, AdoptionID: api.NewID("adoption")}
	return api.Snapshot{SnapshotID: api.NewID("snapshot"), Revision: 1, TaskRef: scope.Ref(api.NewID("task"), 1), GoalRevision: 1, ControlRevision: 1, GoalRef: goalRef, Requirements: []api.Requirement{requirement}, RequirementsDigest: api.Hash(api.Raw([]api.Requirement{requirement})), RequirementsState: "ready", Purpose: "continue_task", FactRefs: []api.ObjectRef{}, UnresolvedCollections: []api.CollectionSummary{}, PolicyRef: profile, InstallLockRef: profile, ModelProfileRef: profile, CapabilityRefs: []api.ComponentRef{}, BindingRefs: []api.ObjectRef{}, MaterialRefs: []api.ContentRef{}, SelectionReportRef: goalRef, ProcessedSources: []api.ContentRef{goalRef}, InputTokens: 100, ReservedOutputTokens: 100, SafetyMarginTokens: 64, CountMode: "upper_bound", TokenizerRef: provider.TokenizerRef(), EncodedDigest: api.Hash([]byte("sealed"))}
}

func acceptProviderDecision(t *testing.T, store runtime.Store, scope runtime.Scope, auth runtime.Auth, registry *runtime.Registry, content *fileContents, snapshot api.Snapshot) api.Command {
	t.Helper()
	ref, err := content.put(scope, api.NewID("snapshot"), "application/json", api.Raw(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	deadline := api.Time(time.Now().Add(time.Minute))
	id := api.NewID("decision")
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: "brain.decide", ExpiresAt: deadline, Payload: api.Raw(brain.DecideInput{DecisionID: id, TaskRef: snapshot.TaskRef, SnapshotRef: ref, SnapshotRevision: 1, ModelProfileRef: snapshot.ModelProfileRef, UseRefs: []api.ObjectRef{}, Limits: []api.Amount{{Unit: "USD", Value: "1"}}, Deadline: deadline})}
	dispatch := runtime.Dispatcher{Store: store, OwnerID: scope.OwnerID, Registry: registry}
	receipt, err := dispatch.Command(context.Background(), auth, api.Raw(command))
	if err != nil || receipt.Stage != "accepted" {
		t.Fatalf("accept original decision: %+v %v", receipt, err)
	}
	return command
}

func drainBrainUntil(t *testing.T, store runtime.Store, scope runtime.Scope, auth runtime.Auth, service *brain.Service, registry *runtime.Registry, id, phase string) brain.View {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for {
		if err := runtime.Drain(ctx, store, scope, registry, 30); err != nil {
			var business *api.Error
			if errors.As(err, &business) {
				t.Fatalf("original Brain work must preserve bounded legal payload: %v cause=%v", err, business.Cause)
			}
			t.Fatalf("original Brain work must preserve bounded legal payload: %v", err)
		}
		view, err := service.Get(ctx, store, scope, auth, id)
		if err != nil {
			t.Fatal(err)
		}
		if view.Publication == phase {
			return view
		}
		select {
		case <-ctx.Done():
			t.Fatalf("original decision did not reach %s: phase=%s status=%s", phase, view.Publication, view.Decision.Status)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// 准确不可变对象存储是外部 I/O 边界；文件真实保留到宿主/数据库重开。
type fileContents struct {
	root   string
	paused bool
}

func (c *fileContents) put(scope runtime.Scope, id, media string, body []byte) (api.ContentRef, error) {
	ref := api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: id, Version: 1, Hash: api.Hash(body), MediaType: media, ByteLength: uint64(len(body))}
	path := filepath.Join(c.root, id+".blob")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		original, readErr := os.ReadFile(path)
		if readErr != nil || string(original) != string(body) {
			return api.ContentRef{}, api.E("idempotency_conflict", "fixture_original_content_changed")
		}
		return ref, nil
	}
	if err != nil {
		return api.ContentRef{}, err
	}
	_, err = file.Write(body)
	closeErr := file.Close()
	if err != nil {
		return api.ContentRef{}, err
	}
	return ref, closeErr
}
func (c *fileContents) Read(_ context.Context, scope runtime.Scope, _ runtime.Auth, ref api.ContentRef, _ string) ([]byte, error) {
	if ref.TenantID != scope.TenantID || ref.OwnerID != scope.OwnerID || !api.ValidID(ref.ContentID) {
		return nil, api.E("forbidden", "fixture_content_scope")
	}
	body, err := os.ReadFile(filepath.Join(c.root, ref.ContentID+".blob"))
	if err == nil && (uint64(len(body)) != ref.ByteLength || api.Hash(body) != ref.Hash) {
		return nil, api.E("invalid_request", "fixture_content_corrupt")
	}
	return body, err
}
func (c *fileContents) Publish(_ context.Context, scope runtime.Scope, _ runtime.Auth, publication brain.Publication, body []byte) (api.ContentRef, error) {
	if c.paused {
		return api.ContentRef{}, api.E("dependency_unavailable", "fixture_publication_paused")
	}
	return c.put(scope, publication.ContentID, publication.MediaType, body)
}
