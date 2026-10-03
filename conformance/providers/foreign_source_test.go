package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

// 许可夹具只登记有限 peer/subject；源许可和副本仍由实际 Memory 服务处理。
type pairedSourceAuthority struct {
	peer, subject runtime.Auth
	consumer      string
}

func (a pairedSourceAuthority) ResolveSourceSubjectTx(_ context.Context, tx runtime.Tx, peer runtime.Auth, ref memory.ForeignReference, control bool) (runtime.Auth, error) {
	if !api.Equal(peer, a.peer) || ref.HolderRef.OwnerID != a.consumer || ref.HolderRef.TenantID != tx.Scope().TenantID || ref.HolderRef.ObjectID != a.subject.SubjectID || ref.ReferenceIntentRef.OwnerID != a.consumer || (!control && ref.HolderRef.Revision != a.subject.CredentialGeneration) || control && ref.HolderRef.Revision > a.subject.CredentialGeneration {
		return runtime.Auth{}, api.E("forbidden", "source_pair_not_registered")
	}
	return a.subject, nil
}

func TestForeignSourceTLSOriginalRegistrationAndControlledBytes(t *testing.T) {
	source := newInformationFixture(t, "https://unused.example")
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "consumer.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = store.Migrate(source.ctx); err != nil {
		t.Fatal(err)
	}
	consumerScope := runtime.Scope{TenantID: source.scope.TenantID, OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	objects, err := objectstore.OpenLocal(t.TempDir(), memory.MaxContentBytes)
	if err != nil {
		t.Fatal(err)
	}
	consumer := memory.New(store, objects)
	peer := runtime.Auth{TenantID: source.scope.TenantID, SubjectID: consumerScope.OwnerID, CredentialGeneration: 1, Roles: []string{"foreign_content_peer"}}
	keys, err := platform.NewDevelopmentKey(source.scope.TenantID, source.scope.OwnerID, []string{"foreign_content"})
	if err != nil {
		t.Fatal(err)
	}
	registry := runtime.NewRegistry()
	source.content.service.Register(registry)
	fa, err := providers.NewForeignSource(providers.ForeignSourceConfig{Store: source.store, Scope: source.scope, Memory: source.content.service, Keys: keys, SigningKeyID: "development-es256", Authority: pairedSourceAuthority{peer, source.auth, consumerScope.OwnerID}, Participants: []string{"content", "memory"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = fa.Register(registry); err != nil {
		t.Fatal(err)
	}
	dispatch := &runtime.Dispatcher{Store: source.store, OwnerID: source.scope.OwnerID, Registry: registry}
	loseRegister := true
	var physicalCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		physicalCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer paired-source-test-credential" {
			w.WriteHeader(401)
			return
		}
		var frame struct {
			Kind    string          `json:"kind"`
			Payload json.RawMessage `json:"payload"`
		}
		b, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if e == nil {
			e = api.Decode(b, &frame)
		}
		var out any
		switch frame.Kind {
		case "command":
			var c api.Command
			e = api.Decode(frame.Payload, &c)
			if e == nil {
				out, e = dispatch.Command(r.Context(), peer, frame.Payload)
			}
			if e == nil && c.Method == "content.foreign.register" && loseRegister {
				loseRegister = false
				conn, _, hijackErr := w.(http.Hijacker).Hijack()
				if hijackErr == nil {
					_ = conn.Close()
					return
				}
				t.Error(hijackErr)
			}
		case "query":
			out, e = dispatch.Query(r.Context(), peer, frame.Payload)
		case "receipt_lookup":
			var q api.ReceiptLookup
			e = api.Decode(frame.Payload, &q)
			if e == nil {
				out, e = dispatch.Lookup(r.Context(), peer, q.CommandID)
			}
		default:
			e = api.E("unsupported", "test_transport_kind")
		}
		kind := "response"
		if e != nil {
			kind = "error"
			var business *api.Error
			if errors.As(e, &business) {
				out = business
			} else {
				out = api.E("dependency_unavailable", "test_source_failure")
			}
		}
		_, _ = w.Write(api.Raw(struct {
			ResultKind string          `json:"result_kind"`
			Payload    json.RawMessage `json:"payload"`
		}{kind, api.Raw(out)}))
	}))
	defer server.Close()
	methods := registry.Contracts()
	digest, _ := api.DigestLimit(methods, 1<<20)
	discovery := harness.Discovery{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: source.scope.OwnerID, SchemaDigest: api.CoreDigest(), Methods: methods, MethodsDigest: digest, IdentityScope: source.scope.TenantID + "/" + source.scope.OwnerID + "/" + source.scope.DatabaseID, Limits: harness.Limits{MaxDomainBytes: api.MaxJSONBytes, MaxFrameBytes: 1 << 20, MaxPending: 4096}}
	journal, err := harness.OpenJournal(t.TempDir(), discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	sdk, err := harness.NewClient(&harness.HTTPTransport{BaseURL: server.URL, Token: "paired-source-test-credential", HTTP: server.Client()}, journal, discovery)
	if err != nil {
		t.Fatal(err)
	}
	public := keys.Keys["development-es256"]
	public.Private = nil
	client := &providers.ForeignSourceClient{SDK: sdk, Keys: &platform.Keyring{Keys: map[string]platform.RegisteredKey{"development-es256": public}}, SourceScope: source.scope, ConsumerScope: consumerScope}
	consumer.Foreign = client
	wrongActor := source.auth
	wrongActor.SubjectID = api.NewID("subject")
	body := []byte(strings.Repeat("original-source-text\n", 8000))
	ref := api.ContentRef{TenantID: source.scope.TenantID, OwnerID: source.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(body), MediaType: "text/plain", ByteLength: uint64(len(body))}
	ref, err = source.content.PublishReceived(source.ctx, source.scope, source.auth, providers.ReceivedPublication{ContentRef: ref, ObtainedAt: api.Time(time.Now()), ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}}, body)
	if err != nil {
		t.Fatal(err)
	}
	in := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: consumerScope.Ref(api.NewID("intent"), 1), HolderRef: source.auth.Ref(consumerScope.OwnerID), Purpose: "task.goal", Location: "local", RetainUntil: api.Time(time.Now().Add(5 * time.Minute))}
	beforeWrong := physicalCalls.Load()
	if _, err = client.RegisterCopy(source.ctx, consumerScope, wrongActor, in); !api.IsCode(err, "forbidden") || physicalCalls.Load() != beforeWrong {
		t.Fatalf("unbound actor left local port: %v", err)
	}
	if _, err = consumer.PrepareForeignUse(source.ctx, consumerScope, source.auth, in); err == nil {
		t.Fatal("actual lost register response returned success")
	}
	use, err := consumer.PrepareForeignUse(source.ctx, consumerScope, source.auth, in)
	if err != nil {
		t.Fatal(err)
	}
	if use.Reference != in || use.Proof.ContentRef != ref || use.Proof.SourceDatabaseID != source.scope.DatabaseID {
		t.Fatal("original reference replaced")
	}
	got, err := consumer.ReadBytes(source.ctx, consumerScope, source.auth, ref, "task.goal", "local")
	if err != nil || string(got) != string(body) {
		t.Fatalf("actual byte transport %d %v", len(got), err)
	}
	control, err := consumer.ControlForeignCopy(source.ctx, consumerScope, source.auth, in.CopyID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Read(source.ctx, consumerScope, source.auth, in, control.Proof); !api.IsCode(err, "forbidden") {
		t.Fatalf("control proof body %v", err)
	}
	one := uint64(1)
	closed, err := dispatch.Command(source.ctx, source.auth, api.Raw(api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: source.scope.OwnerID, CommandID: api.NewID("command"), Method: "content.close", TargetID: ref.ContentID, ExpectedRevision: &one, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CloseInput{ContentRef: ref, Reason: "original source close"})}))
	if err != nil || closed.Stage != "applied" {
		t.Fatalf("close source %+v %v", closed, err)
	}
	if _, err = consumer.ReadBytes(source.ctx, consumerScope, source.auth, ref, "task.goal", "local"); !api.IsCode(err, "forbidden") {
		t.Fatalf("closed source allowed bytes: %v", err)
	}
	if err = consumer.StopForeignCopy(source.ctx, consumerScope, source.auth, in.CopyID); err != nil {
		t.Fatal(err)
	}
	if _, err = consumer.ReadBytes(source.ctx, consumerScope, source.auth, ref, "task.goal", "local"); !api.IsCode(err, "forbidden") {
		t.Fatalf("stopped original copy %v", err)
	}
	// 原命令receipt仍可核对；源cleanup pending不伪称所有介质擦除。
	receipt, err := sdk.Receipt(source.ctx, in.RegisterCommandID)
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("original receipt %+v %v", receipt, err)
	}
}
