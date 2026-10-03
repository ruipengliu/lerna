package interaction_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestClosedPresentationInvalidatesOldBodyAndNotModifiedCallbacks(t *testing.T) {
	f := newApplication(t)
	snapshot := f.upload(t, "{\"title\":\"耐久界面\"}")
	surfaceID := api.NewID("surface")
	f.command(t, "surface.create", surfaceID, nil, interaction.SurfaceInput{BindingRef: f.binding, SnapshotRef: snapshot, RequestRefs: []api.ObjectRef{}})
	presentationID := api.NewID("presentation")
	r := f.command(t, "presentation.open", presentationID, nil, interaction.OpenPresentationInput{EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), SurfaceRef: f.scope.Ref(surfaceID, 1)})
	var p interaction.Presentation
	if e := api.Decode(r.Output, &p); e != nil {
		t.Fatal(e)
	}
	rev := p.Revision
	r = f.command(t, "presentation.begin", presentationID, &rev, interaction.BeginPresentationInput{IntentRevision: p.IntentRevision})
	if e := api.Decode(r.Output, &p); e != nil {
		t.Fatal(e)
	}
	read := interaction.RenderReadInput{Generation: p.Generation, IntentRevision: p.IntentRevision, KnownHash: snapshot.Hash}
	view, e := f.s.ReadPresentation(f.ctx, f.store, f.scope, f.auth, presentationID, read)
	if e != nil || !view.NotModified || view.Presentation.Presented {
		t.Fatalf("cache callback asserted presented %+v %v", view, e)
	}
	rev = p.Revision
	f.command(t, "presentation.close", presentationID, &rev, interaction.ClosePresentationInput{Reason: "用户关窗"})
	if _, e = f.s.ReadPresentation(f.ctx, f.store, f.scope, f.auth, presentationID, read); !api.IsCode(e, "invalid_state") {
		t.Fatalf("old cache callback survived close %v", e)
	}
}

func TestGoalFormUsesOwnerClosedVariantsAndExpiresEvenOnCacheHit(t *testing.T) {
	f := newApplication(t)
	goal, question, snapshot := f.upload(t, "需要用户补充目标"), f.upload(t, "请选择 answer 或 report"), f.upload(t, `{"title":"目标表单"}`)
	taskID := api.NewID("task")
	r := f.command(t, "task.submit", taskID, nil, task.SubmitInput{OrchestratorID: f.scope.OwnerID, GoalRef: goal, PolicyRef: f.policy, Deadline: api.Time(time.Now().Add(20 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
	var submitted task.TaskOutput
	if e := api.Decode(r.Output, &submitted); e != nil {
		t.Fatal(e)
	}
	fact, e := f.task.Read(f.ctx, f.store, f.scope, f.auth, taskID)
	if e != nil {
		t.Fatal(e)
	}
	expires := time.Now().Add(20 * time.Second)
	version := fact.GoalRevision
	request := api.InputRequest{RequestID: api.NewID("request"), TargetRef: submitted.TaskRef, GoalRevision: &version, Purpose: "clarify_goal", QuestionRef: question, AnswerSchemaRef: f.goalAnswerSchema, PreviewRefs: []api.ContentRef{goal}, ExpiresAt: api.Time(expires), State: "pending"}
	trusted := f.auth
	trusted.Roles = append(trusted.Roles, "service")
	var ref api.ObjectRef
	status, e := f.store.Within(f.ctx, f.scope, []string{"task", "content", "memory", "interaction"}, func(tx runtime.Tx) error {
		var e error
		ref, e = f.task.CreateInputTx(f.ctx, tx, trusted, taskID, request)
		return e
	})
	if e != nil || status != runtime.Committed {
		t.Fatalf("create request %s %v", status, e)
	}
	surfaceID, presentationID := api.NewID("surface"), api.NewID("presentation")
	f.command(t, "surface.create", surfaceID, nil, interaction.SurfaceInput{BindingRef: f.binding, SnapshotRef: snapshot, RequestRefs: []api.ObjectRef{ref}})
	r = f.command(t, "presentation.open", presentationID, nil, interaction.OpenPresentationInput{EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), SurfaceRef: f.scope.Ref(surfaceID, 1)})
	var p interaction.Presentation
	if e = api.Decode(r.Output, &p); e != nil {
		t.Fatal(e)
	}
	rev := p.Revision
	r = f.command(t, "presentation.begin", presentationID, &rev, interaction.BeginPresentationInput{IntentRevision: p.IntentRevision})
	if e = api.Decode(r.Output, &p); e != nil {
		t.Fatal(e)
	}
	read := interaction.RenderReadInput{Generation: p.Generation, IntentRevision: p.IntentRevision}
	view, e := f.s.ReadPresentation(f.ctx, f.store, f.scope, f.auth, presentationID, read)
	if e != nil || len(view.Requests) != 1 || len(view.Bodies) != 3 {
		t.Fatalf("closed owner variants not rendered %+v %v", view, e)
	}
	timer := time.NewTimer(time.Until(expires) + 20*time.Millisecond)
	defer timer.Stop()
	<-timer.C
	read.KnownHash = snapshot.Hash
	if _, e = f.s.ReadPresentation(f.ctx, f.store, f.scope, f.auth, presentationID, read); !api.IsCode(e, "expired") {
		t.Fatalf("expired request still enabled through cache callback %v", e)
	}
}

func TestBodyDisclosureIsRecheckedAfterActualByteReadAndOnCacheHit(t *testing.T) {
	f := newApplication(t)
	snapshot := f.upload(t, "{\"title\":\"撤回披露\"}")
	surfaceID := api.NewID("surface")
	f.command(t, "surface.create", surfaceID, nil, interaction.SurfaceInput{BindingRef: f.binding, SnapshotRef: snapshot, RequestRefs: []api.ObjectRef{}})
	id := api.NewID("presentation")
	r := f.command(t, "presentation.open", id, nil, interaction.OpenPresentationInput{EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), SurfaceRef: f.scope.Ref(surfaceID, 1)})
	var p interaction.Presentation
	if e := api.Decode(r.Output, &p); e != nil {
		t.Fatal(e)
	}
	rev := p.Revision
	r = f.command(t, "presentation.begin", id, &rev, interaction.BeginPresentationInput{IntentRevision: p.IntentRevision})
	if e := api.Decode(r.Output, &p); e != nil {
		t.Fatal(e)
	}
	read := interaction.RenderReadInput{Generation: p.Generation, IntentRevision: p.IntentRevision}
	f.content.hook = func() {
		f.content.hook = nil
		one := uint64(1)
		f.command(t, "content.close", snapshot.ContentID, &one, memory.CloseInput{ContentRef: snapshot, Reason: "正文取回期间撤回披露"})
	}
	if _, e := f.s.ReadPresentation(f.ctx, f.store, f.scope, f.auth, id, read); !api.IsCode(e, "forbidden") {
		t.Fatalf("old body callback crossed content closure %v", e)
	}
	read.KnownHash = snapshot.Hash
	if _, e := f.s.ReadPresentation(f.ctx, f.store, f.scope, f.auth, id, read); !api.IsCode(e, "forbidden") {
		t.Fatalf("not_modified bypassed current qualification %v", e)
	}
}

func TestApplicationEventUsesOnlyRegisteredPayloadAndFixedDestination(t *testing.T) {
	f := newApplication(t)
	snapshot := f.upload(t, "{\"title\":\"动作\"}")
	surfaceID := api.NewID("surface")
	f.command(t, "surface.create", surfaceID, nil, interaction.SurfaceInput{BindingRef: f.binding, SnapshotRef: snapshot, RequestRefs: []api.ObjectRef{}})
	id := api.NewID("presentation")
	r := f.command(t, "presentation.open", id, nil, interaction.OpenPresentationInput{EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), SurfaceRef: f.scope.Ref(surfaceID, 1)})
	var p interaction.Presentation
	if e := api.Decode(r.Output, &p); e != nil {
		t.Fatal(e)
	}
	rev := p.Revision
	r = f.command(t, "presentation.begin", id, &rev, interaction.BeginPresentationInput{IntentRevision: p.IntentRevision})
	if e := api.Decode(r.Output, &p); e != nil {
		t.Fatal(e)
	}
	r = f.command(t, "presentation.rendered", id, nil, interaction.RenderAckInput{Generation: p.Generation, IntentRevision: p.IntentRevision, SurfaceRef: p.SurfaceRef, RenderedRefs: []api.ContentRef{snapshot}, RenderSuccess: true})
	if e := api.Decode(r.Output, &p); e != nil {
		t.Fatal(e)
	}
	r = f.command(t, "application_event", id, nil, interaction.ApplicationEventInput{Generation: p.Generation, IntentRevision: p.IntentRevision, SurfaceRef: p.SurfaceRef, Name: "archive", Payload: api.Raw(interaction.SessionControlInput{Reason: "通过受信绑定归档"})})
	var out interaction.ApplicationEventOutput
	if e := api.Decode(r.Output, &out); e != nil {
		t.Fatal(e)
	}
	if out.State != "queued" {
		t.Fatalf("event not durable queued %+v", out)
	}
	f.stepKind(t, interaction.JobApplicationEvent)
	event, e := f.s.ReadApplicationEvent(f.ctx, f.store, f.scope, f.auth, out.EventRef.ObjectID)
	if e != nil {
		t.Fatal(e)
	}
	session, e := f.s.ReadSession(f.ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{})
	if e != nil {
		t.Fatal(e)
	}
	if event.State != "applied" || session.Session.State != "archived" || event.Command.Method != "session.archive" || event.Command.TargetID != f.session {
		t.Fatalf("event arbitrary routing %+v session=%+v", event, session)
	}
}
