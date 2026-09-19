package content

import (
	"context"
	"lerna/brain"
	"lerna/execution"
	"lerna/tasks"
)

type ExecutionQueries interface {
	ChargeExecutionQuery(context.Context, tasks.ActionBinding, string) error
}

// ExecutionBudget binds observations to the original action. Core still checks
// current authority, recovery qualification and remaining quota on every charge.
type ExecutionBudget struct {
	queries ExecutionQueries
	action  tasks.ActionBinding
	denied  error
}

// BindExecution snapshots a host-validated request without granting new quota.
// denied preserves the consumer's error vocabulary for a qualification mismatch.
func BindExecution(queries ExecutionQueries, r execution.Request, denied error) ExecutionBudget {
	return ExecutionBudget{queries, tasks.ActionBinding{Qualification: r.Qualification, OperationID: r.OperationID, Descriptor: r.DescriptorSHA256, InputRef: r.InputRef, ResourceVersion: r.ResourceVersion, ControlVersion: r.ControlVersion}, denied}
}

func (b ExecutionBudget) Action() tasks.ActionBinding { return b.action }

func (b ExecutionBudget) ChargeQuery(ctx context.Context, q tasks.Qualification, key string) error {
	if b.queries == nil || q != b.action.Qualification {
		if b.denied != nil {
			return b.denied
		}
		return brain.Error("PERMISSION_DENIED")
	}
	return b.queries.ChargeExecutionQuery(ctx, b.action, key)
}
