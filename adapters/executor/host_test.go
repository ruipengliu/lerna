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
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

type deviceFixture struct {
	ctx  context.Context
	h    *Host
	b    AdmissionBundle
	keys *platform.Keyring
	peer runtime.Auth
	raw  map[string][]byte
	root string
}

func newDeviceFixture(t *testing.T) *deviceFixture {
	t.Helper()
	b, keys := referenceBundle(t)
	root := t.TempDir()
	token := filepath.Join(root, ".peer")
	if err := os.WriteFile(token, []byte("finite-static-peer-token-only-not-user-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	var jwk struct {
		Kty string `json:"kty"`
		Crv string `json:"crv"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}
	if err := api.Decode(platform.PublicJWK(keys.Keys["development-es256"].Public), &jwk); err != nil {
		t.Fatal(err)
	}
	x, y := jwk.X, jwk.Y
	c := Config{Development: true, TenantID: b.Principal.TenantID, OwnerID: b.EndpointID, InstanceID: b.InstanceID, DatabasePath: filepath.Join(root, "device.db"), DataRoot: root, SigningKeyFile: filepath.Join(root, ".key.pem"), PeerTokenFile: token, Authority: TrustedAuthority{KeyID: "development-es256", OwnerID: b.AuthorityID, PublicX: x, PublicY: y}, Bindings: []Binding{{CapabilityRef: b.Intent.CapabilityRef, BindingRef: b.Intent.BindingRef, InstallLockRef: b.Intent.InstallLockRef, Resources: []string{"managed-files"}, Actions: []string{"file.write"}}}}
	h, err := Open(context.Background(), c, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if fErr := h.Close(); fErr != nil {
			t.Error(fErr)
		}
	})
	f := &deviceFixture{ctx: context.Background(), h: h, b: b, keys: keys, peer: runtime.Auth{TenantID: b.Principal.TenantID, SubjectID: b.AuthorityID, CredentialGeneration: 1, Roles: []string{"executor_peer"}}, raw: map[string][]byte{}, root: root}
	if err = os.Mkdir(filepath.Join(root, "files", "reports"), 0700); err != nil {
		t.Fatal(err)
	}
	text := []byte("independent cloud-admitted file bytes\n")
	ref := api.ContentRef{TenantID: b.Principal.TenantID, OwnerID: b.AuthorityID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(text), MediaType: "text/plain", ByteLength: uint64(len(text))}
	args := api.Raw(target.FileWriteArguments{Path: "reports/result.txt", ExpectedVersion: "absent", ContentRef: ref})
	f.b.Intent.ArgumentsRef.Hash = api.Hash(args)
	f.b.Intent.ArgumentsRef.ByteLength = uint64(len(args))
	f.b.Intent.ProcessedSourceRefs = []api.ContentRef{ref}
	f.b.Intent.DisclosedSourceRefs = []api.ContentRef{}
	f.b.IntentRef.Hash = api.Hash(api.Raw(f.b.Intent))
	f.b.IntentRef.ByteLength = uint64(len(api.Raw(f.b.Intent)))
	f.b.ExecutionHash, _ = api.Digest(f.b.Intent)
	f.b.Contents[0].ContentRef = f.b.IntentRef
	f.b.Contents[1].ContentRef = f.b.Intent.ArgumentsRef
	f.b.Contents = append(f.b.Contents, ContentPermission{ContentRef: ref, Purposes: []string{"managed_file_write", "managed_file_read"}, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetainUntil: f.b.StartBefore})
	f.raw[contentKey(ref)] = text
	f.raw[contentKey(f.b.Intent.ArgumentsRef)] = args
	f.raw[contentKey(f.b.IntentRef)] = api.Raw(f.b.Intent)
	allocation := f.b.Lease
	allocation.AllocationDigest = ""
	allocation.Proof = ""
	f.b.Lease.AllocationDigest, _ = api.Digest(allocation)
	f.b.Lease.Proof, err = (Proof{Keys: keys, SigningKeyID: "development-es256"}).SignLocal(governance.ProofStatement{TenantID: b.Principal.TenantID, IssuerID: b.AuthorityID, AudienceID: b.EndpointID, Purpose: "grant_lease", ObjectRef: b.LeaseRef, Digest: f.b.Lease.AllocationDigest, IssuedAt: f.b.Lease.IssuedAt, StartBefore: f.b.Lease.ExpiresAt})
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := BundleDigest(f.b)
	f.b.Proof, err = keys.Sign("development-es256", bundleClaims(f.b, digest))
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *deviceFixture) command(t *testing.T, method, targetID string, payload any) (api.Command, api.Receipt) {
	t.Helper()
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.h.Scope.OwnerID, CommandID: api.NewID("command"), Method: method, TargetID: targetID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(payload)}
	_, raw, err := f.h.Call(f.ctx, f.peer, "command", api.Raw(c))
	if err != nil {
		t.Fatal(err)
	}
	var receipt api.Receipt
	if err = api.Decode(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	return c, receipt
}
func deviceQuery[T any](t *testing.T, f *deviceFixture, method, targetID string, payload any) T {
	t.Helper()
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.h.Scope.OwnerID, QueryID: api.NewID("query"), Method: method, TargetID: targetID, Payload: api.Raw(payload)}
	_, raw, err := f.h.Call(f.ctx, f.peer, "query", api.Raw(q))
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err = api.Decode(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func (f *deviceFixture) prepare(t *testing.T) {
	t.Helper()
	_, r := f.command(t, "executor.admission.install", f.b.Intent.OperationID, f.b)
	if r.Stage != "applied" {
		t.Fatalf("install: %+v", r)
	}
	for _, p := range f.b.Contents {
		data := f.raw[contentKey(p.ContentRef)]
		for index := uint64(0); index < chunkCount(p.ContentRef.ByteLength); index++ {
			start := index * ChunkBytes
			end := start + ChunkBytes
			if end > uint64(len(data)) {
				end = uint64(len(data))
			}
			_, r = f.command(t, "executor.content.stage", p.ContentRef.ContentID, StageInput{BundleID: f.b.BundleID, ContentRef: p.ContentRef, ChunkIndex: index, ChunkCount: chunkCount(p.ContentRef.ByteLength), DataBase64: base64.StdEncoding.EncodeToString(data[start:end])})
			if r.Stage != "applied" {
				t.Fatalf("stage: %+v", r)
			}
		}
	}
	if err := runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	view := deviceQuery[AdmissionView](t, f, "executor.admission.get", f.b.Intent.OperationID, AdmissionID{f.b.BundleID})
	if !view.Complete || view.Denied {
		t.Fatalf("prepare: %+v", view)
	}
}
func (f *deviceFixture) control(t *testing.T, status, control string, revision uint64) api.ControlSnapshot {
	t.Helper()
	now := time.Now().UTC()
	c := api.ControlSnapshot{TaskID: f.b.Intent.TaskRef.ObjectID, OrchestratorID: f.b.AuthorityID, GoalRevision: f.b.Intent.GoalRevision, ControlRevision: revision, Status: status, Control: control, WindowID: api.NewID("window"), IssuedAt: api.Time(now), StartBefore: api.Time(now.Add(5 * time.Second))}
	digest, _ := api.Digest(c)
	claims := controlClaims(c, digest)
	claims.TenantID = f.b.Principal.TenantID
	claims.ObjectRef.TenantID = f.b.Principal.TenantID
	token, err := f.keys.Sign("development-es256", claims)
	if err != nil {
		t.Fatal(err)
	}
	c.ProofRef = api.ContentRef{TenantID: f.b.Principal.TenantID, OwnerID: f.b.AuthorityID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte(token)), MediaType: "application/jose", ByteLength: uint64(len(token))}
	_, r := f.command(t, "executor.control.install", c.TaskID, ControlDelivery{c, token})
	if r.Stage != "applied" {
		t.Fatalf("control: %+v", r)
	}
	return c
}
func (f *deviceFixture) invoke(t *testing.T, c api.ControlSnapshot) api.Command {
	t.Helper()
	in := execution.InvokeInput{OperationID: f.b.Intent.OperationID, TaskRef: f.b.Intent.TaskRef, GoalRevision: f.b.Intent.GoalRevision, ControlRevision: f.b.Intent.ControlRevision, CapabilityRef: f.b.Intent.CapabilityRef, BindingRef: f.b.Intent.BindingRef, IntentRef: f.b.IntentRef, IntentHash: f.b.ExecutionHash, UseRefs: f.b.UseRefs, Deadline: f.b.Intent.Deadline, ControlSnapshot: c, ReservationRef: f.b.ReservationRef}
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.b.EndpointID, CommandID: f.b.OriginalCommandID, Method: "execution.invoke", TargetID: f.b.Intent.OperationID, ExpiresAt: f.b.Intent.Deadline, Payload: api.Raw(in)}
	_, raw, err := f.h.Call(f.ctx, f.peer, "command", api.Raw(command))
	if err != nil {
		t.Fatal(err)
	}
	var receipt api.Receipt
	if err = api.Decode(raw, &receipt); err != nil || receipt.Stage != "applied" {
		t.Fatalf("invoke: %+v %v", receipt, err)
	}
	return command
}
func TestDeviceExecutesOnlyOriginalSignedAdmissionAndNoTaskParticipant(t *testing.T) {
	f := newDeviceFixture(t)
	f.prepare(t)
	c := f.control(t, "active", "running", 1)
	command := f.invoke(t, c)
	if err := runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	view := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if view.Operation.Effect != "applied" || len(view.Attempts.Items) != 1 || view.Attempts.Items[0].Phase != "reconciled" {
		var diagnostic struct {
			CancelReason string `json:"cancel_reason"`
		}
		if _, err := f.h.Store.Read(f.ctx, f.h.Scope, "execution.operations", f.b.Intent.OperationID, 0, &diagnostic); err == nil {
			t.Logf("original refusal: %s", diagnostic.CancelReason)
		}
		t.Fatalf("operation: %+v", view)
	}
	raw, err := os.ReadFile(filepath.Join(f.root, "files", "reports", "result.txt"))
	if err != nil || string(raw) != "independent cloud-admitted file bytes\n" {
		t.Fatalf("independent bytes: %q %v", raw, err)
	}
	var localLease governance.GrantLease
	if _, err = f.h.Store.Read(f.ctx, f.h.Scope, "governance/leases", f.b.Lease.LeaseID, 0, &localLease); err != nil {
		t.Fatal(err)
	}
	if !localLease.OnceConsumed || len(localLease.Reserved) != 1 {
		t.Fatalf("local once: %+v", localLease)
	}
	for _, contract := range f.h.Registry.Contracts() {
		if contract.Owner == "task" || contract.Owner == "brain" {
			t.Fatal("cloud participant registered on device")
		}
	}
	_, out, err := f.h.Call(f.ctx, f.peer, "command", api.Raw(command))
	if err != nil {
		t.Fatal(err)
	}
	var r api.Receipt
	if err = api.Decode(out, &r); err != nil || r.Stage != "applied" {
		t.Fatal("original lost reply not recoverable")
	}
	if err = runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	again := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if len(again.Attempts.Items) != 1 || again.Attempts.Items[0].AttemptID != view.Attempts.Items[0].AttemptID {
		t.Fatal("original retry created a physical attempt")
	}
}
func TestUnconfiguredExecutorRejectsBeforeOpeningBusinessState(t *testing.T) {
	root := t.TempDir()
	_, err := Open(context.Background(), Config{Development: true, DatabasePath: filepath.Join(root, "device.db"), DataRoot: root}, true)
	if !api.IsCode(err, "unsupported") {
		t.Fatalf("unpaired: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, "device.db")); !os.IsNotExist(err) {
		t.Fatal("unconfigured device opened business database")
	}
}
