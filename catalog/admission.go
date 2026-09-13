package catalog

import (
	"context"
	"lerna/authorization"
	"lerna/execution"
)

// Admission wraps an already authenticated exact-version executor. The host
// retains that executor for old operations; only new admissions need an active
// declaration. VersionLock serializes publication with the durable admission.
type VersionLock interface {
	WithVersion(context.Context, Ref, func(Entry) error) error
}
type Executor interface {
	Invoke(context.Context, execution.Request, string) (execution.Receipt, error)
	GetInvocation(context.Context, string) (execution.Record, error)
}
type Admission struct {
	Store    VersionLock
	Executor Executor
	Ref      Ref
}

func (a Admission) Invoke(ctx context.Context, r execution.Request, material string) (execution.Receipt, error) {
	if a.Store == nil || a.Executor == nil || r.Qualification.Ref.Namespace != a.Ref.Namespace || r.Capability != a.Ref.Name || r.Version != a.Ref.Version || r.Implementation != a.Ref.Implementation || r.ImplementationVersion != a.Ref.ImplementationVersion || r.DescriptorSHA256 != a.Ref.Digest {
		return execution.Receipt{}, fail(authorization.Invalid)
	}
	if _, e := a.Executor.GetInvocation(ctx, r.OperationID); e == nil {
		return a.Executor.Invoke(ctx, r, material)
	} else if !authorization.Is(e, authorization.Denied) && !authorization.Is(e, authorization.NotFound) {
		return execution.Receipt{}, e
	}
	var out execution.Receipt
	e := a.Store.WithVersion(ctx, a.Ref, func(entry Entry) error {
		if entry.Capability.Digest() != r.DescriptorSHA256 {
			return fail(authorization.IdentityConflict)
		}
		var e error
		out, e = a.Executor.Invoke(ctx, r, material)
		return e
	})
	return out, e
}
