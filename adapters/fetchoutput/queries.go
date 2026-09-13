package fetchoutput

import (
	"context"
	"lerna/adapters/taskcontent"
	"lerna/artifacts"
	"lerna/execution"
	"lerna/tasks"
)

type ExecutionQueries interface {
	ChargeExecutionQuery(context.Context, tasks.ActionBinding, string) error
}

// ExecutionContent exposes only execution reads/saves, not the unqualified
// failure reader available on the general-purpose Adapter.
type ExecutionContent struct {
	base    *Adapter
	queries ExecutionQueries
}

var _ execution.RequestContent = (*ExecutionContent)(nil)
var _ execution.Content = (*ExecutionContent)(nil)

// WithQueries creates an execution-only view: unqualified Read/Save fail
// closed, and the original invocation qualification is used for every query.
// It does not replace that qualification with a newly acquired worker lease.
func (a *Adapter) WithQueries(budget ExecutionQueries) (*ExecutionContent, error) {
	if budget == nil {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	return &ExecutionContent{base: a, queries: budget}, nil
}

func (a *ExecutionContent) forRequest(r execution.Request, c execution.Capability) (*Adapter, error) {
	if r.OperationID == "" || r.InputRef == "" || r.Qualification.Ref.Namespace != a.base.binding.Namespace || r.Capability != c.Name || r.Version != c.Version || r.Implementation != c.Implementation || r.ImplementationVersion != c.ImplementationVersion || r.DescriptorSHA256 != c.Digest() {
		return nil, artifacts.Error("PERMISSION_DENIED")
	}
	budget := executionQueryBudget{queries: a.queries, action: tasks.ActionBinding{Qualification: r.Qualification, OperationID: r.OperationID, Descriptor: r.DescriptorSHA256, InputRef: r.InputRef, ResourceVersion: r.ResourceVersion, ControlVersion: r.ControlVersion}}
	content, err := taskcontent.New(a.base.content, budget, r.Qualification)
	if err != nil {
		return nil, err
	}
	return a.base.WithContent(content)
}

type executionQueryBudget struct {
	queries ExecutionQueries
	action  tasks.ActionBinding
}

func (b executionQueryBudget) ChargeQuery(ctx context.Context, q tasks.Qualification, key string) error {
	if q != b.action.Qualification {
		return artifacts.Error("PERMISSION_DENIED")
	}
	return b.queries.ChargeExecutionQuery(ctx, b.action, key)
}

func (a *ExecutionContent) ReadFor(ctx context.Context, token string, request execution.Request, capability execution.Capability) ([]byte, error) {
	bound, err := a.forRequest(request, capability)
	if err != nil {
		return nil, err
	}
	return bound.Read(ctx, token, request.InputRef, capability)
}

func (a *ExecutionContent) SaveFor(ctx context.Context, token string, request execution.Request, operation string, capability execution.Capability, output, evidence []byte) (string, error) {
	bound, err := a.forRequest(request, capability)
	if err != nil {
		return "", err
	}
	return bound.Save(ctx, token, operation, request.InputRef, capability, output, evidence)
}

func (a *ExecutionContent) Read(context.Context, string, string, execution.Capability) ([]byte, error) {
	return nil, artifacts.Error("PERMISSION_DENIED")
}

func (a *ExecutionContent) Save(context.Context, string, string, string, execution.Capability, []byte, []byte) (string, error) {
	return "", artifacts.Error("PERMISSION_DENIED")
}
