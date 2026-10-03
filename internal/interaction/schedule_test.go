package interaction_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/runtime"
)

// 宿主显式登记有限准确政策/安装锁。该门禁不代替后续 Task owner 的预算裁决。
type scheduleGateBridge struct{ policy, lock api.ComponentRef }

func (b scheduleGateBridge) CheckTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, p, lock api.ComponentRef, budget []api.Amount) error {
	if !api.Equal(p, b.policy) || !api.Equal(lock, b.lock) {
		return api.E("forbidden", "schedule_configuration_unregistered")
	}
	return api.ValidateAmounts(budget)
}

func TestScheduleKeepsUnknownSlotAcrossPauseAndClosesOnlyFromTask(t *testing.T) {
	f := newApplication(t)
	id := api.NewID("schedule")
	template := f.upload(t, "原定时目标")
	planned := time.Now().Add(15 * time.Second).Truncate(time.Second)
	r := f.command(t, "schedule.create", id, nil, interaction.ScheduleInput{Spec: interaction.ScheduleSpec{Type: "once_at", At: api.Time(planned)}, Timezone: "Etc/UTC", TZDBVersion: "2026b", TemplateRef: template, PolicyRef: f.policy, InstallLockRef: f.config, TaskTimeoutSeconds: 120, Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
	var out interaction.ScheduleOutput
	if e := api.Decode(r.Output, &out); e != nil {
		t.Fatal(e)
	}
	if out.NextDueAt == nil || *out.NextDueAt != api.Time(planned) {
		t.Fatalf("wrong frozen due %+v", out)
	}
	timer := time.NewTimer(time.Until(planned) + 20*time.Millisecond)
	defer timer.Stop()
	<-timer.C
	f.stepKind(t, interaction.JobTrigger)
	view, e := f.s.ReadSchedule(f.ctx, f.store, f.scope, f.auth, id)
	if e != nil {
		t.Fatal(e)
	}
	if view.ActiveOccurrenceID == "" || !view.Exhausted {
		t.Fatalf("occurrence not recorded %+v", view)
	}
	f.delivery.drop = true
	f.stepKind(t, interaction.JobOccurrence)
	occ, e := f.s.ReadOccurrence(f.ctx, f.store, f.scope, f.auth, view.ActiveOccurrenceID)
	if e != nil {
		t.Fatal(e)
	}
	if occ.Phase != "sending" || occ.Command == nil {
		t.Fatalf("unknown original send %+v", occ)
	}
	original := *occ.Command
	view, e = f.s.ReadSchedule(f.ctx, f.store, f.scope, f.auth, id)
	if e != nil {
		t.Fatal(e)
	}
	rev := view.Revision
	f.command(t, "schedule.pause", id, &rev, interaction.ScheduleControlInput{Reason: "停止未来触发"})
	f.stepKind(t, interaction.JobOccurrence)
	occ, e = f.s.ReadOccurrence(f.ctx, f.store, f.scope, f.auth, occ.OccurrenceID)
	if e != nil {
		t.Fatal(e)
	}
	if occ.Phase != "accepted" || occ.TaskRef == nil || occ.SlotClosed || !api.Equal(occ.Command, &original) {
		t.Fatalf("pause released unknown slot or changed command %+v", occ)
	}
	taskFact, e := f.task.Read(f.ctx, f.store, f.scope, f.auth, occ.TaskRef.ObjectID)
	if e != nil {
		t.Fatal(e)
	}
	if taskFact.Deadline != api.Time(planned.Add(120*time.Second)) {
		t.Fatalf("retry prolonged deadline %s", taskFact.Deadline)
	}
	if original.ExpiresAt != api.Time(planned.Add(60*time.Second)) {
		t.Fatalf("original admission changed %s", original.ExpiresAt)
	}
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), Method: "task.cancel", TargetID: taskFact.TaskID, ExpectedRevision: &taskFact.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(struct {
		TaskID string `json:"task_id"`
		Reason string `json:"reason"`
	}{taskFact.TaskID, "目标不再需要"})}
	if r, e = f.d.Command(f.ctx, f.auth, api.Raw(c)); e != nil || r.Stage != "applied" {
		t.Fatalf("task cancel %+v %v", r, e)
	}
	f.stepKind(t, interaction.JobOccurrence)
	occ, e = f.s.ReadOccurrence(f.ctx, f.store, f.scope, f.auth, occ.OccurrenceID)
	if e != nil {
		t.Fatal(e)
	}
	view, e = f.s.ReadSchedule(f.ctx, f.store, f.scope, f.auth, id)
	if e != nil {
		t.Fatal(e)
	}
	if !occ.SlotClosed || occ.ClosureRef == nil || view.ActiveOccurrenceID != "" || view.State != "paused" {
		t.Fatalf("missing original closure occ=%+v schedule=%+v", occ, view)
	}
}
func (f *applicationFixture) stepKind(t *testing.T, kind string) {
	t.Helper()
	works, status, e := f.store.Claim(f.ctx, f.scope, api.NewID("boot"), []string{kind}, 1, 30*time.Second)
	if e != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("claim %s %+v %s %v", kind, works, status, e)
	}
	h, ok := f.registry.Job(kind)
	if !ok {
		t.Fatalf("job %s missing", kind)
	}
	if e = h(f.ctx, f.store, f.scope, works[0]); e != nil {
		t.Fatal(e)
	}
}
