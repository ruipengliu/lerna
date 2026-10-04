package decisionfixture

import (
	"context"
	"errors"
	"time"

	process "github.com/ruipengliu/lerna/conformance/internal/testkit/process"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

// SourceDescriptor identifies the actual Source owner for a borrowed current
// test child. It contains no service connection credentials or deletion power.
type SourceDescriptor struct {
	Schema string
	Owner  v.OwnerRef
}

func (w *SourceWorld) ProcessSource() SourceDescriptor {
	return SourceDescriptor{Schema: w.cfg.Schema, Owner: w.owner}
}

// BorrowChild holds the same physical child at BOTH acknowledged owner scopes
// before Start/ready/connection creation. This is a concrete two-owner fixture,
// not a resource registry or arbitrary factory.
func (w *World) BorrowChild(child *process.Child) error {
	if child == nil || w.SourceWorld == nil || w.closing || len(w.children) >= 16 {
		return errors.New("invalid Decision fixture child borrow")
	}
	w.children = append(w.children, child)
	w.SourceWorld.children = append(w.SourceWorld.children, child)
	return nil
}

// Reopening an owner does not terminate a running borrowed child. First wait
// for its actual exit, then confirm all parent pipe closes before any SQL Close.
func joinChildren(ctx context.Context, children []*process.Child) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var errs []error
	for _, child := range children {
		confirmed, waitErr := child.Wait(ctx)
		if !confirmed {
			return errors.Join(errors.Join(errs...), waitErr)
		}
		confirmed, closeErr := child.Stop(ctx)
		if !confirmed {
			return errors.Join(errors.Join(errs...), closeErr)
		}
		// Stop preserves unhandled historical causes and acknowledges validated
		// SIGKILL; native Wait cause alone is not a lack of exit confirmation.
		errs = append(errs, closeErr)
	}
	return errors.Join(errs...)
}

func stopChildren(children []*process.Child) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	confirmed := true
	var errs []error
	for _, child := range children {
		done, err := child.Stop(ctx)
		confirmed = confirmed && done
		errs = append(errs, err)
	}
	return confirmed, errors.Join(errs...)
}
