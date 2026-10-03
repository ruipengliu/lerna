package development

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/runtime"
)

type localDelivery struct{ a *App }

func (d localDelivery) Send(ctx context.Context, s runtime.Scope, auth runtime.Auth, c api.Command) (api.Receipt, error) {
	if c.LogicalServiceID != d.a.Config.OwnerID {
		return api.Receipt{}, api.E("unsupported", "remote_owner_not_configured")
	}
	if e := d.a.Identity.CheckCurrent(ctx, auth); e != nil {
		return api.Receipt{}, e
	}
	return d.a.Dispatcher.Command(ctx, auth, api.Raw(c))
}
func (d localDelivery) Lookup(ctx context.Context, s runtime.Scope, auth runtime.Auth, owner, id string) (api.Receipt, error) {
	if owner != d.a.Config.OwnerID {
		return api.Receipt{}, api.E("unsupported", "remote_owner_not_configured")
	}
	return d.a.Dispatcher.Lookup(ctx, auth, id)
}

type localClosure struct{ a *App }

func (c localClosure) Closure(ctx context.Context, s runtime.Scope, auth runtime.Auth, r api.ObjectRef) (interaction.Closure, error) {
	v, e := c.a.Task.Closure(ctx, c.a.Store, s, auth, r)
	return interaction.Closure{TaskRef: v.TaskRef, GoalWorkClosed: v.GoalWorkClosed, EffectsClosed: v.EffectsClosed, ClosureRef: v.ProofRef}, e
}

type requestBridge struct{ a *App }

func (r requestBridge) CheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ObjectRef) (interaction.RequestView, error) {
	v, e := r.a.Task.RequestViewTx(ctx, tx, auth, ref)
	return interaction.RequestView{Request: v.Request, AnswerSchema: v.AnswerSchema, Method: "task.input"}, e
}

func (r requestBridge) CheckBatchTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, refs []api.ObjectRef) ([]interaction.RequestView, error) {
	views, err := r.a.Task.RequestViewsTx(ctx, tx, auth, refs)
	if err != nil {
		return nil, err
	}
	out := make([]interaction.RequestView, len(views))
	for i, view := range views {
		out[i] = interaction.RequestView{Request: view.Request, AnswerSchema: view.AnswerSchema, Method: "task.input"}
	}
	return out, nil
}

var _ interaction.RequestBatchPort = requestBridge{}
