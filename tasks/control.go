package tasks

import (
	"context"
	"lerna/authorization"
	"lerna/internal/randomid"
	"slices"
	"sort"
	"time"
	"unicode/utf8"
)

type ControlLimits struct {
	MaxOperations, MaxObservations, MaxChecks int
	PollInterval, StopTimeout, IOTimeout      time.Duration
}

func (c ControlLimits) valid() bool {
	return c.MaxOperations >= 1 && c.MaxOperations <= 128 && c.MaxObservations >= 1 && c.MaxObservations <= 128 && c.MaxChecks >= 1 && c.MaxChecks <= 32 && c.PollInterval >= 10*time.Millisecond && c.PollInterval <= time.Second && c.StopTimeout >= time.Millisecond && c.StopTimeout <= time.Second && c.IOTimeout >= time.Millisecond && c.IOTimeout <= time.Second
}

type ControlState struct {
	Intent, Progress, OperationID string
	AcceptedAt                    int64
}
type ControlRequest struct {
	OperationID     string
	Ref             Ref
	ExpectedVersion uint64
	Intent, Reason  string
}
type ControlReceipt struct {
	OperationID     string
	Ref             Ref
	Intent, Outcome string
	Version         uint64
}
type controlRecord struct {
	Subject string
	Request ControlRequest
	Receipt ControlReceipt
}
type ControlService struct {
	service *Service
	limits  ControlLimits
}

func (s *Service) Controls(c ControlLimits) (*ControlService, error) {
	if !c.valid() {
		return nil, failure(authorization.Invalid)
	}
	return &ControlService{s, c}, nil
}
func controlAction(intent string) string {
	switch intent {
	case "PAUSE":
		return "task.pause"
	case "CANCEL":
		return "task.cancel"
	case "RUN":
		return "task.resume"
	}
	return ""
}
func controlIntent(t Task) string {
	if t.Control.Intent == "" {
		return "RUN"
	}
	return t.Control.Intent
}
func terminal(t Task) bool {
	return t.State == "COMPLETED" || t.State == "FAILED" || t.State == "CANCELLED"
}
func normalizeWaiting(t *Task) {
	if len(t.WaitingReasons) == 0 && t.State == "WAITING" && t.StopReason != "" {
		t.WaitingReasons = []string{t.StopReason}
	}
}
func addWait(t *Task, reason string) {
	normalizeWaiting(t)
	if !slices.Contains(t.WaitingReasons, reason) {
		t.WaitingReasons = append(t.WaitingReasons, reason)
	}
}
func removeWait(t *Task, reason string) {
	normalizeWaiting(t)
	t.WaitingReasons = slices.DeleteFunc(t.WaitingReasons, func(s string) bool { return s == reason })
	if t.StopReason == reason {
		t.StopReason = ""
		if len(t.WaitingReasons) > 0 {
			t.StopReason = t.WaitingReasons[0]
		}
	}
}
func syncWait(t *Task) {
	t.StopReason = ""
	if len(t.WaitingReasons) > 0 {
		t.StopReason = t.WaitingReasons[0]
	}
}
func (c *ControlService) Request(ctx context.Context, token string, in ControlRequest) (ControlReceipt, error) {
	if controlAction(in.Intent) == "" || !name(in.Ref.Namespace) || !name(in.Ref.TaskID) || len(in.OperationID) == 0 || len(in.OperationID) > 1024 || in.ExpectedVersion == 0 || len(in.Reason) > 1024 || !utf8.ValidString(in.Reason) {
		return ControlReceipt{}, failure(authorization.Invalid)
	}
	changeID, err := randomid.New()
	if err != nil {
		return ControlReceipt{}, err
	}
	var out ControlReceipt
	err = c.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		identity, err := tx.Authorize(token, c.service.config.Resource, controlAction(in.Intent))
		if err != nil {
			return err
		}
		if in.Ref.Namespace != identity.Namespace {
			return failure(authorization.Denied)
		}
		if err = tx.Operation(in.OperationID, identity.Subject, false); err != nil {
			return err
		}
		if delegationOperation(j, in.OperationID) {
			return failure(authorization.IdentityConflict)
		}
		if _, exists := j.InputChanges[in.OperationID]; exists {
			return failure(authorization.IdentityConflict)
		}
		if _, exists := j.Operations[in.OperationID]; exists {
			return failure(authorization.IdentityConflict)
		}
		if old, exists := j.Controls[in.OperationID]; exists {
			if old.Subject != identity.Subject {
				return failure(authorization.Denied)
			}
			if old.Request != in {
				return failure(authorization.IdentityConflict)
			}
			out = old.Receipt
			return nil
		}
		r, ok := j.Runs[in.Ref.TaskID]
		if !ok {
			return failure(authorization.NotFound)
		}
		if r.Task.Subject != identity.Subject {
			return failure(authorization.Denied)
		}
		if j.ControlLimits != (ControlLimits{}) && j.ControlLimits != c.limits {
			return failure(authorization.Invalid)
		}
		count := 0
		for _, old := range j.Controls {
			if old.Request.Ref == in.Ref {
				count++
			}
		}
		if count >= c.limits.MaxOperations {
			return failure(authorization.Unavailable)
		}
		if r.Task.Version != in.ExpectedVersion && !terminal(r.Task) {
			return failure(authorization.Conflict)
		}
		if err = tx.Operation(in.OperationID, identity.Subject, true); err != nil {
			return err
		}
		out = ControlReceipt{OperationID: in.OperationID, Ref: in.Ref, Intent: in.Intent, Version: r.Task.Version, Outcome: "accepted"}
		if terminal(r.Task) {
			out.Outcome = "already_" + r.Task.State
		} else if in.Intent == "RUN" && controlIntent(r.Task) != "PAUSE" {
			return failure(authorization.Conflict)
		} else if controlIntent(r.Task) == "CANCEL" && in.Intent != "CANCEL" {
			return failure(authorization.Conflict)
		} else {
			normalizeWaiting(&r.Task)
			if r.Task.State == "RUNNING" && r.Work[0].DecisionVersion == 0 {
				r.Work[0].InFlight = true
				r.Work[0].DecisionVersion = r.Task.Version
			}
			r.ControlLimits = c.limits
			cutoff := tx.Now().UnixNano()
			if controlIntent(r.Task) == in.Intent && in.Intent != "RUN" && r.Task.Control.AcceptedAt > 0 {
				cutoff = r.Task.Control.AcceptedAt
			}
			r.Task.Control = ControlState{Intent: in.Intent, Progress: "ACCEPTED", OperationID: in.OperationID, AcceptedAt: cutoff}
			r.Task.Version++
			out.Version = r.Task.Version
			if in.Intent == "RUN" {
				removeWait(&r.Task, "pause")
			}
			settleControl(&r, tx, token)
			r.LastCommit = CommitReceipt{Ref: in.Ref, ChangeID: changeID, Version: r.Task.Version}
			r.Records = append(r.Records, Record{Kind: "control:" + in.Intent, Version: r.Task.Version})
			copyInput := in
			j.Commits[in.Ref.TaskID][changeID] = commit{Receipt: r.LastCommit, ControlChange: &copyInput, Snapshot: r}
			j.Runs[in.Ref.TaskID] = r
		}
		j.ControlLimits = c.limits
		j.Controls[in.OperationID] = controlRecord{identity.Subject, in, out}
		return nil
	})
	if err != nil {
		return ControlReceipt{}, err
	}
	return out, nil
}
func settleControl(r *RunSnapshot, tx authorization.RuntimeTransaction, token string) {
	t := &r.Task
	// Legacy RUNNING tasks with no recorded start boundary are conservatively
	// treated as in flight until a source supplies disposition evidence.
	inFlight := r.Work[0].InFlight || (r.Work[0].DecisionVersion == 0 && r.Work[0].Generation > 0 && t.State == "RUNNING")
	if inFlight || delegationPending(*r) {
		r.Work[0].InFlight = inFlight
		addWait(t, "reconciliation")
		t.State = "WAITING"
		t.Control.Progress = "ACCEPTED"
		syncWait(t)
		return
	}
	removeWait(t, "reconciliation")
	switch controlIntent(*t) {
	case "CANCEL":
		t.State = "CANCELLED"
		if t.Control.OperationID != "" {
			t.Control.Progress = "APPLIED"
		}
		r.Work[0].Done = true
		r.Work[0].LeaseUntil = 0
		t.WaitingReasons = nil
		t.StopReason = "cancelled"
	case "PAUSE":
		addWait(t, "pause")
		t.State = "WAITING"
		if t.Control.OperationID != "" {
			t.Control.Progress = "APPLIED"
		}
		r.Work[0].LeaseUntil = 0
		syncWait(t)
	case "RUN":
		if _, err := tx.Authorize(token, t.Resource, "task.execute"); err != nil {
			addWait(t, "authorization")
		} else {
			removeWait(t, "authorization")
		}
		if tx.Now().Unix() >= t.Constraints.DeadlineUnix {
			addWait(t, "deadline")
		}
		if t.Attempts >= t.Constraints.MaxSteps {
			addWait(t, "budget")
		}
		if r.Limits.MaxAttempts > 0 && t.Attempts >= r.Limits.MaxAttempts {
			addWait(t, "retry_limit")
		}
		if t.Control.OperationID != "" {
			t.Control.Progress = "APPLIED"
		}
		t.State = "QUEUED"
		if len(t.WaitingReasons) > 0 {
			t.State = "WAITING"
		}
		syncWait(t)
	}
}
func (c *ControlService) Lookup(ctx context.Context, token, namespace, id string) (ControlReceipt, error) {
	var out ControlReceipt
	err := c.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		identity, err := tx.Authorize(token, c.service.config.Resource, "task.read")
		if err != nil {
			return err
		}
		if namespace != identity.Namespace {
			return failure(authorization.Denied)
		}
		if err = tx.Operation(id, identity.Subject, false); err != nil {
			return err
		}
		record, ok := j.Controls[id]
		if !ok {
			if _, exists := j.Operations[id]; exists {
				return failure(authorization.IdentityConflict)
			}
			return failure(authorization.NotFound)
		}
		if record.Subject != identity.Subject {
			return failure(authorization.Denied)
		}
		if _, err = tx.Authorize(token, c.service.config.Resource, controlAction(record.Request.Intent)); err != nil {
			return err
		}
		out = record.Receipt
		return nil
	})
	return out, err
}

// Observation will be supplied only by a bound, trusted disposition source.
// It is not exposed by the task SDK or control request protocol.
type Observation struct {
	ChangeID                 string
	Qualification            Qualification
	Status, Result, Evidence string
	CompletedAt              int64
}

// ListPending rediscovers disposition work after lost notifications or restart.
// It does not confer target execution authority or expose other subjects' tasks.
func (c *ControlService) ListPending(ctx context.Context, token string, q RecoveryQuery) (RunPage, error) {
	if q.Limit < 1 || q.Limit > c.service.config.MaxPage || len(q.After) > 128 {
		return RunPage{}, failure(authorization.Invalid)
	}
	var out RunPage
	err := c.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		identity, err := tx.Authorize(token, c.service.config.Resource, "task.reconcile")
		if err != nil {
			return err
		}
		if q.Namespace != identity.Namespace {
			return failure(authorization.Denied)
		}
		ids := []string{}
		for id, r := range j.Runs {
			if id > q.After && r.Task.Subject == identity.Subject && !terminal(r.Task) && r.Work[0].InFlight {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		out = RunPage{}
		for i, id := range ids {
			if i == q.Limit {
				out.Next = ids[i-1]
				break
			}
			out.Runs = append(out.Runs, j.Runs[id])
		}
		return nil
	})
	return out, err
}

// PrepareDisposition freezes recovery limits without creating user intent or
// spending a target step. A current reconciler can use it after a start crash,
// even when no pause/cancel request has ever existed.
func (c *ControlService) PrepareDisposition(ctx context.Context, token string, ref Ref) (RunSnapshot, error) {
	var out RunSnapshot
	err := c.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		if ref.Namespace != c.service.config.Namespace {
			return failure(authorization.Denied)
		}
		r, ok := j.Runs[ref.TaskID]
		if !ok {
			return failure(authorization.NotFound)
		}
		identity, err := tx.Authorize(token, r.Task.Resource, "task.reconcile")
		if err != nil {
			return err
		}
		if identity.Subject != r.Task.Subject {
			return failure(authorization.Denied)
		}
		if terminal(r.Task) || !r.Work[0].InFlight {
			return failure(authorization.Conflict)
		}
		if r.ControlLimits != (ControlLimits{}) && r.ControlLimits != c.limits {
			return failure(authorization.Invalid)
		}
		if j.ControlLimits != (ControlLimits{}) && j.ControlLimits != c.limits {
			return failure(authorization.Invalid)
		}
		r.ControlLimits = c.limits
		j.ControlLimits = c.limits
		j.Runs[ref.TaskID] = r
		out = r
		return nil
	})
	return out, err
}
