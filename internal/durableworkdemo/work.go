package durableworkdemo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
)

// WorkRepository is owned by the project consumer; it is not required by the
// admission-only Host or adapters that have not implemented work yet.
type WorkRepository interface {
	TryLockInput(context.Context, runtime.Tx, contract.OwnerRef, contract.ID) (*Input, error)
	LockInput(context.Context, runtime.Tx, contract.OwnerRef, contract.ID) (*Input, error)
	SaveProjection(context.Context, runtime.Tx, contract.OwnerRef, contract.ID, Projection) error
}
type Projection struct {
	InputRevision int64
	TextDigest    string
}
type Work struct {
	Claim runtime.Claim
	Input Input
}
type Worker struct {
	Owner      contract.OwnerRef
	Runner     runtime.TxRunner
	Claims     runtime.ClaimStore
	Repository WorkRepository
	Clock      runtime.Clock
}

func workContext(ctx context.Context) error {
	if ctx == nil {
		return runtime.ErrWorkBounds
	}
	if _, finite := ctx.Deadline(); !finite {
		return runtime.ErrWorkBounds
	}
	return ctx.Err()
}

func (w *Worker) Claim(ctx context.Context, worker string, limit int, lease time.Duration) ([]Work, error) {
	if err := workContext(ctx); err != nil {
		return nil, err
	}
	if worker == "" || len(worker) > 128 || limit < 1 || limit > 64 || lease < time.Millisecond || lease > 5*time.Minute {
		return nil, runtime.ErrWorkBounds
	}
	var batch []Work
	err := w.Runner.Within(ctx, w.Owner, func(ctx context.Context, tx runtime.Tx) error {
		now, err := w.Clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		now = now.UTC().Truncate(time.Microsecond)
		candidates, err := w.Claims.Scan(ctx, tx, now, limit)
		if err != nil {
			return err
		}
		for _, job := range candidates {
			if job.Phase != "project" || job.Object.Kind != "durable_work" {
				continue
			}
			input, err := w.Repository.TryLockInput(ctx, tx, w.Owner, job.Object.ID)
			if err != nil {
				return err
			}
			if input == nil {
				continue
			}
			// Read trusted time after the object lock, including time spent waiting.
			now, err = w.Clock.Now(ctx, tx)
			if err != nil {
				return err
			}
			now = now.UTC().Truncate(time.Microsecond)
			claim, err := w.Claims.Claim(ctx, tx, job, worker, now, now.Add(lease))
			if err != nil {
				return err
			}
			if claim == nil {
				continue
			}
			if input.Revision != claim.ClaimedRevision {
				return runtime.ErrClaim
			}
			batch = append(batch, Work{Claim: *claim, Input: *input})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return batch, nil
}

// Project hashes the exact UTF-8 bytes, without holding a Tx or changing Input.
func Project(work Work) Projection {
	digest := sha256.Sum256([]byte(work.Input.Text))
	return Projection{InputRevision: work.Input.Revision, TextDigest: "sha256:" + hex.EncodeToString(digest[:])}
}

func (w *Worker) Complete(ctx context.Context, claim runtime.Claim, projection Projection) error {
	if err := workContext(ctx); err != nil {
		return err
	}
	if claim.Object.TenantID != w.Owner.TenantID || claim.Object.OwnerID != w.Owner.OwnerID || claim.Object.Kind != "durable_work" || claim.Phase != "project" || projection.InputRevision != claim.ClaimedRevision {
		return runtime.ErrClaim
	}
	return w.Runner.Within(ctx, w.Owner, func(ctx context.Context, tx runtime.Tx) error {
		input, err := w.Repository.LockInput(ctx, tx, w.Owner, claim.Object.ID)
		if err != nil {
			return err
		}
		if input == nil {
			return runtime.ErrClaim
		}
		now, err := w.Clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		now = now.UTC().Truncate(time.Microsecond)
		if err = w.Claims.Complete(ctx, tx, claim, now); err != nil {
			return err
		}
		return w.Repository.SaveProjection(ctx, tx, w.Owner, claim.Object.ID, projection)
	})
}

func (w *Worker) Renew(ctx context.Context, claim runtime.Claim, lease time.Duration) (runtime.Claim, error) {
	if err := workContext(ctx); err != nil {
		return runtime.Claim{}, err
	}
	if lease < time.Millisecond || lease > 5*time.Minute {
		return runtime.Claim{}, runtime.ErrWorkBounds
	}
	if claim.Object.TenantID != w.Owner.TenantID || claim.Object.OwnerID != w.Owner.OwnerID || claim.Object.Kind != "durable_work" || claim.Phase != "project" {
		return runtime.Claim{}, runtime.ErrClaim
	}
	var renewed runtime.Claim
	err := w.Runner.Within(ctx, w.Owner, func(ctx context.Context, tx runtime.Tx) error {
		input, err := w.Repository.LockInput(ctx, tx, w.Owner, claim.Object.ID)
		if err != nil {
			return err
		}
		if input == nil {
			return runtime.ErrClaim
		}
		now, err := w.Clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		now = now.UTC().Truncate(time.Microsecond)
		renewed, err = w.Claims.Renew(ctx, tx, claim, now, now.Add(lease))
		return err
	})
	if err != nil {
		return runtime.Claim{}, err
	}
	return renewed, nil
}
