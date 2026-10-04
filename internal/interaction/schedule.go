package interaction

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type ScheduleInput struct {
	Spec               ScheduleSpec     `json:"spec"`
	Timezone           string           `json:"timezone"`
	TZDBVersion        string           `json:"tzdb_version"`
	TemplateRef        api.ContentRef   `json:"template_ref"`
	PolicyRef          api.ComponentRef `json:"policy_ref"`
	InstallLockRef     api.ComponentRef `json:"install_lock_ref"`
	TaskTimeoutSeconds uint64           `json:"task_timeout_seconds"`
	Budget             []api.Amount     `json:"budget"`
}
type Schedule struct {
	ScheduleInput
	ScheduleID         string       `json:"schedule_id"`
	TenantID           string       `json:"tenant_id"`
	OwnerID            string       `json:"owner_id"`
	Revision           uint64       `json:"revision"`
	RuleRevision       uint64       `json:"rule_revision"`
	State              string       `json:"state"`
	CreatedAt          string       `json:"created_at"`
	EffectiveAfter     string       `json:"effective_after"`
	NextDueAt          *string      `json:"next_due_at,omitempty"`
	Exhausted          bool         `json:"exhausted"`
	ActiveOccurrenceID string       `json:"active_occurrence_id,omitempty"`
	ResumeAfter        *string      `json:"resume_after,omitempty"`
	PausedRange        *PausedRange `json:"paused_range,omitempty"`
}

// PausedRange 保留尚未审计完的暂停时点，当前 NextDueAt 始终指向恢复后的未来。
// 原规则单独冻结，编辑不能把这些时点解释为新规则。
type PausedRange struct {
	ScheduleRef   api.ObjectRef `json:"schedule_ref"`
	RuleRevision  uint64        `json:"rule_revision"`
	NextPlannedAt string        `json:"next_planned_at"`
	Through       string        `json:"through"`
	Spec          ScheduleSpec  `json:"spec"`
	Timezone      string        `json:"timezone"`
	TZDBVersion   string        `json:"tzdb_version"`
}
type scheduleRecord struct {
	Schedule
	Auth runtime.Auth `json:"auth"`
}
type ScheduleOutput struct {
	ScheduleRef         api.ObjectRef         `json:"schedule_ref"`
	RuleRevision        uint64                `json:"rule_revision"`
	State               string                `json:"state"`
	NextDueAt           *string               `json:"next_due_at,omitempty"`
	Exhausted           bool                  `json:"exhausted"`
	AffectedOccurrences api.CollectionSummary `json:"affected_occurrences"`
}
type ScheduleControlInput struct {
	Reason string `json:"reason"`
}
type Occurrence struct {
	OccurrenceID   string          `json:"occurrence_id"`
	Revision       uint64          `json:"revision"`
	ScheduleRef    api.ObjectRef   `json:"schedule_ref"`
	RuleRevision   uint64          `json:"rule_revision"`
	PlannedAt      string          `json:"planned_at"`
	TaskDeadline   string          `json:"task_deadline"`
	AcceptBefore   string          `json:"accept_before"`
	Phase          string          `json:"phase"`
	Frozen         ScheduleInput   `json:"frozen"`
	Command        *api.Command    `json:"command,omitempty"`
	Receipt        *api.Receipt    `json:"receipt,omitempty"`
	TaskRef        *api.ObjectRef  `json:"task_ref,omitempty"`
	SlotClosed     bool            `json:"slot_closed"`
	ClosureRef     *api.ContentRef `json:"closure_ref,omitempty"`
	ClosureTaskRef *api.ObjectRef  `json:"closure_task_ref,omitempty"`
	SkipReason     string          `json:"skip_reason,omitempty"`
}
type occurrenceRecord struct {
	Occurrence
	Auth runtime.Auth `json:"auth"`
}
type SkipRange struct {
	SkipID         string        `json:"skip_id"`
	ScheduleRef    api.ObjectRef `json:"schedule_ref"`
	RuleRevision   uint64        `json:"rule_revision"`
	FirstPlannedAt string        `json:"first_planned_at"`
	LastPlannedAt  string        `json:"last_planned_at"`
	Count          uint64        `json:"count"`
	Reason         string        `json:"reason"`
}

func (s *Service) validateSchedule(ctx context.Context, tx runtime.Tx, a runtime.Auth, in ScheduleInput) error {
	if s.ports.ScheduleGate == nil || s.ports.Calendar == nil || s.ports.Delivery == nil || s.ports.Closure == nil || !api.ValidID(s.config.DiscoveryOwnerID) {
		return api.E("unsupported", "schedule_unconfigured")
	}
	if err := validateSpec(in.Spec); err != nil {
		return err
	}
	if in.TaskTimeoutSeconds == 0 || in.TaskTimeoutSeconds > 30*24*3600 {
		return invalid("invalid_task_timeout")
	}
	if _, err := s.ports.Calendar.Load(in.Timezone, in.TZDBVersion); err != nil {
		return err
	}
	if err := api.ValidateRecord("ComponentRef", in.PolicyRef); err != nil {
		return err
	}
	if err := api.ValidateRecord("ComponentRef", in.InstallLockRef); err != nil {
		return err
	}
	if err := api.ValidateAmounts(in.Budget); err != nil {
		return err
	}
	if len(in.Budget) == 0 || len(in.Budget) > 20 {
		return invalid("budget_required")
	}
	if err := s.ports.ScheduleGate.CheckTx(ctx, tx, a, in.PolicyRef, in.InstallLockRef, in.Budget); err != nil {
		return err
	}
	return s.content(ctx, tx, a, in.TemplateRef, "task.goal")
}
func (s *Service) CreateScheduleTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ScheduleInput) (ScheduleOutput, error) {
	if err := s.validateSchedule(ctx, tx, a, in); err != nil {
		return ScheduleOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return ScheduleOutput{}, err
	}
	next, err := s.NextDue(in.Spec, in.Timezone, in.TZDBVersion, now)
	if err != nil {
		return ScheduleOutput{}, err
	}
	if in.Spec.Type == "once_at" && next == nil {
		return ScheduleOutput{}, api.E("expired", "schedule_time_elapsed")
	}
	r := scheduleRecord{Schedule: Schedule{ScheduleInput: in, ScheduleID: c.TargetID, TenantID: tx.Scope().TenantID, OwnerID: tx.Scope().OwnerID, Revision: 1, RuleRevision: 1, State: "enabled", CreatedAt: api.Time(now), EffectiveAfter: api.Time(now), Exhausted: next == nil}, Auth: a}
	if next != nil {
		value := api.Time(*next)
		r.NextDueAt = &value
	}
	if err = tx.Create(ctx, schedules, c.TargetID, a.SubjectID, r); err != nil {
		return ScheduleOutput{}, err
	}
	if err = bumpCollection(ctx, tx, "schedules", a.SubjectID); err != nil {
		return ScheduleOutput{}, err
	}
	if next != nil {
		if _, err = tx.Raise(ctx, JobTrigger, c.TargetID, tx.Scope().Ref(c.TargetID, 1), *next); err != nil {
			return ScheduleOutput{}, err
		}
	}
	return scheduleOutput(tx.Scope(), r), nil
}
func getSchedule(ctx context.Context, tx runtime.Tx, a runtime.Auth, id string) (scheduleRecord, error) {
	var r scheduleRecord
	_, err := tx.Get(ctx, schedules, id, &r)
	if err == nil {
		err = access(a, tx.Scope(), r.Auth.SubjectID)
	}
	return r, err
}
func saveSchedule(ctx context.Context, tx runtime.Tx, r *scheduleRecord) error {
	old := r.Revision
	r.Revision++
	if err := tx.Put(ctx, schedules, r.ScheduleID, old, *r); err != nil {
		return err
	}
	return bumpCollection(ctx, tx, "schedules", r.Auth.SubjectID)
}
func scheduleOutput(scope runtime.Scope, r scheduleRecord) ScheduleOutput {
	count := uint64(0)
	if r.ActiveOccurrenceID != "" {
		count = 1
	}
	return ScheduleOutput{ScheduleRef: scope.Ref(r.ScheduleID, r.Revision), RuleRevision: r.RuleRevision, State: r.State, NextDueAt: r.NextDueAt, Exhausted: r.Exhausted, AffectedOccurrences: api.CollectionSummary{CollectionRevision: r.Revision, TotalCount: count, UnresolvedCount: count, Complete: true}}
}
func (s *Service) UpdateScheduleTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ScheduleInput) (ScheduleOutput, error) {
	r, err := getSchedule(ctx, tx, a, c.TargetID)
	if err != nil {
		return ScheduleOutput{}, err
	}
	if c.ExpectedRevision == nil || *c.ExpectedRevision != r.Revision {
		return ScheduleOutput{}, api.E("revision_conflict", "rule_changed")
	}
	if r.State == "deleted" {
		return ScheduleOutput{}, api.E("gone", "schedule_deleted")
	}
	if err = s.validateSchedule(ctx, tx, a, in); err != nil {
		return ScheduleOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return ScheduleOutput{}, err
	}
	next, err := s.NextDue(in.Spec, in.Timezone, in.TZDBVersion, now)
	if err != nil {
		return ScheduleOutput{}, err
	}
	if in.Spec.Type == "once_at" && next == nil {
		return ScheduleOutput{}, api.E("expired", "schedule_time_elapsed")
	}
	r.ScheduleInput = in
	r.RuleRevision++
	r.EffectiveAfter = api.Time(now)
	r.NextDueAt = nil
	r.ResumeAfter = nil
	r.Exhausted = next == nil
	if next != nil {
		value := api.Time(*next)
		r.NextDueAt = &value
	}
	if err = saveSchedule(ctx, tx, &r); err != nil {
		return ScheduleOutput{}, err
	}
	if next != nil && r.State == "enabled" {
		if _, err = tx.Raise(ctx, JobTrigger, c.TargetID, tx.Scope().Ref(c.TargetID, r.Revision), *next); err != nil {
			return ScheduleOutput{}, err
		}
	}
	return scheduleOutput(tx.Scope(), r), nil
}
func (s *Service) ControlScheduleTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ScheduleControlInput) (ScheduleOutput, error) {
	r, err := getSchedule(ctx, tx, a, c.TargetID)
	if err != nil {
		return ScheduleOutput{}, err
	}
	if c.ExpectedRevision == nil || *c.ExpectedRevision != r.Revision {
		return ScheduleOutput{}, api.E("revision_conflict", "rule_changed")
	}
	if r.State == "deleted" && c.Method != "schedule.delete" {
		return ScheduleOutput{}, api.E("gone", "schedule_deleted")
	}
	if c.Method == "schedule.pause" && r.State == "paused" || c.Method == "schedule.resume" && r.State == "enabled" || c.Method == "schedule.delete" && r.State == "deleted" {
		return scheduleOutput(tx.Scope(), r), nil
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return ScheduleOutput{}, err
	}
	switch c.Method {
	case "schedule.pause":
		r.State = "paused"
	case "schedule.delete":
		r.State = "deleted"
	case "schedule.resume":
		if r.State != "paused" {
			return ScheduleOutput{}, api.E("invalid_state", "schedule_not_paused")
		}
		if r.PausedRange != nil {
			return ScheduleOutput{}, api.E("overloaded", "paused_skip_pending")
		}
		r.State = "enabled"
		r.ResumeAfter = nil
		if r.NextDueAt != nil {
			due, e := api.ParseTime(*r.NextDueAt)
			if e != nil {
				return ScheduleOutput{}, e
			}
			if !due.After(now) {
				r.PausedRange = &PausedRange{ScheduleRef: tx.Scope().Ref(r.ScheduleID, r.Revision), RuleRevision: r.RuleRevision, NextPlannedAt: *r.NextDueAt, Through: api.Time(now), Spec: r.Spec, Timezone: r.Timezone, TZDBVersion: r.TZDBVersion}
				if e = advanceSchedule(s, &r, now); e != nil {
					return ScheduleOutput{}, e
				}
				if _, e = s.advancePaused(ctx, tx, &r, time.Now()); e != nil {
					return ScheduleOutput{}, e
				}
			}
		}
	default:
		return ScheduleOutput{}, invalid("unknown_schedule_control")
	}
	if err = saveSchedule(ctx, tx, &r); err != nil {
		return ScheduleOutput{}, err
	}
	if r.NextDueAt != nil || r.PausedRange != nil {
		due := now
		if r.NextDueAt != nil {
			due, _ = api.ParseTime(*r.NextDueAt)
		}
		if r.PausedRange != nil {
			due = now
		}
		if _, err = tx.Raise(ctx, JobTrigger, r.ScheduleID, tx.Scope().Ref(r.ScheduleID, r.Revision), due); err != nil {
			return ScheduleOutput{}, err
		}
	}
	if r.ActiveOccurrenceID != "" {
		var occ occurrenceRecord
		if _, err = tx.Get(ctx, occurrences, r.ActiveOccurrenceID, &occ); err != nil {
			return ScheduleOutput{}, err
		}
		// 控制是新的领域事实。原 source revision 的 Raise 只判重，不会唤醒等待作业。
		if err = saveOccurrence(ctx, tx, &occ); err != nil {
			return ScheduleOutput{}, err
		}
		if _, err = tx.Raise(ctx, JobOccurrence, occ.OccurrenceID, tx.Scope().Ref(occ.OccurrenceID, occ.Revision), now); err != nil {
			return ScheduleOutput{}, err
		}
	}
	return scheduleOutput(tx.Scope(), r), nil
}
func (s *Service) ReadSchedule(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, id string) (Schedule, error) {
	return currentDisclosure(ctx, s, store, scope, a, func(tx runtime.Tx) (Schedule, error) {
		var r scheduleRecord
		if _, err := tx.Get(ctx, schedules, id, &r); err != nil {
			return Schedule{}, err
		}
		if err := access(a, scope, r.Auth.SubjectID); err != nil {
			return Schedule{}, err
		}
		return r.Schedule, nil
	})
}
func (s *Service) ReadOccurrence(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, id string) (Occurrence, error) {
	return currentDisclosure(ctx, s, store, scope, a, func(tx runtime.Tx) (Occurrence, error) {
		var r occurrenceRecord
		if _, err := tx.Get(ctx, occurrences, id, &r); err != nil {
			return Occurrence{}, err
		}
		if err := access(a, scope, r.Auth.SubjectID); err != nil {
			return Occurrence{}, api.E("forbidden", "occurrence_redacted")
		}
		return r.Occurrence, nil
	})
}
func (s *Service) workTx(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, fn func(runtime.Tx) (runtime.Disposition, error)) error {
	status, err := store.Within(ctx, scope, s.config.Participants, func(tx runtime.Tx) error {
		disposition, e := fn(tx)
		if e != nil {
			return e
		}
		if e = tx.Guard(ctx, work.Claim); e != nil {
			return e
		}
		return tx.Finish(ctx, work.Claim, disposition)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}
func (s *Service) Trigger(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return s.workTx(ctx, store, scope, work, func(tx runtime.Tx) (runtime.Disposition, error) {
		var r scheduleRecord
		if _, err := tx.Get(ctx, schedules, work.Job.SourceRef.ObjectID, &r); err != nil {
			return runtime.Disposition{}, err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return runtime.Disposition{}, err
		}
		start := time.Now()
		count, err := s.advancePaused(ctx, tx, &r, start)
		if err != nil {
			return runtime.Disposition{}, err
		}
		if r.State != "enabled" || r.NextDueAt == nil {
			if count > 0 {
				if err = saveSchedule(ctx, tx, &r); err != nil {
					return runtime.Disposition{}, err
				}
			}
			if r.PausedRange != nil {
				return runtime.Ready(now), nil
			}
			return runtime.Done(), nil
		}
		due, err := api.ParseTime(*r.NextDueAt)
		if err != nil {
			return runtime.Disposition{}, err
		}
		var pauseCutoff time.Time
		if r.ResumeAfter != nil {
			pauseCutoff, _ = api.ParseTime(*r.ResumeAfter)
		}
		var skipped *SkipRange
		for r.NextDueAt != nil && count < 100 && time.Since(start) < 10*time.Millisecond {
			due, _ = api.ParseTime(*r.NextDueAt)
			reason := ""
			if !pauseCutoff.IsZero() && !due.After(pauseCutoff) {
				reason = "paused"
			} else if !now.Before(due.Add(60*time.Second)) || !now.Before(due.Add(time.Duration(r.TaskTimeoutSeconds)*time.Second)) {
				reason = "missed"
			}
			if reason == "" {
				break
			}
			if skipped != nil && skipped.Reason != reason {
				break
			}
			if skipped == nil {
				skipped = &SkipRange{SkipID: s.config.Identity.NewID("skip"), ScheduleRef: scope.Ref(r.ScheduleID, r.Revision), RuleRevision: r.RuleRevision, FirstPlannedAt: api.Time(due), Reason: reason}
			}
			skipped.Count++
			skipped.LastPlannedAt = api.Time(due)
			count++
			if err = advanceSchedule(s, &r, due); err != nil {
				return runtime.Disposition{}, err
			}
		}
		if skipped != nil {
			if err = tx.Create(ctx, skips, skipped.SkipID, r.ScheduleID, *skipped); err != nil {
				return runtime.Disposition{}, err
			}
			if err = bumpCollection(ctx, tx, "skips", r.ScheduleID); err != nil {
				return runtime.Disposition{}, err
			}
		}
		if r.NextDueAt != nil {
			due, _ = api.ParseTime(*r.NextDueAt)
			if !pauseCutoff.IsZero() && due.After(pauseCutoff) {
				r.ResumeAfter = nil
			}
			if !due.After(now) && count < 100 && time.Since(start) < 10*time.Millisecond && r.ResumeAfter == nil {
				id := s.config.Identity.NewID("occurrence")
				deadline := due.Add(time.Duration(r.TaskTimeoutSeconds) * time.Second)
				accept := due.Add(60 * time.Second)
				if deadline.Before(accept) {
					accept = deadline
				}
				o := occurrenceRecord{Occurrence: Occurrence{OccurrenceID: id, Revision: 1, ScheduleRef: scope.Ref(r.ScheduleID, r.Revision), RuleRevision: r.RuleRevision, PlannedAt: api.Time(due), TaskDeadline: api.Time(deadline), AcceptBefore: api.Time(accept), Phase: "recorded", Frozen: r.ScheduleInput}, Auth: r.Auth}
				if r.ActiveOccurrenceID != "" {
					o.Phase = "skipped"
					o.SkipReason = "overlap"
					o.SlotClosed = true
				} else {
					r.ActiveOccurrenceID = id
				}
				if err = tx.Create(ctx, occurrences, id, r.ScheduleID, o); err != nil {
					return runtime.Disposition{}, err
				}
				if err = bumpCollection(ctx, tx, "occurrences", r.ScheduleID); err != nil {
					return runtime.Disposition{}, err
				}
				if o.Phase == "recorded" {
					if _, err = tx.Raise(ctx, JobOccurrence, id, scope.Ref(id, 1), now); err != nil {
						return runtime.Disposition{}, err
					}
				}
				if err = advanceSchedule(s, &r, due); err != nil {
					return runtime.Disposition{}, err
				}
			}
		}
		if r.NextDueAt == nil {
			r.ResumeAfter = nil
		}
		if err = saveSchedule(ctx, tx, &r); err != nil {
			return runtime.Disposition{}, err
		}
		if r.PausedRange != nil {
			return runtime.Ready(now), nil
		}
		if r.NextDueAt == nil {
			return runtime.Done(), nil
		}
		next, _ := api.ParseTime(*r.NextDueAt)
		if !next.After(now) {
			return runtime.Ready(now), nil
		}
		return runtime.Waiting(next), nil
	})
}

func (s *Service) advancePaused(ctx context.Context, tx runtime.Tx, r *scheduleRecord, start time.Time) (int, error) {
	pending := r.PausedRange
	if pending == nil {
		return 0, nil
	}
	through, err := api.ParseTime(pending.Through)
	if err != nil {
		return 0, err
	}
	var skipped *SkipRange
	count := 0
	for pending != nil && count < 100 && time.Since(start) < 10*time.Millisecond {
		due, e := api.ParseTime(pending.NextPlannedAt)
		if e != nil {
			return 0, e
		}
		if due.After(through) {
			r.PausedRange = nil
			break
		}
		if skipped == nil {
			skipped = &SkipRange{SkipID: s.config.Identity.NewID("skip"), ScheduleRef: pending.ScheduleRef, RuleRevision: pending.RuleRevision, FirstPlannedAt: api.Time(due), Reason: "paused"}
		}
		skipped.Count++
		skipped.LastPlannedAt = api.Time(due)
		count++
		next, e := s.NextDue(pending.Spec, pending.Timezone, pending.TZDBVersion, due)
		if e != nil {
			return 0, e
		}
		if next == nil || next.After(through) {
			r.PausedRange = nil
			pending = nil
		} else {
			pending.NextPlannedAt = api.Time(*next)
		}
	}
	if skipped != nil {
		if err = tx.Create(ctx, skips, skipped.SkipID, r.ScheduleID, *skipped); err != nil {
			return 0, err
		}
		if err = bumpCollection(ctx, tx, "skips", r.ScheduleID); err != nil {
			return 0, err
		}
	}
	return count, nil
}
func advanceSchedule(s *Service, r *scheduleRecord, after time.Time) error {
	next, err := s.NextDue(r.Spec, r.Timezone, r.TZDBVersion, after)
	if err != nil {
		return err
	}
	r.NextDueAt = nil
	r.Exhausted = next == nil
	if next != nil {
		value := api.Time(*next)
		r.NextDueAt = &value
	}
	return nil
}
func saveOccurrence(ctx context.Context, tx runtime.Tx, r *occurrenceRecord) error {
	old := r.Revision
	r.Revision++
	if err := tx.Put(ctx, occurrences, r.OccurrenceID, old, *r); err != nil {
		return err
	}
	return bumpCollection(ctx, tx, "occurrences", r.ScheduleRef.ObjectID)
}
func closeSlot(ctx context.Context, tx runtime.Tx, schedule *scheduleRecord, occ *occurrenceRecord) error {
	occ.SlotClosed = true
	if schedule.ActiveOccurrenceID == occ.OccurrenceID {
		schedule.ActiveOccurrenceID = ""
		return saveSchedule(ctx, tx, schedule)
	}
	return nil
}
