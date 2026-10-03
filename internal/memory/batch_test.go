package memory_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

func TestContinuousIndexProcessesChangeAfterTheFirstTwoHundred(t *testing.T) {
	f := newFixture(t)
	values := f.values(t, "共享正文不代表共享Memory身份")
	for i := 0; i < 201; i++ {
		id := api.NewID("memory")
		r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: values})
		if r.Stage != "applied" {
			t.Fatalf("create %d: %+v", i, r)
		}
	}
	if err := runtime.Drain(f.ctx, f.service.Store, f.scope, f.dispatcher.Registry, 50); err != nil {
		t.Fatal(err)
	}
	status, err := f.service.IndexStatus(f.ctx, f.scope, f.auth)
	if err != nil || status.ChangeHead != 201 || status.ContiguousWatermark != 201 || status.State != "ready" {
		t.Fatalf("index stopped after the first bounded page: %+v %v", status, err)
	}
}

func TestSourceImpactProcessesTheTwentyFirstDerivedContentAndMemory(t *testing.T) {
	f := newFixture(t)
	source := f.upload(t, "多个派生共同依赖的准确来源")
	scope := f.upload(t, "本人派生记忆范围")
	ids := make([]string, 21)
	for i := range ids {
		ids[i] = api.NewID("memory")
		values := memory.MemoryValues{Type: "inference", ContentRef: f.upload(t, fmt.Sprintf("派生推断%d", i), source), Sources: []api.SourceEvidence{}, ScopeRef: scope, PolicyRef: f.policy.PolicyRef, ObservedAt: api.Time(time.Now())}
		r := f.command(t, "memory.create", ids[i], nil, memory.CreateInput{MemoryID: ids[i], Values: values})
		if r.Stage != "applied" {
			t.Fatalf("create %d: %+v", i, r)
		}
	}
	one := uint64(1)
	r := f.command(t, "content.close", source.ContentID, &one, memory.CloseInput{ContentRef: source, Reason: "撤销所有已处理来源，包括无披露引用的来源"})
	if r.Stage != "applied" {
		t.Fatalf("close: %+v", r)
	}
	if err := runtime.Drain(f.ctx, f.service.Store, f.scope, f.dispatcher.Registry, 500); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		record, err := f.service.InspectMemory(f.ctx, f.scope, f.auth, id)
		if err != nil || record.State != "needs_review" {
			t.Fatalf("bounded propagation missed current dependent %s: %+v %v", id, record, err)
		}
	}
}

func TestSecondRestrictionRechecksAlreadyRevisedDependentMemory(t *testing.T) {
	f := newFixture(t)
	parent := api.NewID("memory")
	values := f.values(t, "可以收窄两次的原断言")
	r := f.command(t, "memory.create", parent, nil, memory.CreateInput{MemoryID: parent, Values: values})
	if r.Stage != "applied" {
		t.Fatalf("parent create: %+v", r)
	}
	child := api.NewID("memory")
	derived := values
	derived.Type = "inference"
	derived.ContentRef = f.upload(t, "仍使用原断言的推断", values.ContentRef)
	r = f.command(t, "memory.create", child, nil, memory.CreateInput{MemoryID: child, Values: derived})
	if r.Stage != "applied" {
		t.Fatalf("child create: %+v", r)
	}
	narrow := f.policy.Values
	for step, removed := range []string{"memory.extract", "memory.query"} {
		purposes := []string{}
		for _, purpose := range narrow.Purposes {
			if purpose != removed {
				purposes = append(purposes, purpose)
			}
		}
		narrow.Purposes = purposes
		digest, _ := api.Digest(narrow)
		policy, err := memory.NewPolicy(api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}, narrow)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.service.InstallPolicy(f.ctx, f.scope, f.auth, policy); err != nil {
			t.Fatal(err)
		}
		expected := uint64(step + 1)
		r = f.command(t, "memory.restrict", parent, &expected, memory.RestrictInput{MemoryID: parent, RestrictedPolicyRef: policy.PolicyRef})
		if r.Stage != "applied" {
			t.Fatalf("restrict %d: %+v", step, r)
		}
		if err = runtime.Drain(f.ctx, f.service.Store, f.scope, f.dispatcher.Registry, 100); err != nil {
			t.Fatal(err)
		}
		current, err := f.service.InspectMemory(f.ctx, f.scope, f.auth, child)
		if err != nil || current.State != "active" || current.Revision != uint64(step+2) {
			t.Fatalf("second scope event skipped current dependent: %+v %v", current, err)
		}
	}
}
