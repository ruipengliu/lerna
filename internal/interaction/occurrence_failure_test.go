package interaction_test

import (
	"context"
	"errors"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/runtime"
	"testing"
	"time"
)

type failingScheduleGate struct {
	interaction.ScheduleGate
	err error
}

func (g *failingScheduleGate) CheckTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, p, l api.ComponentRef, b []api.Amount) error {
	if g.err != nil {
		return g.err
	}
	return g.ScheduleGate.CheckTx(ctx, tx, a, p, l, b)
}

type failingOccurrenceContent struct {
	interaction.ContentPort
	err error
}

func (c *failingOccurrenceContent) CheckTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, r api.ContentRef, p string) error {
	if c.err != nil {
		return c.err
	}
	return c.ContentPort.CheckTx(ctx, tx, a, r, p)
}

func TestOccurrencePreservesOriginalSlotOnUnclassifiedGateFailures(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "proven_refusal", true: "original_recovery"}[recovery], func(t *testing.T) { testOccurrenceFailure(t, recovery) })
	}
}
func testOccurrenceFailure(t *testing.T, recovery bool) {
	f := newApplication(t)
	gate := &failingScheduleGate{ScheduleGate: f.appPorts.ScheduleGate}
	content := &failingOccurrenceContent{ContentPort: f.appPorts.Content}
	ports := f.appPorts
	ports.ScheduleGate = gate
	ports.Content = content
	s, e := interaction.New(f.appConfig, ports)
	if e != nil {
		t.Fatal(e)
	}
	id := api.NewID("schedule")
	template := f.upload(t, "原定时目标")
	planned := time.Now().Add(2 * time.Second).Truncate(time.Second)
	created := f.command(t, "schedule.create", id, nil, interaction.ScheduleInput{Spec: interaction.ScheduleSpec{Type: "interval", AnchorAt: api.Time(planned), EverySeconds: 2}, Timezone: "Etc/UTC", TZDBVersion: "2026b", TemplateRef: template, PolicyRef: f.policy, InstallLockRef: f.config, TaskTimeoutSeconds: 120, Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
	var admitted interaction.ScheduleOutput
	if e := api.Decode(created.Output, &admitted); e != nil || admitted.NextDueAt == nil {
		t.Fatalf("actual admitted due %+v %v", admitted, e)
	}
	due, e := api.ParseTime(*admitted.NextDueAt)
	if e != nil {
		t.Fatal(e)
	}
	timer := time.NewTimer(time.Until(due) + 20*time.Millisecond)
	defer timer.Stop()
	<-timer.C
	f.stepKind(t, interaction.JobTrigger)
	original, e := s.ReadSchedule(f.ctx, f.store, f.scope, f.auth, id)
	if e != nil || original.ActiveOccurrenceID == "" {
		t.Fatalf("original slot %+v %v", original, e)
	}
	occ, e := s.ReadOccurrence(f.ctx, f.store, f.scope, f.auth, original.ActiveOccurrenceID)
	if e != nil {
		t.Fatal(e)
	}
	works, status, e := f.store.Claim(f.ctx, f.scope, api.NewID("boot"), []string{interaction.JobOccurrence}, 1, 30*time.Second)
	if e != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("claim %+v %s %v", works, status, e)
	}
	for _, port := range []string{"schedule", "content"} {
		for _, failure := range []error{errors.New("repository unavailable"), errors.New("corrupt repository record"), api.E("overloaded", "bounded_authority_queue"), runtime.ErrNotFound, api.E("forbidden", "identity_authority_unavailable"), api.E("expired", "foreign_proof_expired"), errors.Join(api.E("forbidden", "source_closed"), errors.New("repository unavailable"))} {
			gate.err = nil
			content.err = nil
			if port == "schedule" {
				gate.err = failure
			} else {
				content.err = failure
			}
			if e = s.Occur(f.ctx, f.store, f.scope, works[0]); !errors.Is(e, failure) {
				t.Fatalf("%s lost original error: %v", port, e)
			}
			current, e := s.ReadOccurrence(f.ctx, f.store, f.scope, f.auth, occ.OccurrenceID)
			if e != nil || !api.Equal(current, occ) {
				t.Fatalf("failure changed original occurrence: %+v %v", current, e)
			}
			schedule, e := s.ReadSchedule(f.ctx, f.store, f.scope, f.auth, id)
			if e != nil || !api.Equal(schedule, original) {
				t.Fatalf("failure released original slot: %+v %v", schedule, e)
			}
		}
	}
	if recovery {
		gate.err = nil
		content.err = nil
		f.delivery.drop = true
		if e = s.Occur(f.ctx, f.store, f.scope, works[0]); e != nil {
			t.Fatal(e)
		}
		sending, e := s.ReadOccurrence(f.ctx, f.store, f.scope, f.auth, occ.OccurrenceID)
		if e != nil || sending.Phase != "sending" || sending.Command == nil || sending.SlotClosed || sending.TaskDeadline != occ.TaskDeadline || sending.AcceptBefore != occ.AcceptBefore {
			t.Fatalf("recovery replaced original occurrence/deadlines: %+v %v", sending, e)
		}
		command := *sending.Command
		f.delivery.drop = false
		works, status, e = f.store.Claim(f.ctx, f.scope, api.NewID("boot"), []string{interaction.JobOccurrence}, 1, 30*time.Second)
		if e != nil || status != runtime.Committed || len(works) != 1 {
			t.Fatalf("original retry claim %+v %s %v", works, status, e)
		}
		if e = s.Occur(f.ctx, f.store, f.scope, works[0]); e != nil {
			t.Fatal(e)
		}
		accepted, e := s.ReadOccurrence(f.ctx, f.store, f.scope, f.auth, occ.OccurrenceID)
		if e != nil || accepted.Phase != "accepted" || accepted.TaskRef == nil || accepted.TaskRef.ObjectID != command.TargetID || !api.Equal(accepted.Command, &command) || accepted.SlotClosed || f.delivery.sends != 1 {
			t.Fatalf("recovery changed original command/task/slot: %+v sends%d %v", accepted, f.delivery.sends, e)
		}
		return
	}
	gate.err = api.E("forbidden", "schedule_configuration_unregistered")
	content.err = nil
	if e = s.Occur(f.ctx, f.store, f.scope, works[0]); e != nil {
		t.Fatal(e)
	}
	skipped, e := s.ReadOccurrence(f.ctx, f.store, f.scope, f.auth, occ.OccurrenceID)
	if e != nil || skipped.Phase != "skipped" || !skipped.SlotClosed || skipped.Command != nil {
		t.Fatalf("proven refusal not closed %+v %v", skipped, e)
	}
}
