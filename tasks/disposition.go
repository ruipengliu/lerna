package tasks

import (
	"context"
	"lerna/authorization"
	"lerna/internal/randomid"
	"reflect"
	"unicode/utf8"
)

type ControlWorkStore interface {
	PollControl(context.Context, Ref) (RunSnapshot, error)
	Observe(context.Context, Observation) (RunSnapshot, error)
	ReserveCheck(context.Context, Ref, string) (RunSnapshot, error)
}

func observationQualification(r RunSnapshot) Qualification {
	q := QualificationOf(r)
	q.Version = r.Work[0].DecisionVersion
	return q
}
func (p *WorkPort) PollControl(ctx context.Context, ref Ref) (RunSnapshot, error) {
	var out RunSnapshot
	err := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, ok := j.Runs[ref.TaskID]
		if ref.Namespace != p.service.config.Namespace {
			return failure(authorization.Denied)
		}
		if !ok {
			return failure(authorization.NotFound)
		}
		identity, err := tx.Authorize(p.binding.Token, r.Task.Resource, "task.execute")
		if err != nil {
			identity, err = tx.Authorize(p.binding.Token, r.Task.Resource, "task.reconcile")
		}
		if err != nil {
			return err
		}
		if identity.Subject != p.binding.Subject || r.Task.Subject != p.binding.Subject {
			return failure(authorization.Denied)
		}
		out = r
		return nil
	})
	return out, err
}
func (p *WorkPort) Observe(ctx context.Context, in Observation) (RunSnapshot, error) {
	if !name(in.ChangeID) || len(in.Result) > 65536 || !utf8.ValidString(in.Result) || len(in.Evidence) > 1024 || !utf8.ValidString(in.Evidence) {
		return RunSnapshot{}, failure(authorization.Invalid)
	}
	switch in.Status {
	case "STOPPED", "UNKNOWN":
		if in.Result != "" || in.CompletedAt != 0 {
			return RunSnapshot{}, failure(authorization.Invalid)
		}
	case "COMPLETED":
		if in.Result == "" || in.Evidence == "" || in.CompletedAt <= 0 {
			return RunSnapshot{}, failure(authorization.Invalid)
		}
	default:
		return RunSnapshot{}, failure(authorization.Unsupported)
	}
	var out RunSnapshot
	err := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		q := in.Qualification
		r, ok := j.Runs[q.Ref.TaskID]
		if q.Ref.Namespace != p.service.config.Namespace {
			return failure(authorization.Denied)
		}
		if !ok {
			return failure(authorization.NotFound)
		}
		// A reconciler never needs target execution permission. For compatibility,
		// a script-only worker can also acknowledge its own uncontrolled return.
		scriptReturn := !r.Work[0].EffectAware && r.Task.Control.OperationID == "" && in.Status == "STOPPED"
		identity, err := tx.Authorize(p.binding.Token, r.Task.Resource, "task.reconcile")
		if err != nil && scriptReturn {
			identity, err = tx.Authorize(p.binding.Token, r.Task.Resource, "task.execute")
		}
		if err != nil {
			return err
		}
		if identity.Subject != p.binding.Subject || r.Task.Subject != p.binding.Subject || q.Owner != r.Task.Owner || q.Epoch != r.Task.OwnerEpoch {
			return failure(authorization.Denied)
		}
		if old, ok := j.Commits[q.Ref.TaskID][in.ChangeID]; ok {
			if old.Observation == nil || old.WorkerID != p.binding.WorkerID || !reflect.DeepEqual(*old.Observation, in) {
				return failure(authorization.IdentityConflict)
			}
			out = old.Snapshot
			return nil
		}
		w := &r.Work[0]
		if w.ExecutionOperation != "" {
			return failure(authorization.Unsupported)
		}
		if terminal(r.Task) || !w.InFlight || q.WorkID != w.ID || q.Generation != w.Generation || q.Version != w.DecisionVersion || w.Worker != p.binding.WorkerID {
			return failure(authorization.Conflict)
		}
		maxObservations := r.ControlLimits.MaxObservations
		if !r.ControlLimits.valid() {
			if !scriptReturn {
				return failure(authorization.Unsupported)
			}
			maxObservations = int(r.Limits.MaxAttempts)
		}
		count := 0
		for _, c := range j.Commits[q.Ref.TaskID] {
			if c.Observation != nil {
				count++
			}
		}
		if count >= maxObservations {
			return failure(authorization.Unavailable)
		}
		switch in.Status {
		case "UNKNOWN":
			r.Task.State = "WAITING"
			addWait(&r.Task, "reconciliation")
			if r.Task.Control.OperationID != "" {
				r.Task.Control.Progress = "ACCEPTED"
			}
			syncWait(&r.Task)
		case "STOPPED":
			w.InFlight = false
			w.LeaseUntil = 0
			removeWait(&r.Task, "reconciliation")
			settleControl(&r, tx, p.binding.Token)
		case "COMPLETED":
			if !w.EffectAware || !p.binding.AllowEffectEvidence || in.CompletedAt > tx.Now().UnixNano() || (controlIntent(r.Task) != "RUN" && in.CompletedAt > r.Task.Control.AcceptedAt) {
				return failure(authorization.Denied)
			}
			w.InFlight = false
			if r.UpdateVersion > w.DecisionVersion {
				// Preserve the original effect evidence in this observation commit,
				// without treating it as completion of the updated goal.
				w.LeaseUntil = 0
				removeWait(&r.Task, "reconciliation")
				settleControl(&r, tx, p.binding.Token)
				break
			}
			w.Done = true
			w.LeaseUntil = 0
			r.Task.State = "COMPLETED"
			r.Task.Result = in.Result
			r.Task.WaitingReasons = nil
			r.Task.StopReason = ""
			if r.Task.Control.OperationID != "" {
				r.Task.Control.Progress = "NOT_PREVENTED"
			}
		}
		r.Task.Version++
		r.LastCommit = CommitReceipt{Ref: q.Ref, ChangeID: in.ChangeID, Version: r.Task.Version}
		r.Records = append(r.Records, Record{Kind: "disposition:" + in.Status, Version: r.Task.Version})
		copyInput := in
		j.Commits[q.Ref.TaskID][in.ChangeID] = commit{Receipt: r.LastCommit, WorkerID: p.binding.WorkerID, Observation: &copyInput, Snapshot: r}
		j.Runs[q.Ref.TaskID] = r
		out = r
		return nil
	})
	if err != nil {
		return RunSnapshot{}, err
	}
	return out, nil
}

// ReserveCheck durably consumes a finite disposition attempt before invoking
// the external source. It neither spends target steps nor authorizes new work.
func (p *WorkPort) ReserveCheck(ctx context.Context, ref Ref, id string) (RunSnapshot, error) {
	var out RunSnapshot
	err := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		if ref.Namespace != p.service.config.Namespace || !name(id) {
			return failure(authorization.Invalid)
		}
		r, ok := j.Runs[ref.TaskID]
		if !ok {
			return failure(authorization.NotFound)
		}
		identity, err := tx.Authorize(p.binding.Token, r.Task.Resource, "task.reconcile")
		if err != nil {
			return err
		}
		if identity.Subject != p.binding.Subject || r.Task.Subject != p.binding.Subject || r.Work[0].Worker != p.binding.WorkerID {
			return failure(authorization.Denied)
		}
		if old, ok := j.Commits[ref.TaskID][id]; ok {
			if old.WorkerID != p.binding.WorkerID || old.DispositionCheck != ref {
				return failure(authorization.IdentityConflict)
			}
			out = old.Snapshot
			out.CheckAlreadyReserved = true
			return nil
		}
		if terminal(r.Task) || !r.Work[0].InFlight {
			return failure(authorization.Conflict)
		}
		if !r.ControlLimits.valid() {
			return failure(authorization.Unsupported)
		}
		if r.DispositionChecks >= r.ControlLimits.MaxChecks {
			return failure(authorization.Unavailable)
		}
		r.DispositionChecks++
		r.LastCommit = CommitReceipt{Ref: ref, ChangeID: id, Version: r.Task.Version}
		r.Records = append(r.Records, Record{Kind: "disposition-check", Version: r.Task.Version})
		j.Commits[ref.TaskID][id] = commit{Receipt: r.LastCommit, WorkerID: p.binding.WorkerID, DispositionCheck: ref, Snapshot: r}
		j.Runs[ref.TaskID] = r
		out = r
		return nil
	})
	return out, err
}

type Disposition struct {
	Status, Result, Evidence string
	CompletedAt              int64
}

// Sources are trusted adapters bound to the original work's WorkerID. They
// verify their own completion evidence; a decision proposal is not such evidence.
type DispositionSource interface {
	RequestStop(context.Context, Work) error
	Inspect(context.Context, Work) (Disposition, error)
}

// ReconcileDisposition performs one reserved, bounded attempt. On unknown
// reserve outcome it returns without invoking the source; lookup the original
// change ID through RunStore before retrying that reservation.
func ReconcileDisposition(ctx context.Context, store ControlWorkStore, source DispositionSource, ref Ref, changeID string, limits ControlLimits) (RunSnapshot, error) {
	if store == nil || source == nil || !name(changeID) || !limits.valid() {
		return RunSnapshot{}, failure(authorization.Invalid)
	}
	pollCtx, pollCancel := context.WithTimeout(ctx, limits.IOTimeout)
	defer pollCancel()
	current, err := store.PollControl(pollCtx, ref)
	if err != nil {
		return RunSnapshot{}, err
	}
	if terminal(current.Task) || !current.Work[0].InFlight {
		return current, nil
	}
	if current.ControlLimits != limits {
		return RunSnapshot{}, failure(authorization.Unsupported)
	}
	bounded, cancel := context.WithTimeout(ctx, current.ControlLimits.IOTimeout)
	defer cancel()
	reserved, err := store.ReserveCheck(bounded, ref, changeID)
	if err != nil {
		return RunSnapshot{}, err
	}
	if reserved.CheckAlreadyReserved {
		return current, nil
	}
	ioCtx, ioCancel := context.WithTimeout(ctx, current.ControlLimits.IOTimeout)
	defer ioCancel()
	disposition := Disposition{Status: "UNKNOWN"}
	if err = source.RequestStop(ioCtx, reserved.Work[0]); err == nil {
		if d, e := source.Inspect(ioCtx, reserved.Work[0]); e == nil {
			disposition = d
		}
	}
	id, err := randomid.New()
	if err != nil {
		return RunSnapshot{}, err
	}
	observeCtx, observeCancel := context.WithTimeout(context.WithoutCancel(ctx), current.ControlLimits.IOTimeout)
	defer observeCancel()
	in := Observation{ChangeID: id, Qualification: observationQualification(reserved), Status: disposition.Status, Result: disposition.Result, Evidence: disposition.Evidence, CompletedAt: disposition.CompletedAt}
	out, err := store.Observe(observeCtx, in)
	return observationResult(in, out, err)
}
