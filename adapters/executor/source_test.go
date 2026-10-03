package executor

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 此 seam 使用真实设备执行产物和公开原 content.register_copy；holder/intent
// 明确预置为云端原主体和本方持久意图引用，未把它冒充完整 Task 发布。
func TestDeviceSourceCopyPreservesOriginalOwnerAndRequiresRegisteredPurpose(t *testing.T) {
	f := newDeviceFixture(t)
	freezeSourcePolicies(t, f)
	f.prepare(t)
	f.invoke(t, f.control(t, "active", "running", 1))
	if err := runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	view := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if view.Operation.ResultRef == nil {
		t.Fatal("actual device result missing")
	}
	ref := *view.Operation.ResultRef
	r := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: api.ObjectRef{TenantID: f.b.Principal.TenantID, OwnerID: f.b.AuthorityID, ObjectID: api.NewID("intent"), Revision: 1}, HolderRef: f.b.Principal.Auth().Ref(f.b.AuthorityID), Purpose: "execution_result", Location: "cloud", RetainUntil: api.Time(time.Now().Add(15 * time.Second))}
	in := memory.RegisterCopyInput{CopyID: r.CopyID, ContentRef: r.ContentRef, HolderRef: r.HolderRef, Purpose: r.Purpose, Location: r.Location, RetainUntil: r.RetainUntil, ReferenceIntentRef: r.ReferenceIntentRef}
	_, receipt := f.command(t, "content.register_copy", ref.ContentID, in)
	if receipt.Stage != "applied" {
		t.Fatalf("original source registration: %+v", receipt)
	}
	p := deviceQuery[memory.ForeignProof](t, f, "executor.content.current", ref.ContentID, struct {
		Reference memory.ForeignReference `json:"reference"`
		Control   bool                    `json:"control"`
	}{r, false})
	if !api.Equal(p.ContentRef, ref) || p.SourceDatabaseID != f.h.Scope.DatabaseID || p.SubjectRef != r.HolderRef || p.SourceState != "published" || p.UseState != "allowed" || p.CleanupState != "pending" || p.PolicyValues.IndependentDerived || !p.PolicyValues.Continuous || len(p.ProcessedSources) != 1 || p.ProcessedSources[0].OwnerID != f.b.AuthorityID || p.Proof == "" {
		t.Fatalf("source truth changed: %+v", p)
	}
	chunk := deviceQuery[ContentChunk](t, f, "executor.content.get", ref.ContentID, struct {
		ContentRef api.ContentRef          `json:"content_ref"`
		ChunkIndex uint64                  `json:"chunk_index"`
		Reference  memory.ForeignReference `json:"reference"`
	}{ref, 0, r})
	data, err := base64.StdEncoding.Strict().DecodeString(chunk.DataBase64)
	if err != nil || api.Hash(data) != ref.Hash || uint64(len(data)) != ref.ByteLength {
		t.Fatalf("original output bytes: %v", err)
	}
	bad := in
	bad.CopyID = api.NewID("copy")
	bad.Purpose = "brain_context"
	_, refused := f.command(t, "content.register_copy", ref.ContentID, bad)
	if refused.Stage != "rejected" || refused.Error == nil || refused.Error.Code != "forbidden" {
		t.Fatalf("unconfigured purpose expanded authority: %+v", refused)
	}
}

func TestTLSForeignSourceGateClosesNewReadsButRetainsOriginalCopyControl(t *testing.T) {
	f := newDeviceFixture(t)
	freezeSourcePolicies(t, f)
	client, ctx := startTLSHost(t, f)
	if err := client.Prepare(ctx, f.b, func(_ context.Context, p ContentPermission) ([]byte, error) {
		return f.raw[contentKey(p.ContentRef)], nil
	}); err != nil {
		t.Fatal(err)
	}
	window := f.control(t, "active", "running", 1)
	var delivery ControlDelivery
	if _, err := f.h.Store.Read(ctx, f.h.Scope, Namespace+".controls", window.WindowID, 0, &delivery); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Dispatch(ctx, frozenInvoke(f, window), delivery); err != nil {
		t.Fatal(err)
	}
	view := waitApplied(t, ctx, client, f.b.Intent.OperationID)
	if view.Operation.ResultRef == nil {
		t.Fatal("actual result missing")
	}
	ref := *view.Operation.ResultRef
	a := f.b.Principal.Auth()
	s := runtime.Scope{TenantID: a.TenantID, OwnerID: f.b.AuthorityID}
	r := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: s.Ref(api.NewID("intent"), 1), HolderRef: a.Ref(s.OwnerID), Purpose: "execution_result", Location: "cloud", RetainUntil: api.Time(time.Now().Add(15 * time.Second))}
	key := f.h.Keys.Keys["device-es256"]
	port := &SourceClient{Client: client, Keys: &platform.Keyring{Keys: map[string]platform.RegisteredKey{"device-es256": {TenantID: s.TenantID, Issuer: ref.OwnerID, Public: key.Public, Purposes: []string{"executor_content"}}}}}
	proof, err := port.RegisterCopy(ctx, s, a, r)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := port.Control(ctx, s, a, r)
	if err != nil || metadata.Mode != "control" || metadata.UseState != "allowed" {
		t.Fatalf("accurate still-allowed control: %+v %v", metadata, err)
	}
	if _, err = port.Read(ctx, s, a, r, metadata); !api.IsCode(err, "forbidden") {
		t.Fatalf("control proof granted data: %v", err)
	}
	bytes, err := port.Read(ctx, s, a, r, proof)
	if err != nil || api.Hash(bytes) != ref.Hash {
		t.Fatalf("original registered bytes: %v", err)
	}
	consumer, err := sqlite.Open(filepath.Join(t.TempDir(), "consumer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if err = consumer.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s.DatabaseID = consumer.ID()
	_, err = consumer.Within(ctx, s, []string{"content"}, func(tx runtime.Tx) error { return port.VerifyTx(ctx, tx, a, r, proof) })
	if err != nil {
		t.Fatalf("pure signature gate: %v", err)
	}
	modified := proof
	modified.PolicyValues.Subjects = []string{api.NewID("user")}
	_, err = consumer.Within(ctx, s, []string{"content"}, func(tx runtime.Tx) error { return port.VerifyTx(ctx, tx, a, r, modified) })
	if err == nil {
		t.Fatal("tampered original policy passed source gate")
	}
	newAuth := a
	newAuth.CredentialGeneration++
	if _, err = port.Current(ctx, s, newAuth, r); !api.IsCode(err, "forbidden") {
		t.Fatalf("new generation reused old data holder: %v", err)
	}
	if _, err = port.Control(ctx, s, newAuth, r); err != nil {
		t.Fatalf("new generation lost old stop responsibility: %v", err)
	}
	proof, err = port.Current(ctx, s, a, r)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.h.ClosePublishedContent(ctx, ref, "source_revoked"); err != nil {
		t.Fatal(err)
	}
	if _, err = port.Current(ctx, s, a, r); !api.IsCode(err, "forbidden") {
		t.Fatalf("closed source reopened by mirror: %v", err)
	}
	if _, err = port.Read(ctx, s, a, r, proof); !api.IsCode(err, "forbidden") {
		t.Fatalf("old proof bypassed actual source closure: %v", err)
	}
	control, err := port.Control(ctx, s, a, r)
	if err != nil || control.SourceState != "closing" || control.UseState != "closing" || control.ControlRevision != 2 || control.CleanupState != "pending" {
		t.Fatalf("original holder control lost: %+v %v", control, err)
	}
	final, err := port.Release(ctx, s, a, r, memory.ReleaseCopyInput{CopyID: r.CopyID, ContentRef: ref, ControlRevision: control.ControlRevision, UseStopped: true, CleanupState: "unknown", EvidenceRefs: []api.ContentRef{}})
	if err != nil || final.UseState != "use_stopped" || final.CleanupState != "unknown" {
		t.Fatalf("unknown cleanup falsely completed: %+v %v", final, err)
	}
	entry, err := client.SDK.Journal.Read(ctx, r.ReleaseCommandID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = port.Release(ctx, s, a, r, memory.ReleaseCopyInput{CopyID: r.CopyID, ContentRef: ref, ControlRevision: control.ControlRevision, UseStopped: true, CleanupState: "unknown", EvidenceRefs: []api.ContentRef{}}); err != nil {
		t.Fatalf("original cleanup reply recovery: %v", err)
	}
	second, err := client.SDK.Journal.Read(ctx, r.ReleaseCommandID)
	if err != nil || !api.Equal(entry.Command, second.Command) {
		t.Fatal("cleanup replay refreshed original TTL or payload")
	}
	t.Logf("EXECUTOR_SOURCE_EVIDENCE %s", api.Raw(struct {
		ContentRef         api.ContentRef `json:"content_ref"`
		CopyID             string         `json:"copy_id"`
		DeviceDatabaseID   string         `json:"device_database_id"`
		ConsumerDatabaseID string         `json:"consumer_database_id"`
		ControlRevision    uint64         `json:"control_revision"`
		UseState           string         `json:"use_state"`
		CleanupState       string         `json:"cleanup_state"`
	}{ref, r.CopyID, f.h.Scope.DatabaseID, s.DatabaseID, final.ControlRevision, final.UseState, final.CleanupState}))
}

func TestSourceCopyConfigurationCannotExpandOriginalPolicyOrLegacyCache(t *testing.T) {
	for _, scenario := range []string{"legacy_policy_absent", "subject_generation_not_attested", "purpose_intersection"} {
		t.Run(scenario, func(t *testing.T) {
			f := newDeviceFixture(t)
			holder := f.b.Principal.Auth().Ref(f.b.AuthorityID)
			if scenario != "legacy_policy_absent" {
				freezeSourcePolicies(t, f)
			}
			if scenario == "subject_generation_not_attested" {
				holder.ObjectID = api.NewID("user")
				f.h.Config.OutputSubjectRefs = []api.ObjectRef{holder}
			}
			if scenario == "purpose_intersection" {
				f.h.Config.OutputPurposes = []string{"arbitrary_read"}
				for i := range f.b.Contents {
					policy := f.b.Contents[i].SourcePolicy
					policy.Values.Purposes = []string{"execution_usage_proof"}
					policy.PolicyRef.Digest, _ = api.Digest(policy.Values)
				}
				digest, _ := BundleDigest(f.b)
				f.b.Proof, _ = f.keys.Sign("development-es256", bundleClaims(f.b, digest))
			}
			f.prepare(t)
			f.invoke(t, f.control(t, "active", "running", 1))
			if err := runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
				t.Fatal(err)
			}
			view := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
			if view.Operation.ResultRef == nil || view.Operation.Effect != "applied" || len(view.Attempts.Items) != 1 {
				t.Fatal("legacy original attempt facts were discarded")
			}
			in := memory.RegisterCopyInput{CopyID: api.NewID("copy"), ContentRef: *view.Operation.ResultRef, HolderRef: holder, ReferenceIntentRef: api.ObjectRef{TenantID: f.b.Principal.TenantID, OwnerID: f.b.AuthorityID, ObjectID: api.NewID("intent"), Revision: 1}, Purpose: "execution_result", Location: "cloud", RetainUntil: api.Time(time.Now().Add(5 * time.Second))}
			_, receipt := f.command(t, "content.register_copy", in.ContentRef.ContentID, in)
			if receipt.Stage != "rejected" || receipt.Error == nil || receipt.Error.Code != "forbidden" {
				t.Fatalf("config granted unproven source scope: %+v", receipt)
			}
		})
	}
}

func TestTLSOriginalSourceRegistrationReplyRecoveryAndExpiredCopyCleanup(t *testing.T) {
	f := newDeviceFixture(t)
	freezeSourcePolicies(t, f)
	client, ctx := startTLSHost(t, f)
	if err := client.Prepare(ctx, f.b, func(_ context.Context, p ContentPermission) ([]byte, error) {
		return f.raw[contentKey(p.ContentRef)], nil
	}); err != nil {
		t.Fatal(err)
	}
	window := f.control(t, "active", "running", 1)
	var delivery ControlDelivery
	if _, err := f.h.Store.Read(ctx, f.h.Scope, Namespace+".controls", window.WindowID, 0, &delivery); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Dispatch(ctx, frozenInvoke(f, window), delivery); err != nil {
		t.Fatal(err)
	}
	view := waitApplied(t, ctx, client, f.b.Intent.OperationID)
	if view.Operation.ResultRef == nil {
		t.Fatal("actual result missing")
	}
	ref := *view.Operation.ResultRef
	a := f.b.Principal.Auth()
	s := runtime.Scope{TenantID: a.TenantID, OwnerID: f.b.AuthorityID}
	r := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: s.Ref(api.NewID("intent"), 1), HolderRef: a.Ref(s.OwnerID), Purpose: "execution_result", Location: "cloud", RetainUntil: api.Time(time.Now().Add(3 * time.Second))}
	key := f.h.Keys.Keys["device-es256"]
	port := &SourceClient{Client: client, Keys: &platform.Keyring{Keys: map[string]platform.RegisteredKey{"device-es256": {TenantID: s.TenantID, Issuer: ref.OwnerID, Public: key.Public, Purposes: []string{"executor_content"}}}}}
	client.SDK.Transport = &loseOriginalReply{inner: client.SDK.Transport, journal: client.SDK.Journal, commandID: r.RegisterCommandID}
	if _, err := port.RegisterCopy(ctx, s, a, r); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("original source register lost reply: %v", err)
	}
	entry, err := client.SDK.Journal.Read(ctx, r.RegisterCommandID)
	if err != nil || entry.Command.ExpiresAt != r.RetainUntil {
		t.Fatal("original registration not journaled")
	}
	if recovered, partial, err := client.SDK.Recover(ctx); err != nil || partial || len(recovered) != 1 || recovered[0].CommandID != r.RegisterCommandID {
		t.Fatalf("original copy receipt recovery: %+v %v", recovered, err)
	}
	proof, err := port.Current(ctx, s, a, r)
	if err != nil || proof.CopyID != r.CopyID {
		t.Fatalf("original copy changed: %+v %v", proof, err)
	}
	until, _ := api.ParseTime(r.RetainUntil)
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(time.Until(until.Add(40 * time.Millisecond))):
	}
	if _, err = port.Read(ctx, s, a, r, proof); !api.IsCode(err, "expired") {
		t.Fatalf("expired cached bytes read: %v", err)
	}
	newAuth := a
	newAuth.CredentialGeneration++
	control, err := port.Control(ctx, s, newAuth, r)
	if err != nil || control.HolderRef != r.HolderRef || control.Mode != "control" || control.UseState != "closing" {
		t.Fatalf("expired original holder control: %+v %v", control, err)
	}
	final, err := port.Release(ctx, s, newAuth, r, memory.ReleaseCopyInput{CopyID: r.CopyID, ContentRef: ref, ControlRevision: control.ControlRevision, UseStopped: true, CleanupState: "pending", EvidenceRefs: []api.ContentRef{}})
	if err != nil || final.UseState != "use_stopped" || final.CleanupState != "pending" {
		t.Fatalf("data expiry erased stop responsibility: %+v %v", final, err)
	}
	stop, err := client.SDK.Journal.Read(ctx, r.ReleaseCommandID)
	if err != nil || stop.Command.ExpiresAt <= r.RetainUntil {
		t.Fatalf("cleanup TTL incorrectly used data expiry: %+v %v", stop.Command, err)
	}
}

func freezeSourcePolicies(t *testing.T, f *deviceFixture) {
	t.Helper()
	for i := range f.b.Contents {
		p := &f.b.Contents[i]
		values := memory.PolicyValues{Subjects: []string{f.b.AuthorityID}, Purposes: []string{"execution_result", "execution_usage_proof"}, Locations: []string{"cloud", "device"}, RetainUntil: p.RetainUntil, Continuous: true, IndependentDerived: false}
		digest, err := api.Digest(values)
		if err != nil {
			t.Fatal(err)
		}
		p.SourcePolicy = &memory.Policy{PolicyRef: api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}, Values: values, Revision: 1, State: "active"}
		p.SubjectRefs = []api.ObjectRef{f.b.Principal.Auth().Ref(f.b.AuthorityID)}
	}
	digest, err := BundleDigest(f.b)
	if err != nil {
		t.Fatal(err)
	}
	f.b.Proof, err = f.keys.Sign("development-es256", bundleClaims(f.b, digest))
	if err != nil {
		t.Fatal(err)
	}
}
