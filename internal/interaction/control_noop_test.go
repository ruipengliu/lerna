package interaction_test

import (
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/conformance/testkit"
	"github.com/ruipengliu/lerna/internal/interaction"
	"testing"
	"time"
)

func TestCurrentCASRepeatedControlsPreserveFacts(t *testing.T) {
	f := newApplication(t)
	jobs := testkit.ObserveJobs(f.store)
	f.d.Store = jobs
	for _, method := range []string{"session.archive", "session.reopen", "session.delete"} {
		v, e := f.s.ReadSession(f.ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{})
		if e != nil {
			t.Fatal(e)
		}
		rev := v.Session.Revision
		first := f.command(t, method, f.session, &rev, interaction.SessionControlInput{Reason: "控制"})
		var out interaction.SessionOutput
		if e = api.Decode(first.Output, &out); e != nil {
			t.Fatal(e)
		}
		rev = out.SessionRef.Revision
		jobs.ForbidChanges = true
		repeated := f.command(t, method, f.session, &rev, interaction.SessionControlInput{Reason: "重复控制"})
		if !api.Equal(first.Output, repeated.Output) {
			t.Fatalf("%s changed current facts: %s -> %s", method, first.Output, repeated.Output)
		}
		assertCurrentControlChecks(t, f, method, f.session, rev, interaction.SessionControlInput{Reason: "同态仍核当前身份"})
		jobs.ForbidChanges = false
	}
	id := api.NewID("schedule")
	f.command(t, "schedule.create", id, nil, interaction.ScheduleInput{Spec: interaction.ScheduleSpec{Type: "interval", AnchorAt: api.Time(time.Now().Add(time.Hour).Truncate(time.Second)), EverySeconds: 60}, Timezone: "Etc/UTC", TZDBVersion: "2026b", TemplateRef: f.upload(t, "定时目标"), PolicyRef: f.policy, InstallLockRef: f.config, TaskTimeoutSeconds: 120, Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
	rev := uint64(1)
	first := f.command(t, "schedule.pause", id, &rev, interaction.ScheduleControlInput{Reason: "暂停"})
	var schedule interaction.ScheduleOutput
	if e := api.Decode(first.Output, &schedule); e != nil {
		t.Fatal(e)
	}
	rev = schedule.ScheduleRef.Revision
	originalJobs := api.Raw(jobs.Jobs)
	jobs.ForbidChanges = true
	repeated := f.command(t, "schedule.pause", id, &rev, interaction.ScheduleControlInput{Reason: "重复暂停"})
	if !api.Equal(first.Output, repeated.Output) {
		t.Fatalf("repeat pause changed facts: %s -> %s", first.Output, repeated.Output)
	}
	if !api.Equal(originalJobs, api.Raw(jobs.Jobs)) {
		t.Fatal("repeat pause changed original Job facts")
	}
	assertCurrentControlChecks(t, f, "schedule.pause", id, rev, interaction.ScheduleControlInput{Reason: "同态仍核当前身份"})
	jobs.ForbidChanges = false
	for _, method := range []string{"schedule.resume", "schedule.delete"} {
		view, e := f.s.ReadSchedule(f.ctx, f.store, f.scope, f.auth, id)
		if e != nil {
			t.Fatal(e)
		}
		rev = view.Revision
		first := f.command(t, method, id, &rev, interaction.ScheduleControlInput{Reason: "控制"})
		var out interaction.ScheduleOutput
		if e = api.Decode(first.Output, &out); e != nil {
			t.Fatal(e)
		}
		rev = out.ScheduleRef.Revision
		prior := api.Raw(jobs.Jobs)
		jobs.ForbidChanges = true
		repeated := f.command(t, method, id, &rev, interaction.ScheduleControlInput{Reason: "同态控制"})
		if !api.Equal(first.Output, repeated.Output) || !api.Equal(prior, api.Raw(jobs.Jobs)) {
			t.Fatalf("%s changed original facts/duties", method)
		}
		assertCurrentControlChecks(t, f, method, id, rev, interaction.ScheduleControlInput{Reason: "同态仍核当前身份"})
		jobs.ForbidChanges = false
	}
	surfaceID, presentationID := api.NewID("surface"), api.NewID("presentation")
	f.command(t, "surface.create", surfaceID, nil, interaction.SurfaceInput{BindingRef: f.binding, SnapshotRef: f.upload(t, `{"title":"界面"}`), RequestRefs: []api.ObjectRef{}})
	f.command(t, "presentation.open", presentationID, nil, interaction.OpenPresentationInput{EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), SurfaceRef: f.scope.Ref(surfaceID, 1)})
	rev = 1
	first = f.command(t, "presentation.close", presentationID, &rev, interaction.ClosePresentationInput{Reason: "关闭"})
	var presentation interaction.Presentation
	if e := api.Decode(first.Output, &presentation); e != nil {
		t.Fatal(e)
	}
	rev = presentation.Revision
	jobs.ForbidChanges = true
	repeated = f.command(t, "presentation.close", presentationID, &rev, interaction.ClosePresentationInput{Reason: "重复关闭"})
	if !api.Equal(first.Output, repeated.Output) {
		t.Fatalf("repeat close changed generation: %s -> %s", first.Output, repeated.Output)
	}
	assertCurrentControlChecks(t, f, "presentation.close", presentationID, rev, interaction.ClosePresentationInput{Reason: "当前身份"})
	jobs.ForbidChanges = false
	rev = 1
	first = f.command(t, "surface.close", surfaceID, &rev, interaction.SurfaceControlInput{Reason: "关闭"})
	var surface interaction.Surface
	if e := api.Decode(first.Output, &surface); e != nil {
		t.Fatal(e)
	}
	rev = surface.Revision
	jobs.ForbidChanges = true
	repeated = f.command(t, "surface.close", surfaceID, &rev, interaction.SurfaceControlInput{Reason: "重复关闭"})
	if !api.Equal(first.Output, repeated.Output) {
		t.Fatalf("repeat surface close changed facts: %s -> %s", first.Output, repeated.Output)
	}
	assertCurrentControlChecks(t, f, "surface.close", surfaceID, rev, interaction.SurfaceControlInput{Reason: "当前身份"})
}

func assertCurrentControlChecks(t *testing.T, f *applicationFixture, method, target string, current uint64, input any) {
	t.Helper()
	for _, staleIdentity := range []bool{false, true} {
		auth := f.auth
		revision := current - 1
		code := "revision_conflict"
		if staleIdentity {
			auth.SubjectID = api.NewID("subject")
			installCurrentSubject(t, f.store, f.scope, auth)
			revision = current
			code = "forbidden"
		}
		c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), Method: method, TargetID: target, ExpectedRevision: &revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(input)}
		receipt, e := f.d.Command(f.ctx, auth, api.Raw(c))
		if e != nil || receipt.Stage != "rejected" || !api.IsCode(receipt.Error, code) {
			t.Fatalf("%s same-state lost current CAS/authority: %+v %v", method, receipt, e)
		}
	}
}
