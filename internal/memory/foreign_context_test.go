package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

type currentFlowUses struct {
	uses []memory.ForeignUse
	err  error
}

func (p *currentFlowUses) ForeignUses() ([]memory.ForeignUse, error) { return p.uses, p.err }
func TestForeignUseProviderCarriesFreshProofWithoutReplacingPureCurrentGate(t *testing.T) {
	source, local := newForeignFixture(t), newForeignFixture(t)
	local.scope.TenantID, local.auth = source.scope.TenantID, source.auth
	keys, err := platform.NewDevelopmentKey(source.scope.TenantID, source.scope.OwnerID, []string{"executor_content"})
	if err != nil {
		t.Fatal(err)
	}
	port := &foreignAuthority{source: source, consumer: local.scope, keys: keys}
	local.service.Foreign = port
	ref := source.upload(t, "original source bytes for one entry")
	in := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: local.scope.Ref(api.NewID("intent"), 1), HolderRef: local.auth.Ref(local.scope.OwnerID), Purpose: "task.goal", Location: "local", RetainUntil: api.Time(time.Now().Add(10 * time.Minute))}
	use, err := local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in)
	if err != nil {
		t.Fatal(err)
	}
	p := &currentFlowUses{}
	ctx := memory.WithForeignUseProvider(context.Background(), p)
	check := func(ctx context.Context) error {
		_, err := local.service.Store.Within(ctx, local.scope, []string{"content", "memory"}, func(tx runtime.Tx) error {
			_, err := local.service.CheckContentTx(ctx, tx, local.auth, ref, "task.goal", "local", false)
			return err
		})
		return err
	}
	if err = check(ctx); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("empty entry borrowed persisted proof: %v", err)
	}
	p.uses = []memory.ForeignUse{use}
	// 提供方只读当次内存；所有签名、代次和本方已知否决仍由真正门禁检查。
	port.offline = true
	if err = check(ctx); err != nil {
		t.Fatalf("fresh actual flow proof rejected: %v", err)
	}
	wrong := use
	wrong.Proof.SubjectRef.Revision++
	p.uses = []memory.ForeignUse{wrong}
	if err = check(ctx); err == nil {
		t.Fatal("wrong credential generation bypassed pure gate")
	}
	p.uses = []memory.ForeignUse{use}
	ctx2 := memory.WithForeignUseProvider(ctx, &currentFlowUses{})
	if err = check(ctx2); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("next entry inherited positive proof: %v", err)
	}
	sentinel := errors.New("actual carrier failure")
	p.err = sentinel
	if err = check(ctx); !errors.Is(err, sentinel) {
		t.Fatalf("provider error reclassified: %v", err)
	}
	p.err = nil
	port.offline = false
	one := uint64(1)
	if r := source.command(t, "content.close", ref.ContentID, &one, memory.CloseInput{ContentRef: ref, Reason: "current original source revoked"}); r.Stage != "applied" {
		t.Fatal(r)
	}
	if _, err = local.service.ControlForeignCopy(local.ctx, local.scope, local.auth, in.CopyID); err != nil {
		t.Fatal(err)
	}
	if err = check(ctx); err == nil {
		t.Fatal("old positive flow defeated currently known source close")
	}
}

func TestForeignUseProviderSelectsExactCurrentHolderForEachActualSubject(t *testing.T) {
	source, local := newForeignFixture(t), newForeignFixture(t)
	local.scope.TenantID, local.auth = source.scope.TenantID, source.auth
	second := local.auth
	second.SubjectID = api.NewID("subject")
	values := source.policy.Values
	values.Subjects = []string{source.auth.SubjectID, second.SubjectID}
	digest, _ := api.Digest(values)
	policy, err := memory.NewPolicy(api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}, values)
	if err != nil {
		t.Fatal(err)
	}
	if err = source.service.InstallPolicy(source.ctx, source.scope, source.auth, policy); err != nil {
		t.Fatal(err)
	}
	source.policy = policy
	keys, err := platform.NewDevelopmentKey(source.scope.TenantID, source.scope.OwnerID, []string{"executor_content"})
	if err != nil {
		t.Fatal(err)
	}
	port := &foreignAuthority{source: source, consumer: local.scope, keys: keys}
	local.service.Foreign = port
	ref := source.upload(t, "same original bytes, two exactly authorized holders")
	uses := []memory.ForeignUse{}
	for _, actor := range []runtime.Auth{local.auth, second} {
		in := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: local.scope.Ref(api.NewID("intent"), 1), HolderRef: actor.Ref(local.scope.OwnerID), Purpose: "task.goal", Location: "local", RetainUntil: api.Time(time.Now().Add(10 * time.Minute))}
		use, err := local.service.PrepareForeignUse(local.ctx, local.scope, actor, in)
		if err != nil {
			t.Fatal(err)
		}
		uses = append(uses, use)
	}
	ctx := memory.WithForeignUseProvider(context.Background(), &currentFlowUses{uses: uses})
	check := func(actor runtime.Auth) error {
		_, err := local.service.Store.Within(ctx, local.scope, []string{"content", "memory"}, func(tx runtime.Tx) error {
			_, err := local.service.CheckContentTx(ctx, tx, actor, ref, "task.goal", "local", false)
			return err
		})
		return err
	}
	if err = check(second); err != nil {
		t.Fatalf("second actual signed holder misselected: %v", err)
	}
	if err = check(local.auth); err != nil {
		t.Fatalf("first actual signed holder rejected: %v", err)
	}
	wrong := second
	wrong.CredentialGeneration++
	if err = check(wrong); err == nil {
		t.Fatal("old holder authorized newer generation")
	}
	wrong = second
	wrong.SubjectID = api.NewID("subject")
	if err = check(wrong); err == nil {
		t.Fatal("another actor borrowed another subject's copy")
	}
}
