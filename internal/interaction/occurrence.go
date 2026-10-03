package interaction

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Service) Occur(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var snapshot occurrenceRecord
	if _, err := store.Read(ctx, scope, occurrences, work.Job.SourceRef.ObjectID, 0, &snapshot); err != nil {
		return err
	}
	if snapshot.SlotClosed || snapshot.Phase == "skipped" || snapshot.Phase == "rejected" {
		return runtime.Finish(ctx, store, scope, s.config.Participants, work, runtime.Done(), nil)
	}
	if err := store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	if snapshot.Phase == "accepted" {
		if snapshot.TaskRef == nil {
			return api.E("invalid_state", "accepted_task_mapping_missing")
		}
		closure, err := s.ports.Closure.Closure(ctx, scope, snapshot.Auth, *snapshot.TaskRef)
		if err != nil {
			return s.waitOccurrence(ctx, store, scope, work)
		}
		if closure.TaskRef.TenantID != scope.TenantID || closure.TaskRef.OwnerID != snapshot.TaskRef.OwnerID || closure.TaskRef.ObjectID != snapshot.TaskRef.ObjectID {
			return api.E("forbidden", "closure_target_mismatch")
		}
		if !closure.GoalWorkClosed || !closure.EffectsClosed {
			return s.waitOccurrence(ctx, store, scope, work)
		}
		if err = api.ValidateRecord("ContentRef", closure.ClosureRef); err != nil {
			return err
		}
		return s.workTx(ctx, store, scope, work, func(tx runtime.Tx) (runtime.Disposition, error) {
			var schedule scheduleRecord
			if _, e := tx.Get(ctx, schedules, snapshot.ScheduleRef.ObjectID, &schedule); e != nil {
				return runtime.Disposition{}, e
			}
			var occ occurrenceRecord
			if _, e := tx.Get(ctx, occurrences, snapshot.OccurrenceID, &occ); e != nil {
				return runtime.Disposition{}, e
			}
			if occ.Phase != "accepted" || occ.SlotClosed {
				return runtime.Done(), nil
			}
			occ.ClosureRef = &closure.ClosureRef
			occ.ClosureTaskRef = &closure.TaskRef
			if e := closeSlot(ctx, tx, &schedule, &occ); e != nil {
				return runtime.Disposition{}, e
			}
			if e := saveOccurrence(ctx, tx, &occ); e != nil {
				return runtime.Disposition{}, e
			}
			return runtime.Done(), nil
		})
	}
	var prepared occurrenceRecord
	var now time.Time
	ready := false
	status, err := store.Within(ctx, scope, s.config.Participants, func(tx runtime.Tx) error {
		var schedule scheduleRecord
		if _, e := tx.Get(ctx, schedules, snapshot.ScheduleRef.ObjectID, &schedule); e != nil {
			return e
		}
		var occ occurrenceRecord
		if _, e := tx.Get(ctx, occurrences, snapshot.OccurrenceID, &occ); e != nil {
			return e
		}
		var e error
		now, e = tx.Now(ctx)
		if e != nil {
			return e
		}
		if occ.Phase != "recorded" && occ.Phase != "sending" {
			prepared = occ
			return tx.Guard(ctx, work.Claim)
		}
		if occ.Phase == "recorded" {
			expiry, _ := api.ParseTime(occ.AcceptBefore)
			reason := ""
			if schedule.State != "enabled" {
				reason = "paused_or_deleted"
			} else if !now.Before(expiry) {
				reason = "missed"
			} else if schedule.ActiveOccurrenceID != occ.OccurrenceID {
				return api.E("invalid_state", "active_slot_unknown")
			}
			if reason == "" {
				if e = s.ports.ScheduleGate.CheckTx(ctx, tx, occ.Auth, occ.Frozen.PolicyRef, occ.Frozen.InstallLockRef, occ.Frozen.Budget); e != nil {
					if api.IsCode(e, "dependency_unavailable") || api.IsCode(e, "overloaded") {
						return e
					}
					reason = "permission_or_budget_blocked"
				}
			}
			if reason == "" {
				if e = s.content(ctx, tx, occ.Auth, occ.Frozen.TemplateRef, "task.goal"); e != nil {
					if api.IsCode(e, "dependency_unavailable") {
						return e
					}
					reason = "input_unpublished"
				}
			}
			if reason != "" {
				occ.Phase = "skipped"
				occ.SkipReason = reason
				if e = closeSlot(ctx, tx, &schedule, &occ); e != nil {
					return e
				}
				if e = saveOccurrence(ctx, tx, &occ); e != nil {
					return e
				}
				prepared = occ
				return tx.Guard(ctx, work.Claim)
			}
			taskID := api.NewID("task")
			occ.Command = &api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: s.config.DiscoveryOwnerID, CommandID: api.NewID("command"), Method: "task.submit", TargetID: taskID, ExpiresAt: occ.AcceptBefore, Payload: api.Raw(taskSubmit{OrchestratorID: s.config.DiscoveryOwnerID, GoalRef: occ.Frozen.TemplateRef, PolicyRef: occ.Frozen.PolicyRef, Deadline: occ.TaskDeadline, Budget: occ.Frozen.Budget})}
			occ.Phase = "sending"
			if e = saveOccurrence(ctx, tx, &occ); e != nil {
				return e
			}
		}
		prepared = occ
		ready = occ.Command != nil
		return tx.Guard(ctx, work.Claim)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	if err != nil {
		return err
	}
	if !ready {
		return runtime.Finish(ctx, store, scope, s.config.Participants, work, runtime.Done(), nil)
	}
	if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	receipt, err := s.ports.Delivery.Lookup(ctx, scope, prepared.Auth, prepared.Command.LogicalServiceID, prepared.Command.CommandID)
	if api.IsCode(err, "not_found") {
		if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
			return err
		}
		receipt, err = s.ports.Delivery.Send(ctx, scope, prepared.Auth, *prepared.Command)
	}
	if err != nil {
		return runtime.Finish(ctx, store, scope, s.config.Participants, work, runtime.Ready(now), nil)
	}
	if err = checkReceipt(*prepared.Command, receipt); err != nil {
		return err
	}
	if receipt.Stage == "accepted" {
		return s.waitOccurrence(ctx, store, scope, work)
	}
	return s.workTx(ctx, store, scope, work, func(tx runtime.Tx) (runtime.Disposition, error) {
		var schedule scheduleRecord
		if _, e := tx.Get(ctx, schedules, prepared.ScheduleRef.ObjectID, &schedule); e != nil {
			return runtime.Disposition{}, e
		}
		var occ occurrenceRecord
		if _, e := tx.Get(ctx, occurrences, prepared.OccurrenceID, &occ); e != nil {
			return runtime.Disposition{}, e
		}
		if occ.Phase != "sending" {
			return runtime.Done(), nil
		}
		occ.Receipt = &receipt
		disposition := runtime.Done()
		if receipt.Stage == "rejected" {
			occ.Phase = "rejected"
			if e := closeSlot(ctx, tx, &schedule, &occ); e != nil {
				return runtime.Disposition{}, e
			}
		} else {
			var out struct {
				TaskRef api.ObjectRef `json:"task_ref"`
			}
			if e := json.Unmarshal(receipt.Output, &out); e != nil {
				return runtime.Disposition{}, e
			}
			if out.TaskRef.TenantID != scope.TenantID || out.TaskRef.OwnerID != occ.Command.LogicalServiceID || out.TaskRef.ObjectID != occ.Command.TargetID || out.TaskRef.Revision == 0 {
				return runtime.Disposition{}, api.E("forbidden", "receipt_task_mapping_mismatch")
			}
			occ.Phase = "accepted"
			occ.TaskRef = &out.TaskRef
			now, e := tx.Now(ctx)
			if e != nil {
				return runtime.Disposition{}, e
			}
			disposition = runtime.Ready(now)
		}
		if e := saveOccurrence(ctx, tx, &occ); e != nil {
			return runtime.Disposition{}, e
		}
		return disposition, nil
	})
}
func (s *Service) waitOccurrence(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return s.workTx(ctx, store, scope, work, func(tx runtime.Tx) (runtime.Disposition, error) {
		now, err := tx.Now(ctx)
		if err != nil {
			return runtime.Disposition{}, err
		}
		return runtime.Waiting(now.Add(time.Second)), nil
	})
}
