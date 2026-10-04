package decisionfixture

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	decisionpg "github.com/ruipengliu/lerna/adapters/postgres/decision_engine"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

// World assembles two real PG owners. Each writer generation is an actual
// adapter instance; administrative ownership remains with the original handle.
type World struct {
	*SourceWorld
	decisionCfg   postgres.Config
	admin, store  *decisionpg.Store
	decisionOwned bool
}

func NewWorld(t *testing.T, ctx context.Context) *World {
	t.Helper()
	w := &World{}
	var err error
	defer func() {
		if err != nil {
			err = errors.Join(err, w.Cleanup())
			t.Fatal(err)
		}
	}()
	w.SourceWorld, err = createSourceWorld(t, ctx)
	if err != nil {
		return w
	}
	w.decisionCfg, err = ownedConfig(w.SourceWorld.cfg.DSN)
	if err != nil {
		return w
	}
	w.admin, err = decisionpg.Open(ctx, w.decisionCfg)
	if err != nil {
		return w
	}
	if err = w.admin.CreateSchema(ctx); err != nil {
		return w
	}
	w.decisionOwned = true
	if err = RegisterOwnedSchema(w.decisionCfg.Schema); err != nil {
		return w
	}
	w.store, err = decisionpg.Open(ctx, w.decisionCfg)
	if err != nil {
		return w
	}
	if err = w.store.Migrate(ctx); err != nil {
		return w
	}
	owner := w.scenario.DecisionRef
	service, buildErr := w.buildService()
	if buildErr != nil {
		err = buildErr
		return w
	}
	err = service.InstallPool(ctx, workpool.Default("fixture-decision-pool", []contract.OwnerRef{{TenantID: contract.ID(owner.TenantID), OwnerID: contract.ID(owner.OwnerID)}}), 0)
	if err != nil {
		return w
	}
	t.Cleanup(func() {
		if err := w.Cleanup(); err != nil {
			t.Error(err)
		}
	})
	return w
}
func (w *World) Store() *decisionpg.Store { return w.store }
func (w *World) Config() postgres.Config  { return w.decisionCfg }
func (w *World) Service() *decision.Service {
	w.t.Helper()
	service, err := w.buildService()
	if err != nil {
		w.t.Fatal(err)
	}
	return service
}
func (w *World) buildService() (*decision.Service, error) {
	scene := w.scenario
	return decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: w.store, Authority: w.source, Source: w.source, Publisher: w.source, Component: scene.Request.Payload.ComponentRef, Worker: "fixture-worker", Lease: 5 * time.Second, PoolControl: true})
}
func (w *World) Reopen(ctx context.Context) {
	w.t.Helper()
	if w.closing {
		w.t.Fatal("fixture is closing")
	}
	if err := w.store.Close(); err != nil {
		w.t.Fatal(err)
	}
	w.store = nil
	w.SourceWorld.Reopen(ctx)
	var err error
	w.store, err = decisionpg.Open(ctx, w.decisionCfg)
	if err != nil {
		w.t.Fatal(err)
	}
}
func (w *World) ReadArtifact(ctx context.Context, ref v.ContentRef) ([]byte, error) {
	scene := w.scenario
	permission, err := w.source.Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		return nil, err
	}
	return w.source.ReadPublished(ctx, ref, permission)
}
func (w *World) Cleanup() error {
	if w.SourceWorld != nil {
		w.closing = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if w.store != nil {
		if err := w.store.Close(); err != nil {
			return err
		}
		w.store = nil
	}
	var errs []error
	if w.decisionOwned {
		if err := w.admin.DropTestSchema(ctx); err != nil {
			errs = append(errs, err)
		} else {
			w.decisionOwned = false
		}
	}
	if w.admin != nil && !w.decisionOwned {
		if err := w.admin.Close(); err != nil {
			errs = append(errs, err)
		} else {
			w.admin = nil
		}
	}
	if w.SourceWorld != nil {
		errs = append(errs, w.SourceWorld.Cleanup())
	}
	return errors.Join(errs...)
}
