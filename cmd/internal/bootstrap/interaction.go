package bootstrap

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/runtime"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	if e := d.a.Identity.CheckCurrent(ctx, auth); e != nil {
		return api.Receipt{}, e
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

type calendarBridge struct{ c Config }

func (c calendarBridge) Load(zone, version string) (*time.Location, error) {
	if version != c.c.TZDBVersion || strings.Contains(zone, "..") || filepath.IsAbs(zone) {
		return nil, api.E("dependency_unavailable", "tzdb_unavailable")
	}
	manifest, e := os.ReadFile(filepath.Join(c.c.TZDBRoot, "tzdata.zi"))
	if e != nil || !strings.HasPrefix(string(manifest), "# version "+version+"\n") {
		return nil, api.E("dependency_unavailable", "tzdb_unavailable")
	}
	b, e := os.ReadFile(filepath.Join(c.c.TZDBRoot, zone))
	if e != nil {
		return nil, api.E("dependency_unavailable", "tzdb_unavailable")
	}
	location, e := time.LoadLocationFromTZData(zone, b)
	if e != nil {
		return nil, api.E("dependency_unavailable", "tzdb_unavailable")
	}
	return location, nil
}
