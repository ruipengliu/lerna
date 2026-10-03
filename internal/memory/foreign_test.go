package memory_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// PostgreSQL 矩阵为每个 owner 建独立临时数据库，不把同库分区当跨库证据。
func newForeignFixture(t *testing.T) fixture {
	t.Helper()
	if os.Getenv("HARNESS_FOREIGN_TEST_DRIVER") != "postgres" {
		return newFixture(t)
	}
	dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("explicit PostgreSQL verification requires HARNESS_TEST_POSTGRES_DSN")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.ToLower(api.NewID("foreign"))
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		_ = admin.Close(ctx)
		t.Fatal(err)
	}
	ownerDSN := dsn + " dbname=" + name
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal("invalid configured PostgreSQL URL")
		}
		u.Path = "/" + name
		ownerDSN = u.String()
	}
	store, err := postgres.Open(ctx, ownerDSN, postgres.WithMaxConnections(4))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		if err := admin.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return newFixtureWithStore(t, store)
}

// 测试 seam 是两个真实 Memory owner 的命令/字节入口与受信签名端口。
// 端口直接转交原方法，后续故障只控制答复/可达性，不替代业务或 SQL。
type foreignAuthority struct {
	source        fixture
	keys          *platform.Keyring
	consumer      runtime.Scope
	offline       bool
	loseRegister  bool
	registerCalls int
}

func (p *foreignAuthority) signed(ctx context.Context, auth runtime.Auth, in memory.ForeignReference, control bool) (memory.ForeignProof, error) {
	if p.offline {
		return memory.ForeignProof{}, api.E("dependency_unavailable", "original_source_unreachable")
	}
	proof, err := p.source.service.CurrentForeignCopy(ctx, p.source.scope, auth, in, control)
	if err != nil {
		return proof, err
	}
	digest, err := api.Digest(proof)
	if err != nil {
		return proof, err
	}
	proof.Proof, err = p.keys.Sign("development-es256", platform.ProofClaims{TenantID: in.ContentRef.TenantID, Issuer: in.ContentRef.OwnerID, Audience: p.consumer.OwnerID, Purpose: "executor_content", ObjectRef: api.ObjectRef{TenantID: in.ContentRef.TenantID, OwnerID: in.ContentRef.OwnerID, ObjectID: in.ContentRef.ContentID, Revision: in.ContentRef.Version}, Digest: digest, ControlRevision: proof.ControlRevision, WindowID: in.CopyID, IssuedAt: proof.IssuedAt, StartBefore: proof.StartBefore})
	return proof, err
}
func (p *foreignAuthority) RegisterCopy(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in memory.ForeignReference) (memory.ForeignProof, error) {
	p.registerCalls++
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: p.source.scope.OwnerID, CommandID: in.RegisterCommandID, Method: "content.register_copy", TargetID: in.ContentRef.ContentID, ExpiresAt: in.RetainUntil, Payload: api.Raw(memory.RegisterCopyInput{CopyID: in.CopyID, ContentRef: in.ContentRef, HolderRef: in.HolderRef, Purpose: in.Purpose, Location: in.Location, RetainUntil: in.RetainUntil, ReferenceIntentRef: in.ReferenceIntentRef})}
	r, err := p.source.dispatcher.Command(ctx, auth, api.Raw(c))
	if err != nil {
		return memory.ForeignProof{}, err
	}
	if r.Error != nil {
		return memory.ForeignProof{}, r.Error
	}
	if p.loseRegister {
		p.loseRegister = false
		return memory.ForeignProof{}, api.E("dependency_unavailable", "registered_reply_lost")
	}
	return p.signed(ctx, auth, in, false)
}

func TestForeignRegistrationLostReplyRecoversOriginalAndOfflineStopsReads(t *testing.T) {
	source, local := newForeignFixture(t), newForeignFixture(t)
	local.scope.TenantID, local.auth = source.scope.TenantID, source.auth
	keys, err := platform.NewDevelopmentKey(source.scope.TenantID, source.scope.OwnerID, []string{"executor_content"})
	if err != nil {
		t.Fatal(err)
	}
	port := &foreignAuthority{source: source, consumer: local.scope, keys: keys, loseRegister: true}
	local.service.Foreign = port
	ref := source.upload(t, "登记答复丢失，恢复原副本")
	in := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: local.scope.Ref(api.NewID("intent"), 1), HolderRef: local.auth.Ref(local.scope.OwnerID), Purpose: "task.goal", Location: "local", RetainUntil: api.Time(time.Now().Add(20 * time.Minute))}
	if _, err = local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("lost original registration: %v", err)
	}
	use, err := local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in)
	if err != nil || use.Proof.CopyID != in.CopyID || port.registerCalls != 2 {
		t.Fatalf("original recovery: %+v %v calls%d", use, err, port.registerCalls)
	}
	changed := in
	changed.RegisterCommandID = api.NewID("command")
	if _, err = local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, changed); !api.IsCode(err, "idempotency_conflict") {
		t.Fatalf("replacement registration: %v", err)
	}
	port.offline = true
	if _, err = local.service.ReadBytes(local.ctx, local.scope, local.auth, ref, "task.goal", "local"); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("offline new read: %v", err)
	}
	port.offline = false
	one := uint64(1)
	r := source.command(t, "content.close", ref.ContentID, &one, memory.CloseInput{ContentRef: ref, Reason: "保留原控制收尾"})
	if r.Stage != "applied" {
		t.Fatal(r)
	}
	control, err := local.service.ControlForeignCopy(local.ctx, local.scope, local.auth, in.CopyID)
	if err != nil || control.Proof.ControlRevision != 2 || control.Proof.UseState != "closing" {
		t.Fatalf("original control: %+v %v", control, err)
	}
	// 当前同主体新凭据只可收尾原 holder；它不能复用旧登记的新读取许可。
	local.auth.CredentialGeneration = 2
	if _, err = local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in); !api.IsCode(err, "forbidden") {
		t.Fatalf("new credential opened original read registration: %v", err)
	}
	if err = local.service.StopForeignCopy(local.ctx, local.scope, local.auth, in.CopyID); err != nil {
		t.Fatal(err)
	}
	control, err = local.service.ControlForeignCopy(local.ctx, local.scope, local.auth, in.CopyID)
	if err != nil || control.Proof.UseState != "use_stopped" || control.Proof.CleanupState != "pending" {
		t.Fatalf("source stop separate from erase proof: %+v %v", control, err)
	}
}
func (p *foreignAuthority) Current(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in memory.ForeignReference) (memory.ForeignProof, error) {
	return p.signed(ctx, auth, in, false)
}
func (p *foreignAuthority) Control(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in memory.ForeignReference) (memory.ForeignProof, error) {
	return p.signed(ctx, auth, in, true)
}
func (p *foreignAuthority) Read(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in memory.ForeignReference, proof memory.ForeignProof) ([]byte, error) {
	return p.source.service.ReadBytes(ctx, p.source.scope, auth, in.ContentRef, in.Purpose, in.Location)
}
func (p *foreignAuthority) Release(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in memory.ForeignReference, report memory.ReleaseCopyInput) (memory.ForeignProof, error) {
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: p.source.scope.OwnerID, CommandID: in.ReleaseCommandID, Method: "content.release_copy", TargetID: in.ContentRef.ContentID, ExpiresAt: in.RetainUntil, Payload: api.Raw(report)}
	r, err := p.source.dispatcher.Command(ctx, auth, api.Raw(c))
	if err != nil {
		return memory.ForeignProof{}, err
	}
	if r.Error != nil {
		return memory.ForeignProof{}, r.Error
	}
	return p.signed(ctx, auth, in, true)
}
func (p *foreignAuthority) VerifyTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in memory.ForeignReference, proof memory.ForeignProof) error {
	unsigned := proof
	unsigned.Proof = ""
	digest, err := api.Digest(unsigned)
	if err != nil {
		return err
	}
	_, err = p.keys.VerifySource(proof.Proof, platform.ProofClaims{TenantID: in.ContentRef.TenantID, Issuer: in.ContentRef.OwnerID, Audience: tx.Scope().OwnerID, Purpose: "executor_content", ObjectRef: api.ObjectRef{TenantID: in.ContentRef.TenantID, OwnerID: in.ContentRef.OwnerID, ObjectID: in.ContentRef.ContentID, Revision: in.ContentRef.Version}, Digest: digest, ControlRevision: proof.ControlRevision, WindowID: in.CopyID, IssuedAt: proof.IssuedAt, StartBefore: proof.StartBefore})
	return err
}

func TestForeignContentKeepsOriginalOwnerAndRequiresCurrentSource(t *testing.T) {
	source, local := newForeignFixture(t), newForeignFixture(t)
	// 独立数据库、两个 owner；受信同一 tenant/原主体。
	local.scope.TenantID, local.auth = source.scope.TenantID, source.auth
	keys, err := platform.NewDevelopmentKey(source.scope.TenantID, source.scope.OwnerID, []string{"executor_content"})
	if err != nil {
		t.Fatal(err)
	}
	port := &foreignAuthority{source: source, consumer: local.scope, keys: keys}
	local.service.Foreign = port
	ref := source.upload(t, "设备原始结果，不换 owner 和版本")
	in := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: local.scope.Ref(api.NewID("intent"), 1), HolderRef: local.auth.Ref(local.scope.OwnerID), Purpose: "task.goal", Location: "local", RetainUntil: api.Time(time.Now().Add(20 * time.Minute))}
	use, err := local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in)
	if err != nil || use.Proof.ContentRef != ref {
		t.Fatalf("foreign registration: %+v %v", use, err)
	}
	control, err := local.service.ControlForeignCopy(local.ctx, local.scope, local.auth, in.CopyID)
	if err != nil {
		t.Fatal(err)
	}
	controlCtx, err := memory.WithForeignUses(local.ctx, []memory.ForeignUse{control})
	if err != nil {
		t.Fatal(err)
	}
	_, err = local.service.Store.Within(controlCtx, local.scope, []string{"content", "memory"}, func(tx runtime.Tx) error {
		_, err := local.service.CheckContentTx(controlCtx, tx, local.auth, ref, "task.goal", "local", false)
		return err
	})
	if !api.IsCode(err, "forbidden") {
		t.Fatalf("control proof granted body use: %v", err)
	}
	body, err := local.service.ReadBytes(local.ctx, local.scope, local.auth, ref, "task.goal", "local")
	if err != nil || string(body) != "设备原始结果，不换 owner 和版本" {
		t.Fatalf("original content: %q %v", body, err)
	}
	one := uint64(1)
	r := source.command(t, "content.close", ref.ContentID, &one, memory.CloseInput{ContentRef: ref, Reason: "主动关闭源"})
	if r.Stage != "applied" {
		t.Fatalf("source close: %+v", r)
	}
	if _, err = local.service.ReadBytes(local.ctx, local.scope, local.auth, ref, "task.goal", "local"); !api.IsCode(err, "forbidden") {
		t.Fatalf("closed mirror allowed: %v", err)
	}
}

func TestForeignSourcePublicationIntersectsOriginalPolicyAndOwnerKey(t *testing.T) {
	source, local := newForeignFixture(t), newForeignFixture(t)
	local.scope.TenantID, local.auth = source.scope.TenantID, source.auth
	if err := local.service.InstallPolicy(local.ctx, local.scope, local.auth, source.policy); err != nil {
		t.Fatal(err)
	}
	local.policy = source.policy
	keys, err := platform.NewDevelopmentKey(source.scope.TenantID, source.scope.OwnerID, []string{"executor_content"})
	if err != nil {
		t.Fatal(err)
	}
	local.service.Foreign = &foreignAuthority{source: source, consumer: local.scope, keys: keys}
	ref := source.upload(t, "foreign input for local derivation")
	var uses []memory.ForeignUse
	for _, purpose := range []string{"content.write", "task.goal"} {
		in := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: local.scope.Ref(api.NewID("intent"), 1), HolderRef: local.auth.Ref(local.scope.OwnerID), Purpose: purpose, Location: "local", RetainUntil: api.Time(time.Now().Add(20 * time.Minute))}
		use, err := local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in)
		if err != nil {
			t.Fatal(err)
		}
		uses = append(uses, use)
	}
	ctx, err := memory.WithForeignUses(local.ctx, uses)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("derived result retains exact foreign provenance")
	// 相同 content_id/version 的外部来源不会被误当成本方自引用。
	derived := api.ContentRef{TenantID: local.scope.TenantID, OwnerID: local.scope.OwnerID, ContentID: ref.ContentID, Version: ref.Version, Hash: api.Hash(body), MediaType: "text/plain", ByteLength: uint64(len(body))}
	request := memory.PublicationRequest{ContentRef: derived, TransferID: api.NewID("transfer"), ReserveCommandID: api.NewID("command"), PutCommandID: api.NewID("command"), PolicyRef: local.policy.PolicyRef, ProcessedSources: []api.ContentRef{ref}, DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(time.Now().Add(10 * time.Minute)), TransferDeadline: api.Time(time.Now().Add(time.Minute))}
	if _, err = local.service.Upload(ctx, local.scope, local.auth, request, body); err != nil {
		t.Fatalf("foreign original derived publication: %v", err)
	}
	got, err := local.service.ReadBytes(local.ctx, local.scope, local.auth, derived, "task.goal", "local")
	if err != nil || string(got) != string(body) {
		t.Fatalf("original derived: %q %v", got, err)
	}
	one := uint64(1)
	source.command(t, "content.close", ref.ContentID, &one, memory.CloseInput{ContentRef: ref, Reason: "foreign closure propagates without owner collision"})
	if _, err = local.service.ReadBytes(local.ctx, local.scope, local.auth, derived, "task.goal", "local"); !api.IsCode(err, "forbidden") {
		t.Fatalf("closed foreign derivation: %v", err)
	}
	_, err = local.service.Store.Within(ctx, local.scope, []string{"content", "memory"}, func(tx runtime.Tx) error {
		_, err := local.service.CheckContentTx(ctx, tx, local.auth, ref, "content.write", "local", false)
		return err
	})
	if !api.IsCode(err, "forbidden") {
		t.Fatalf("old proof for another purpose reopened known-closed source: %v", err)
	}
}

func TestForeignIntentCommitUnknownStopsRPCAndReopensOriginalIdentity(t *testing.T) {
	source := newForeignFixture(t)
	path := t.TempDir() + "/foreign.sqlite"
	var lose atomic.Bool
	store, err := sqlite.Open(path, sqlite.WithCommitFault(func(phase sqlite.CommitPhase) error {
		if phase == sqlite.AfterCommit && lose.Swap(false) {
			return errors.New("lost original reference-intent commit reply")
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.Migrate(source.ctx); err != nil {
		t.Fatal(err)
	}
	local := newFixtureWithStore(t, store)
	local.scope.TenantID, local.auth = source.scope.TenantID, source.auth
	keys, err := platform.NewDevelopmentKey(source.scope.TenantID, source.scope.OwnerID, []string{"executor_content"})
	if err != nil {
		t.Fatal(err)
	}
	port := &foreignAuthority{source: source, consumer: local.scope, keys: keys}
	local.service.Foreign = port
	ref := source.upload(t, "original intent must be confirmed before remote registration")
	in := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: local.scope.Ref(api.NewID("intent"), 1), HolderRef: local.auth.Ref(local.scope.OwnerID), Purpose: "task.goal", Location: "local", RetainUntil: api.Time(time.Now().Add(20 * time.Minute))}
	lose.Store(true)
	if _, err = local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in); !errors.Is(err, runtime.ErrCommitUnknown) || port.registerCalls != 0 {
		t.Fatalf("unknown local commit sent remote request: %v calls%d", err, port.registerCalls)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(path, sqlite.WithExpectedDatabaseID(local.scope.DatabaseID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	local.service = memory.New(reopened, local.service.Objects)
	if port.registerCalls != 0 {
		t.Fatal("construction sent registration")
	}
	local.service.Foreign = port
	use, err := local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in)
	if err != nil || use.Reference != in || port.registerCalls != 1 {
		t.Fatalf("original reopened reference: %+v %v calls%d", use, err, port.registerCalls)
	}
	other := local.auth
	other.SubjectID = api.NewID("subject")
	if _, err = local.service.PrepareForeignUse(local.ctx, local.scope, other, in); !api.IsCode(err, "forbidden") {
		t.Fatalf("other actor accepted original reference: %v", err)
	}
	foreignTenant := in
	foreignTenant.ContentRef.TenantID = api.NewID("tenant")
	if _, err = local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, foreignTenant); !api.IsCode(err, "forbidden") {
		t.Fatalf("cross tenant original reference: %v", err)
	}
	local.service.Foreign = nil
	if _, err = local.service.ReadBytes(local.ctx, local.scope, local.auth, ref, "task.goal", "local"); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("nil port opened disk mirror: %v", err)
	}
}

func TestMemoryKeepsForeignSourceRegistrationAndDeletesOnlyItsMetadata(t *testing.T) {
	source, local := newForeignFixture(t), newForeignFixture(t)
	local.scope.TenantID, local.auth = source.scope.TenantID, source.auth
	local.policy = source.policy
	if err := local.service.InstallPolicy(local.ctx, local.scope, local.auth, source.policy); err != nil {
		t.Fatal(err)
	}
	keys, err := platform.NewDevelopmentKey(source.scope.TenantID, source.scope.OwnerID, []string{"executor_content"})
	if err != nil {
		t.Fatal(err)
	}
	port := &foreignAuthority{source: source, consumer: local.scope, keys: keys}
	local.service.Foreign = port
	ref := source.upload(t, "可解释的外部事实原断言")
	var uses []memory.ForeignUse
	var copyID string
	for _, purpose := range []string{"memory.save", "memory.read"} {
		in := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: local.scope.Ref(api.NewID("intent"), 1), HolderRef: local.auth.Ref(local.scope.OwnerID), Purpose: purpose, Location: "local", RetainUntil: api.Time(time.Now().Add(20 * time.Minute))}
		use, err := local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in)
		if err != nil {
			t.Fatal(err)
		}
		uses = append(uses, use)
		copyID = in.CopyID
	}
	ctx, err := memory.WithForeignUses(local.ctx, uses)
	if err != nil {
		t.Fatal(err)
	}
	local.ctx = ctx
	values := memory.MemoryValues{Type: "fact", ContentRef: ref, ScopeRef: local.upload(t, "外部事实保存范围"), PolicyRef: source.policy.PolicyRef, Sources: []api.SourceEvidence{}, ObservedAt: api.Time(time.Now())}
	id := api.NewID("memory")
	r := local.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: values})
	if r.Stage != "applied" {
		t.Fatalf("foreign memory create: %+v", r)
	}
	record, err := local.service.ReadMemory(ctx, local.scope, local.auth, memory.ReadMemoryInput{MemoryID: id, Purpose: "memory.read"})
	if err != nil || record.Values.ContentRef != ref {
		t.Fatalf("foreign original memory: %+v %v", record, err)
	}
	one := uint64(1)
	r = local.command(t, "memory.delete", id, &one, memory.DeleteInput{MemoryID: id, Reason: "删除记录不关闭独立源"})
	if r.Stage != "applied" {
		t.Fatal(r)
	}
	if err = runtime.Drain(local.ctx, local.service.Store, local.scope, local.dispatcher.Registry, 100); err != nil {
		t.Fatal(err)
	}
	record, err = local.service.InspectMemory(local.ctx, local.scope, local.auth, id)
	if err != nil || record.CleanupState != "complete" {
		t.Fatalf("metadata cleanup: %+v %v", record, err)
	}
	control, err := local.service.ControlForeignCopy(local.ctx, local.scope, local.auth, copyID)
	if err != nil || control.Proof.UseState != "allowed" {
		t.Fatalf("deleting metadata closed separate original copy: %+v %v", control, err)
	}
}
