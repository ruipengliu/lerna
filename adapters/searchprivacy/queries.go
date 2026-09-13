package searchprivacy

import (
	"context"
	"lerna/adapters/executioncontent"
	"lerna/adapters/taskcontent"
	"lerna/execution"
	"lerna/fetch"
	"lerna/tasks"
)

// ExecutionQueries binds observations to the original admitted action, including
// explicit recovery qualification. Constructing a view never grants fresh quota.
type ExecutionQueries interface {
	ChargeExecutionQuery(context.Context, tasks.ActionBinding, string) error
}

func (g *Guard) WithQueries(queries ExecutionQueries) (*Guard, error) {
	if queries == nil {
		return nil, fetch.Invalid
	}
	copy := *g
	copy.queries = queries
	return &copy, nil
}

func (g *Guard) forRequest(r execution.Request) (*executioncontent.Adapter, error) {
	if g.queries == nil {
		return g.reader, nil
	}
	c := g.capability
	if r.OperationID == "" || r.InputRef == "" || r.Capability != c.Name || r.Version != c.Version || r.Implementation != c.Implementation || r.ImplementationVersion != c.ImplementationVersion || r.DescriptorSHA256 != c.Digest() {
		return nil, fetch.Denied
	}
	budget := executionBudget{g.queries, tasks.ActionBinding{Qualification: r.Qualification, OperationID: r.OperationID, Descriptor: r.DescriptorSHA256, InputRef: r.InputRef, ResourceVersion: r.ResourceVersion, ControlVersion: r.ControlVersion}}
	content, err := taskcontent.New(g.content, budget, r.Qualification)
	if err != nil {
		return nil, err
	}
	return g.reader.WithContent(content), nil
}

type executionBudget struct {
	queries ExecutionQueries
	action  tasks.ActionBinding
}

func (b executionBudget) ChargeQuery(ctx context.Context, q tasks.Qualification, key string) error {
	if q != b.action.Qualification {
		return fetch.Denied
	}
	return b.queries.ChargeExecutionQuery(ctx, b.action, key)
}
