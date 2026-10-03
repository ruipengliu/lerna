package memory_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
)

func TestExperienceKeepsUnknownOutcomeAndCannotDeclareUnverifiedSuccess(t *testing.T) {
	f := newFixture(t)
	goal, result, boundary := f.upload(t, "实验目标"), f.upload(t, "效果尚未核验"), f.upload(t, "仅适用于该任务的边界")
	spec := memory.ExperienceSpec{GoalRef: goal, PrerequisiteRefs: []api.ContentRef{}, Outcome: "unknown", ResultRef: result, EvidenceRefs: []api.ContentRef{}, BoundaryRef: boundary}
	upload := func(spec memory.ExperienceSpec) (api.ContentRef, error) {
		body := api.Raw(spec)
		ref := api.ContentRef{TenantID: f.scope.TenantID, OwnerID: f.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(body), ByteLength: uint64(len(body)), MediaType: memory.ExperienceMediaType}
		expiry, _ := api.ParseTime(f.policy.Values.RetainUntil)
		return f.service.Upload(f.ctx, f.scope, f.auth, memory.PublicationRequest{ContentRef: ref, TransferID: api.NewID("upload"), ReserveCommandID: api.NewID("command"), PutCommandID: api.NewID("command"), PolicyRef: f.policy.PolicyRef, ProcessedSources: []api.ContentRef{goal, result, boundary}, DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(expiry.Add(-20 * time.Minute)), TransferDeadline: api.Time(time.Now().Add(time.Minute))}, body)
	}
	ref, err := upload(spec)
	if err != nil {
		t.Fatalf("unknown experience upload: %v", err)
	}
	values := f.values(t, "占位断言不会进入经验正文")
	values.Type = "experience"
	values.ContentRef = ref
	id := api.NewID("memory")
	r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: values})
	if r.Stage != "applied" {
		t.Fatalf("explicit unknown experience save: %+v", r)
	}
	spec.Outcome = "success"
	if _, err = upload(spec); !api.IsCode(err, "unsupported") {
		t.Fatalf("text declaration became verified successful experience: %v", err)
	}
}
