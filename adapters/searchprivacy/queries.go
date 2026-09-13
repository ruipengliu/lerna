package searchprivacy

import (
	"lerna/adapters/executioncontent"
	"lerna/adapters/taskcontent"
	"lerna/execution"
	"lerna/fetch"
)

func (g *Guard) WithQueries(queries taskcontent.ExecutionQueries) (*Guard, error) {
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
	budget := taskcontent.BindExecution(g.queries, r, fetch.Denied)
	content, err := taskcontent.New(g.content, budget, r.Qualification)
	if err != nil {
		return nil, err
	}
	return g.reader.WithContent(content), nil
}
