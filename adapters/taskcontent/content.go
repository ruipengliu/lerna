// Package taskcontent charges Content observations to an existing task query
// budget before I/O. The Core owns accounting, authorization and worker fencing.
package taskcontent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"lerna/artifacts"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
)

type Content interface {
	Call(context.Context, artifacts.Binding, *wire.ContentRequest) (*wire.ContentResponse, error)
}
type Budget interface {
	ChargeQuery(context.Context, tasks.Qualification, string) error
}
type Adapter struct {
	content       Content
	budget        Budget
	qualification tasks.Qualification
}

// New binds one exact task/worker qualification. Recovery must bind its current
// authorized qualification; constructing another adapter does not renew quota.
func New(content Content, budget Budget, q tasks.Qualification) (*Adapter, error) {
	if content == nil || budget == nil || q.Ref.Namespace == "" || q.Ref.TaskID == "" {
		return nil, brain.Error("INVALID_ARGUMENT")
	}
	return &Adapter{content, budget, q}, nil
}
func (a *Adapter) Call(ctx context.Context, b artifacts.Binding, in *wire.ContentRequest) (*wire.ContentResponse, error) {
	if in == nil || b.Namespace != a.qualification.Ref.Namespace {
		return nil, brain.Error("INVALID_ARGUMENT")
	}
	switch in.Method {
	case "GET", "READ", "LOOKUP":
		// Each attempt is new work, including failed reads. Random identities avoid
		// collisions across concurrent callers and process restarts; Core atomically
		// enforces the aggregate limit. No local counter or refund is involved.
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, err
		}
		if err := a.budget.ChargeQuery(ctx, a.qualification, "content/"+in.Method+"/"+hex.EncodeToString(id[:])); err != nil {
			return nil, err
		}
	}
	return a.content.Call(ctx, b, in)
}
