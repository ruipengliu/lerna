package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
)

type frame struct {
	Ref    WorkRef
	Task   *TaskState
	Caller api.Caller
}

func (c *Coordinator) ownerCaller(t *TaskState) api.Caller {
	return api.Caller{TenantID: c.Config.Scope.TenantID, ActorID: c.Config.Scope.OwnerID, SourceOwnerID: c.Config.Scope.OwnerID, OnBehalfOf: t.SubjectID}
}
func outcome(r durable.Result) error {
	if r.Outcome == durable.CommitUnknown {
		return api.ErrCommitUnknown
	}
	return r.Err
}
func (c *Coordinator) frame(ctx context.Context, u Unit) (frame, error) {
	var f frame
	r := u.Within(ctx, func(tx *durable.Tx) error {
		if err := c.Repository.Read(tx); err != nil {
			return err
		}
		claim := u.Claim()
		if claim.Scope() != c.Config.Scope {
			return durable.ErrScope
		}
		row, err := c.Repository.Get(tx, "work", claim.Key().Responsibility)
		if err != nil {
			return err
		}
		if row != nil {
			f.Ref, err = Decode[WorkRef](row)
			if err != nil {
				return err
			}
			if workKey(f.Ref) != claim.Key() || f.Ref.TaskID != claim.SourceRef() {
				return durable.ErrInvariant
			}
			f.Task, err = c.Repository.Task(tx, f.Ref.TaskID)
			if err != nil {
				return err
			}
			if f.Task == nil {
				return durable.ErrInvariant
			}
			f.Caller = c.ownerCaller(f.Task)
		} else if claim.Key().Kind == "verify" {
			row, e := c.Repository.Get(tx, "defect_impact", claim.SourceRef())
			if e != nil {
				return e
			}
			if row == nil {
				return durable.ErrInvariant
			}
			f.Ref = WorkRef{Kind: "verify", ObjectKind: "defect", ObjectID: claim.SourceRef()}
			if workKey(f.Ref) != claim.Key() {
				return durable.ErrInvariant
			}
			f.Caller = api.Caller{TenantID: c.Config.Scope.TenantID, ActorID: c.Config.Scope.OwnerID, SourceOwnerID: c.Config.Scope.OwnerID}
		} else if claim.Key().Kind == "settle" {
			receiver, err := c.Repository.Receiver(tx, claim.SourceRef())
			if err != nil {
				return err
			}
			if receiver == nil {
				return durable.ErrInvariant
			}
			f.Ref = WorkRef{Kind: "settle", ObjectKind: "receiver", ObjectID: receiver.Receiver.AllocationID}
			if workKey(f.Ref) != claim.Key() {
				return durable.ErrInvariant
			}
			f.Caller = api.Caller{TenantID: c.Config.Scope.TenantID, ActorID: c.Config.Scope.OwnerID, SourceOwnerID: c.Config.Scope.OwnerID}
			if receiver.Receiver.TaskID != nil {
				f.Task, err = c.Repository.Task(tx, *receiver.Receiver.TaskID)
				if err != nil {
					return err
				}
				if f.Task == nil {
					return durable.ErrInvariant
				}
				f.Caller = c.ownerCaller(f.Task)
			}
		} else {
			return durable.ErrInvariant
		}
		return tx.Guard(claim)
	})
	return f, outcome(r)
}
func (c *Coordinator) Handle(ctx context.Context, u Unit) (result error) {
	defer func() {
		if result != nil {
			claim := u.Claim()
			result = fmt.Errorf("orchestrator %s/%s source %s: %w", claim.Key().Kind, claim.Key().Responsibility, claim.SourceRef(), result)
		}
	}()
	var f frame
	var err error
	if err = u.Step(ctx, func() error { f, err = c.frame(ctx, u); return err }); err != nil {
		return err
	}
	if !c.dependencies(f.Ref) {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	switch f.Ref.Kind {
	case "decide":
		return c.decide(ctx, u, f)
	case "dispatch":
		return c.dispatch(ctx, u, f)
	case "poll":
		return c.poll(ctx, u, f)
	case "verify":
		return c.verify(ctx, u, f)
	case "control":
		return c.propagate(ctx, u, f)
	case "settle":
		return c.settle(ctx, u, f)
	case "extract":
		return c.extract(ctx, u, f)
	default:
		return durable.ErrUnsupported
	}
}
func (c *Coordinator) finish(ctx context.Context, u Unit, f frame, reason string, done bool) error {
	return u.Step(ctx, func() error {
		return outcome(u.Within(ctx, func(tx *durable.Tx) error {
			if f.Task == nil {
				if err := c.Repository.Read(tx); err != nil {
					return err
				}
				if _, err := c.Repository.Gate(tx, "receiver-"+f.Ref.ObjectID, false, false); err != nil {
					return err
				}
				now, err := tx.Now()
				if err != nil {
					return err
				}
				d := durable.Done()
				if !done {
					d = durable.Waiting(now.Add(c.Config.Limits.TrustedReview), reason)
				}
				_, err = tx.Finish(u.Claim(), d)
				return err
			}
			ch, t, err := c.load(tx, f.Task.Task.TaskID, false, nil)
			if err != nil {
				return err
			}
			d := durable.Done()
			if !done {
				d = durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), reason)
				if reason == "automatic_queries_exhausted" {
					d = durable.Waiting(ch.Now.Add(c.Config.Limits.TrustedReview), reason)
				}
			}
			if (reason == "dependency_unavailable" || reason == "authorization") && !done && !terminal(t) {
				kind := "dependency"
				if reason == "authorization" {
					kind = "authorization"
				}
				wait(t, kind, reason, &api.ObjectRef{OwnerID: c.Config.Scope.OwnerID, ID: u.Claim().Key().Responsibility, Revision: u.Claim().ObservedRevision()})
				t.Task.Revision++
				if err = c.persist(tx, ch, t); err != nil {
					return err
				}
			}
			claim := u.Claim()
			return c.flush(tx, ch, &claim, d)
		}))
	})
}
func (c *Coordinator) readRecord(ctx context.Context, u Unit, kind, id string) (*Record, error) {
	var out *Record
	err := u.Step(ctx, func() error {
		return outcome(u.Within(ctx, func(tx *durable.Tx) error {
			if err := c.Repository.Read(tx); err != nil {
				return err
			}
			var err error
			out, err = c.Repository.Get(tx, kind, id)
			return err
		}))
	})
	return out, err
}
func (c *Coordinator) external(ctx context.Context, u Unit, f frame, method, target string, fn func() error) error {
	return u.Step(ctx, func() error {
		if c.Ports.Clock == nil || !c.Ports.Clock.Ready() {
			return failure("dependency_unavailable", "qualified original clock is unavailable")
		}
		access := api.Access{Method: method, TargetID: target}
		if f.Task != nil {
			access.TaskID = f.Task.Task.TaskID
			access.PolicyRef = &f.Task.Task.PolicyRef
		}
		if err := c.Ports.Authority.Current(ctx, f.Caller, access, c.Ports.Clock.Now()); err != nil {
			return err
		}
		return fn()
	})
}
func (c *Coordinator) attempt(ctx context.Context, u Unit, f frame) (bool, error) {
	allowed := false
	err := u.Step(ctx, func() error {
		return outcome(u.Within(ctx, func(tx *durable.Tx) error {
			ch, t, err := c.load(tx, f.Task.Task.TaskID, false, nil)
			if err != nil {
				return err
			}
			id := u.Claim().Key().Responsibility
			r, err := c.Repository.Get(tx, "attempt", id)
			if err != nil {
				return err
			}
			v := AttemptState{}
			if r != nil {
				v, err = Decode[AttemptState](r)
				if err != nil {
					return err
				}
			}
			if v.Count >= t.Policy.MaxPolls {
				claim := u.Claim()
				return c.flush(tx, ch, &claim, durable.Waiting(ch.Now.Add(c.Config.Limits.TrustedReview), "automatic_queries_exhausted"))
			}
			v.Count++
			v.Observed = u.Claim().ObservedRevision()
			if err = c.Repository.Put(tx, t, rec("attempt", id, t.Task.TaskID, v.Count, "open", false, v)); err != nil {
				return err
			}
			claim := u.Claim()
			if err = c.jobCommit(tx, ch, &claim, nil); err != nil {
				return err
			}
			allowed = true
			return nil
		}))
	})
	return allowed, err
}
func (c *Coordinator) publish(ctx context.Context, u Unit, f frame, id string) (api.ContentRef, error) {
	r, err := c.readRecord(ctx, u, "publication", id)
	if err != nil {
		return api.ContentRef{}, err
	}
	p, err := Decode[Publication](r)
	if err != nil {
		return api.ContentRef{}, err
	}
	if p.Ref != nil {
		return *p.Ref, nil
	}
	var ref api.ContentRef
	if err = c.external(ctx, u, f, "content.write", id, func() error {
		var err error
		ref, err = c.Ports.Content.Write(ctx, f.Caller, p.ID, p.MediaType, p.Body)
		return err
	}); err != nil {
		return ref, err
	}
	if ref.TenantID != c.Config.Scope.TenantID || ref.Hash != p.Digest || ref.ByteLength != int64(len(p.Body)) || ref.MediaType != p.MediaType || validateValue("ContentRef", ref) != nil {
		return ref, failure("invalid_output", "publication owner returned a different exact body")
	}
	err = u.Step(ctx, func() error {
		return outcome(u.Within(ctx, func(tx *durable.Tx) error {
			ch, t, err := c.load(tx, p.TaskID, false, nil)
			if err != nil {
				return err
			}
			stored, err := c.Repository.Get(tx, "publication", p.ID)
			if err != nil {
				return err
			}
			current, err := Decode[Publication](stored)
			if err != nil {
				return err
			}
			if current.Digest != p.Digest {
				return durable.ErrInvariant
			}
			if current.Ref != nil {
				if Hash(current.Ref) != Hash(ref) {
					return durable.ErrInvariant
				}
				return tx.Guard(u.Claim())
			}
			current.Ref = &ref
			current.Body = nil
			if err = c.Repository.Put(tx, t, rec("publication", p.ID, t.Task.TaskID, 2, "closed", false, current)); err != nil {
				return err
			}
			claim := u.Claim()
			return c.jobCommit(tx, ch, &claim, nil)
		}))
	})
	return ref, err
}
func publication(id, task string, body any) (Publication, error) {
	b := Raw(body)
	canonical, err := durable.CanonicalJSON(b)
	if err != nil {
		return Publication{}, err
	}
	h := sha256.Sum256(canonical)
	return Publication{ID: id, TaskID: task, MediaType: "application/json", Digest: "sha256:" + hex.EncodeToString(h[:]), Body: canonical}, nil
}

func (c *Coordinator) changeWork(ctx context.Context, u Unit, f frame, subtree bool, fn func(*durable.Tx, *Change, *TaskState) (durable.Disposition, error)) error {
	return c.changeWorkWith(ctx, u, f, subtree, nil, nil, fn)
}
func (c *Coordinator) changeWorkWith(ctx context.Context, u Unit, f frame, subtree bool, extras []*TaskState, before func(*durable.Tx) error, fn func(*durable.Tx, *Change, *TaskState) (durable.Disposition, error)) error {
	return u.Step(ctx, func() error {
		return outcome(u.Within(ctx, func(tx *durable.Tx) error {
			if before != nil {
				if e := before(tx); e != nil {
					return fmt.Errorf("pre-change: %w", e)
				}
			}
			ch, t, err := c.load(tx, f.Task.Task.TaskID, subtree, extras)
			if err != nil {
				return fmt.Errorf("load change: %w", err)
			}
			clearWait(t, "dependency", u.Claim().Key().Responsibility)
			clearWait(t, "authorization", u.Claim().Key().Responsibility)
			d, err := fn(tx, ch, t)
			if err != nil {
				return fmt.Errorf("change body: %w", err)
			}
			claim := u.Claim()
			if err = c.flush(tx, ch, &claim, d); err != nil {
				return fmt.Errorf("flush change: %w", err)
			}
			return nil
		}))
	})
}

func (c *Coordinator) guardWork(ctx context.Context, u Unit, f frame, subtree bool, fn func(*durable.Tx, *Change, *TaskState) error) error {
	return u.Step(ctx, func() error {
		return outcome(u.Within(ctx, func(tx *durable.Tx) error {
			ch, t, e := c.load(tx, f.Task.Task.TaskID, subtree, nil)
			if e != nil {
				return e
			}
			if e = fn(tx, ch, t); e != nil {
				return e
			}
			claim := u.Claim()
			return c.jobCommit(tx, ch, &claim, nil)
		}))
	})
}
func notFound(err error) bool {
	var f *api.Failure
	return errors.As(err, &f) && f.Detail.Code == "not_found"
}
func originalCall(owner, method, target, id string, payload any, deadline time.Time) api.OriginalCall {
	return api.OriginalCall{OwnerID: owner, Command: api.Command{CommandID: id, Method: method, TargetID: target, ExpiresAt: stamp(deadline), Payload: Raw(payload)}}
}

func (c *Coordinator) dependencies(ref WorkRef) bool {
	p := c.Ports
	switch ref.Kind {
	case "decide":
		return p.Content != nil && p.Policies != nil && p.Brain != nil
	case "dispatch":
		if ref.ObjectKind == "delegation" {
			return p.Collaboration != nil
		}
		return p.Executor != nil
	case "poll":
		switch ref.ObjectKind {
		case "decision":
			return p.Brain != nil && p.Content != nil
		case "operation":
			return p.Executor != nil && p.Content != nil
		case "delegation":
			return true
		case "input":
			return p.Content != nil
		}
	case "control":
		if ref.ObjectKind == "delegation" {
			return p.Collaboration != nil
		}
		return p.Executor != nil
	case "settle":
		return p.Billing != nil && p.Content != nil
	case "verify":
		return ref.ObjectKind == "defect" || p.Verification != nil && p.Content != nil
	case "extract":
		return p.Content != nil && p.Extraction != nil
	}
	return false
}
