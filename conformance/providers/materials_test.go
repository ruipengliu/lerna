package providers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

type memoryMaterialReader struct {
	service     *memory.Service
	scope       runtime.Scope
	auth        runtime.Auth
	reads       atomic.Int64
	unavailable atomic.Bool
}

func (r *memoryMaterialReader) ReadMaterial(ctx context.Context, ref api.ContentRef) ([]byte, error) {
	r.reads.Add(1)
	if r.unavailable.Load() {
		return nil, api.E("dependency_unavailable", "original_material_authority_unavailable")
	}
	return r.service.Read(ctx, r.scope, r.auth, ref, "brain.input")
}

func materialFixture(t *testing.T, cfg providers.OpenAIConfig) (*memoryMaterialReader, func(string) api.ContentRef) {
	t.Helper()
	objects, err := objectstore.OpenLocal(t.TempDir(), memory.MaxContentBytes)
	if err != nil {
		t.Fatal(err)
	}
	service := memory.New(cfg.Store, objects)
	auth := runtime.Auth{TenantID: cfg.Scope.TenantID, SubjectID: api.NewID("subject"), CredentialGeneration: 1, Roles: []string{"content_admin"}}
	values := memory.PolicyValues{Subjects: []string{auth.SubjectID}, Purposes: []string{"content.write", "brain.input"}, Locations: []string{"local", "cloud"}, RetainUntil: api.Time(time.Now().Add(time.Hour)), Continuous: true}
	digest, err := api.Digest(values)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := memory.NewPolicy(api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}, values)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.InstallPolicy(context.Background(), cfg.Scope, auth, policy); err != nil {
		t.Fatal(err)
	}
	reader := &memoryMaterialReader{service: service, scope: cfg.Scope, auth: auth}
	upload := func(body string) api.ContentRef {
		ref := api.ContentRef{TenantID: cfg.Scope.TenantID, OwnerID: cfg.Scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte(body)), MediaType: "text/plain", ByteLength: uint64(len(body))}
		_, err := service.Upload(context.Background(), cfg.Scope, auth, memory.PublicationRequest{ContentRef: ref, TransferID: api.NewID("upload"), ReserveCommandID: api.NewID("command"), PutCommandID: api.NewID("command"), PolicyRef: policy.PolicyRef, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(time.Now().Add(10 * time.Minute)), TransferDeadline: api.Time(time.Now().Add(5 * time.Minute))}, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		return ref
	}
	return reader, upload
}

func TestExactGoalDocumentMaterialsAndFrozenLookupWithoutSecondRead(t *testing.T) {
	var sends atomic.Int64
	physical := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		body, _ := io.ReadAll(r.Body)
		physical <- body
		_, _ = w.Write(providerReply(t))
	}))
	defer server.Close()
	cfg := configuration(t, server.URL)
	reader, upload := materialFixture(t, cfg)
	goalBody := "根据已保存的真实行动结果，编写中文报告。"
	goal := upload(goalBody)
	resultBody := `{"status":"succeeded","observed_bytes":"原结果正文"}`
	result := upload(resultBody)
	document := api.Raw(api.GoalDocument{FormatVersion: 1, InitialGoalRef: goal, AmendmentRefs: []api.ContentRef{}})
	documentRef := upload(string(document))
	cfg.MaterialResolver = reader
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := api.Snapshot{SnapshotID: api.NewID("snapshot"), Revision: 1, TaskRef: cfg.Scope.Ref(api.NewID("task"), 1), GoalRef: documentRef, ModelProfileRef: engine.Profile().Ref, TokenizerRef: engine.TokenizerRef(), ProcessedSources: []api.ContentRef{documentRef, goal, result}, MaterialRefs: []api.ContentRef{goal, result}, ReservedOutputTokens: 100}
	enc, err := engine.Encode(context.Background(), snapshot, document, engine.Profile())
	if err != nil || reader.reads.Load() != 2 {
		t.Fatalf("accurate material encoding: %v reads=%d", err, reader.reads.Load())
	}
	reader.unavailable.Store(true)
	call := api.NewID("call")
	out, err := engine.Request(context.Background(), call, enc)
	if err != nil || out.Draft.Kind != "complete" {
		t.Fatalf("frozen request needed new source IO: %v", err)
	}
	actual := <-physical
	var wire struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	var input struct {
		Goal      string `json:"goal_utf8"`
		Materials []struct {
			Ref  api.ContentRef `json:"ref"`
			Body string         `json:"body_utf8"`
		} `json:"materials"`
	}
	if err = json.Unmarshal(actual, &wire); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(wire.Messages[1].Content), &input); err != nil {
		t.Fatal(err)
	}
	if input.Goal != string(document) || len(input.Materials) != 2 || input.Materials[0].Ref != goal || input.Materials[0].Body != goalBody || input.Materials[1].Ref != result || input.Materials[1].Body != resultBody {
		t.Fatalf("model received inaccurate reference material: %+v", input)
	}
	restored, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restored.Lookup(context.Background(), call, enc)
	if err != nil || !api.Equal(out, recovered) || sends.Load() != 1 || reader.reads.Load() != 2 {
		t.Fatalf("original frozen material recovery: %v sends=%d reads=%d", err, sends.Load(), reader.reads.Load())
	}
	undeclared := snapshot
	undeclared.ProcessedSources = []api.ContentRef{documentRef, goal}
	if _, err = engine.Encode(context.Background(), undeclared, document, engine.Profile()); !api.IsCode(err, "forbidden") || reader.reads.Load() != 2 {
		t.Fatalf("undeclared material read before gate: %v reads=%d", err, reader.reads.Load())
	}
}

func TestContextDerivedEncodingFieldsDoNotChangePhysicalBody(t *testing.T) {
	cfg := configuration(t, "https://provider.example/v1/chat/completions")
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first := encoded(t, engine, cfg)
	var wire struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	var input struct {
		Snapshot api.Snapshot `json:"snapshot"`
		Goal     string       `json:"goal_utf8"`
	}
	if err = json.Unmarshal(first.Body, &wire); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(wire.Messages[1].Content), &input); err != nil {
		t.Fatal(err)
	}
	input.Snapshot.InputTokens = first.InputTokens
	input.Snapshot.EncodedDigest = first.Digest
	second, err := engine.Encode(context.Background(), input.Snapshot, []byte(input.Goal), engine.Profile())
	if err != nil || !api.Equal(first, second) {
		t.Fatalf("Context→Brain changed physical bytes/digest merely by saving derived metadata: %v", err)
	}
	var _ brain.Engine = engine
}

func TestLostResponseWithMaterialsDoesNotReadSourcesOrSendAgain(t *testing.T) {
	var sends atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	cfg := configuration(t, server.URL)
	reader, upload := materialFixture(t, cfg)
	goal := upload("原引用的中文目标")
	document := api.Raw(api.GoalDocument{FormatVersion: 1, InitialGoalRef: goal, AmendmentRefs: []api.ContentRef{}})
	documentRef := upload(string(document))
	cfg.MaterialResolver = reader
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := api.Snapshot{TaskRef: cfg.Scope.Ref(api.NewID("task"), 1), GoalRef: documentRef, ModelProfileRef: engine.Profile().Ref, TokenizerRef: engine.TokenizerRef(), ProcessedSources: []api.ContentRef{documentRef, goal}, MaterialRefs: []api.ContentRef{goal}}
	enc, err := engine.Encode(context.Background(), snapshot, document, engine.Profile())
	if err != nil {
		t.Fatal(err)
	}
	reader.unavailable.Store(true)
	call := api.NewID("call")
	if _, err = engine.Request(context.Background(), call, enc); !api.IsCode(err, "effect_unknown") {
		t.Fatalf("lost material call: %v", err)
	}
	restored, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Lookup(context.Background(), call, enc); !api.IsCode(err, "effect_unknown") {
		t.Fatalf("original material lookup: %v", err)
	}
	if _, err = restored.Request(context.Background(), call, enc); !api.IsCode(err, "effect_unknown") {
		t.Fatalf("repeat material request: %v", err)
	}
	if sends.Load() != 1 || reader.reads.Load() != 1 {
		t.Fatalf("uncertain call made new IO: sends=%d reads=%d", sends.Load(), reader.reads.Load())
	}
}

func TestBinaryMaterialAndUndeclaredBytesNeverReachModel(t *testing.T) {
	cfg := configuration(t, "https://provider.example/v1/chat/completions")
	reader, upload := materialFixture(t, cfg)
	goal := upload("原目标")
	binary := upload(string([]byte{0xff, 0xfe}))
	cfg.MaterialResolver = reader
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := api.Snapshot{TaskRef: cfg.Scope.Ref(api.NewID("task"), 1), GoalRef: goal, ModelProfileRef: engine.Profile().Ref, TokenizerRef: engine.TokenizerRef(), ProcessedSources: []api.ContentRef{goal, binary}, MaterialRefs: []api.ContentRef{binary}}
	if _, err = engine.Encode(context.Background(), snapshot, []byte("原目标"), engine.Profile()); !api.IsCode(err, "invalid_request") {
		t.Fatalf("binary unsupported in utf8 model wire: %v", err)
	}
	before := reader.reads.Load()
	oversized := binary
	oversized.ByteLength = engine.Profile().MaxInputBytes + 1
	snapshot.ProcessedSources = []api.ContentRef{goal, oversized}
	snapshot.MaterialRefs = []api.ContentRef{oversized}
	if _, err = engine.Encode(context.Background(), snapshot, []byte("原目标"), engine.Profile()); !api.IsCode(err, "invalid_request") || reader.reads.Load() != before {
		t.Fatalf("declared bytes unbounded before read: %v reads=%d", err, reader.reads.Load())
	}
}
