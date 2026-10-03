package interaction

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Service) Dispatch(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var snapshot submissionRecord
	if _, err := store.Read(ctx, scope, submissions, work.Job.SourceRef.ObjectID, 0, &snapshot); err != nil {
		return err
	}
	if snapshot.Submission.State == "withdrawn" || snapshot.Submission.State == "applied" || snapshot.Submission.State == "rejected" {
		return runtime.Finish(ctx, store, scope, s.config.Participants, work, runtime.Done(), nil)
	}
	if err := store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	var closure *Closure
	var answerErr error
	if snapshot.Submission.State == "queued" && snapshot.Input != nil {
		body, err := s.ports.Content.Read(ctx, scope, snapshot.Auth, snapshot.Input.AnswerRef, "interaction.input")
		if err != nil {
			return s.deferDispatch(ctx, store, scope, work)
		}
		if uint64(len(body)) != snapshot.Input.AnswerRef.ByteLength || api.Hash(body) != snapshot.Input.AnswerRef.Hash {
			answerErr = api.E("invalid_request", "answer_bytes_mismatch")
		} else {
			answerErr = validateAnswer(snapshot.InputView.AnswerSchema, body)
		}
	}
	if snapshot.Submission.State == "queued" && snapshot.Submission.PredecessorTaskRef != nil {
		if s.ports.Closure == nil {
			return api.E("unsupported", "closure_unconfigured")
		}
		v, err := s.ports.Closure.Closure(ctx, scope, snapshot.Auth, *snapshot.Submission.PredecessorTaskRef)
		if err != nil {
			return s.deferDispatch(ctx, store, scope, work)
		}
		if v.TaskRef.TenantID != scope.TenantID || v.TaskRef.OwnerID != snapshot.Submission.PredecessorTaskRef.OwnerID || v.TaskRef.ObjectID != snapshot.Submission.PredecessorTaskRef.ObjectID {
			return api.E("forbidden", "closure_target_mismatch")
		}
		if v.GoalWorkClosed && v.EffectsClosed {
			closure = &v
		}
	}
	var prepared submissionRecord
	var now time.Time
	ready := false
	status, err := store.Within(ctx, scope, s.config.Participants, func(tx runtime.Tx) error {
		var r submissionRecord
		if _, e := tx.Get(ctx, submissions, work.Job.SourceRef.ObjectID, &r); e != nil {
			return e
		}
		if r.Submission.State != "queued" && r.Submission.State != "sending" {
			return tx.Guard(ctx, work.Claim)
		}
		branch, e := getBranch(ctx, tx, r.Submission.SessionRef.ObjectID, r.Submission.BranchID)
		if e != nil {
			return e
		}
		now, e = tx.Now(ctx)
		if e != nil {
			return e
		}
		if r.Submission.State == "queued" {
			if r.GoalQueue && (len(branch.Pending) == 0 || branch.Pending[0] != r.Submission.SubmissionID) {
				return tx.Guard(ctx, work.Claim)
			}
			if r.OrderKey != "" {
				var order orderRecord
				if _, e = tx.Get(ctx, queues, r.OrderKey, &order); e != nil {
					return e
				}
				if len(order.Pending) == 0 || order.Pending[0] != r.Submission.SubmissionID {
					return tx.Guard(ctx, work.Claim)
				}
			}
			queueEnd, e := api.ParseTime(r.QueueDeadline)
			if e != nil {
				return e
			}
			rejection := answerErr
			if !now.Before(queueEnd) {
				rejection = api.E("expired", "submission_queue_elapsed")
			}
			if rejection == nil && r.Submission.PredecessorTaskRef != nil && closure == nil {
				return tx.Guard(ctx, work.Claim)
			}
			purpose := "task.goal"
			if r.Input != nil {
				purpose = "interaction.input"
			}
			if rejection == nil {
				if e = s.content(ctx, tx, r.Auth, r.Submission.ContentRef, purpose); e != nil {
					if api.IsCode(e, "dependency_unavailable") {
						return e
					}
					rejection = e
				}
			}
			if rejection == nil {
				for _, ref := range r.AttachmentRefs {
					purpose := "task.goal"
					if r.Input != nil {
						purpose = "interaction.preview"
					}
					if e = s.content(ctx, tx, r.Auth, ref, purpose); e != nil {
						rejection = e
						break
					}
				}
			}
			if rejection == nil && r.Input != nil {
				view, e := s.ports.Requests.CheckTx(ctx, tx, r.Auth, r.Input.RequestRef)
				if e != nil {
					rejection = e
				} else {
					expiry, _ := api.ParseTime(view.Request.ExpiresAt)
					if view.Request.Revision != r.Input.RequestRef.Revision || view.Request.State != "pending" || !now.Before(expiry) || !api.Equal(view.Request.PreviewRefs, r.Input.PreviewRefs) {
						rejection = api.E("invalid_state", "request_target_mismatch")
					}
				}
			}
			if rejection != nil {
				var apiErr *api.Error
				if e, ok := rejection.(*api.Error); ok {
					apiErr = e
				} else {
					return rejection
				}
				r.Error = apiErr
				r.Submission.State = "rejected"
				if e = s.releaseSubmission(ctx, tx, &r, &branch); e != nil {
					return e
				}
				if e = saveSubmission(ctx, tx, &r); e != nil {
					return e
				}
				prepared = r
				return tx.Guard(ctx, work.Claim)
			}
			if r.Command == nil {
				r.Command = s.goalCommand(scope, scope.Ref(r.Submission.SubmissionID, 1), r, now)
				r.Submission.DispatchCommandRef = &api.ObjectRef{TenantID: scope.TenantID, OwnerID: r.Command.LogicalServiceID, ObjectID: r.Command.CommandID, Revision: 1}
				r.ClosureRef = &closure.ClosureRef
			}
			r.Submission.State = "sending"
			if e = saveSubmission(ctx, tx, &r); e != nil {
				return e
			}
		}
		prepared = r
		ready = r.Command != nil
		return tx.Guard(ctx, work.Claim)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	if err != nil {
		return err
	}
	if !ready {
		if prepared.Submission.State == "rejected" {
			return runtime.Finish(ctx, store, scope, s.config.Participants, work, runtime.Done(), nil)
		}
		return s.deferDispatch(ctx, store, scope, work)
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
		return runtime.Finish(ctx, store, scope, s.config.Participants, work, runtime.Waiting(now.Add(time.Second)), func(tx runtime.Tx) error {
			var r submissionRecord
			if _, e := tx.Get(ctx, submissions, work.Job.SourceRef.ObjectID, &r); e != nil {
				return e
			}
			if r.Submission.State == "sending" {
				r.Receipt = &receipt
				return saveSubmission(ctx, tx, &r)
			}
			return nil
		})
	}
	return runtime.Finish(ctx, store, scope, s.config.Participants, work, runtime.Done(), func(tx runtime.Tx) error {
		var r submissionRecord
		if _, e := tx.Get(ctx, submissions, work.Job.SourceRef.ObjectID, &r); e != nil {
			return e
		}
		if r.Submission.State != "sending" {
			return nil
		}
		branch, e := getBranch(ctx, tx, r.Submission.SessionRef.ObjectID, r.Submission.BranchID)
		if e != nil {
			return e
		}
		r.Receipt = &receipt
		r.Submission.State = receipt.Stage
		if receipt.Stage == "applied" {
			var output struct {
				TaskRef *api.ObjectRef `json:"task_ref"`
			}
			if e = json.Unmarshal(receipt.Output, &output); e != nil {
				return e
			}
			if output.TaskRef != nil {
				if output.TaskRef.TenantID != scope.TenantID || output.TaskRef.OwnerID != r.Command.LogicalServiceID || output.TaskRef.ObjectID != r.Command.TargetID {
					return api.E("forbidden", "receipt_task_mapping_mismatch")
				}
				r.Submission.TaskRef = output.TaskRef
			}
		} else {
			r.Error = receipt.Error
		}
		if e = s.releaseSubmission(ctx, tx, &r, &branch); e != nil {
			return e
		}
		return saveSubmission(ctx, tx, &r)
	})
}
func checkReceipt(c api.Command, r api.Receipt) error {
	canonical, err := api.Canonical(api.Raw(c))
	if err != nil {
		return err
	}
	if r.CommandID != c.CommandID || r.RequestDigest != api.Hash(canonical) || r.Stage != "applied" && r.Stage != "rejected" && r.Stage != "accepted" {
		return api.E("forbidden", "original_receipt_mismatch")
	}
	return nil
}
func (s *Service) deferDispatch(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var now time.Time
	status, err := store.Within(ctx, scope, s.config.Participants, func(tx runtime.Tx) error {
		var e error
		now, e = tx.Now(ctx)
		if e != nil {
			return e
		}
		if e = tx.Guard(ctx, work.Claim); e != nil {
			return e
		}
		return tx.Finish(ctx, work.Claim, runtime.Waiting(now.Add(time.Second)))
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}
