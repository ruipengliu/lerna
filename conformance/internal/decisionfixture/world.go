package decisionfixture

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	decisionpg "github.com/ruipengliu/lerna/adapters/postgres/decision_engine"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	process "github.com/ruipengliu/lerna/conformance/internal/testkit/process"
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
	peers         []*decisionpg.Store
	decisionOwned bool
	children      []*process.Child
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

// OpenPeer retains every successful additional writer until confirmed Close.
// Cleanup will not drop the owned schema while a peer's exit is unconfirmed.
func (w *World) OpenPeer(ctx context.Context) (*decisionpg.Store, error) {
	if w.closing {
		return nil, errors.New("fixture is closing")
	}
	peer, err := decisionpg.Open(ctx, w.decisionCfg)
	if peer != nil {
		w.peers = append(w.peers, peer)
	}
	return peer, err
}
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
	if err := joinChildren(ctx, w.children); err != nil {
		w.t.Fatal(err)
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
	joined, childErr := stopChildren(w.children)
	if !joined {
		return childErr
	}
	var errs []error
	errs = append(errs, childErr)
	for len(w.peers) > 0 {
		if err := w.peers[0].Close(); err != nil {
			return errors.Join(errors.Join(errs...), err)
		}
		w.peers = w.peers[1:]
	}
	if w.store != nil {
		if err := w.store.Close(); err != nil {
			return errors.Join(errors.Join(errs...), err)
		}
		w.store = nil
	}
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

// AdditionalScenario grants another exact fixture Decision while preserving the
// original immutable materials and component lock. It creates no real Task.
func (w *World) AdditionalScenario(ctx context.Context, ownerID, id v.ID, deadline time.Time) Scenario {
	w.t.Helper()
	scene := w.Scenario()
	permission, err := w.source.Authorize(ctx, scene.Subject, scene.DecisionRef, "start", &scene.Request.Payload)
	if err != nil {
		w.t.Fatal(err)
	}
	snapshot, err := w.source.ReadSnapshot(ctx, scene.Request.Payload.SnapshotRef, permission, v.MaxBodyBytes)
	if err != nil {
		w.t.Fatal(err)
	}
	material, err := w.source.ReadMaterial(ctx, scene.MaterialRef, "rule.input", permission, v.MaxBodyBytes)
	if err != nil {
		w.t.Fatal(err)
	}
	scene.DecisionRef.OwnerID = ownerID
	scene.DecisionRef.ID = id
	permission.DecisionOwner.OwnerID = ownerID
	if _, err = w.source.Seed(ctx, Bundle{DecisionRef: scene.DecisionRef, Permission: permission, Snapshot: snapshot, Materials: []Material{{Ref: scene.MaterialRef, Bytes: material}}, Purposes: []string{"decide", "get", "command.get", "start", "material", "rule.input", "fixture.lock", "publish", "proposal.publish", "artifact.publish"}, RuleVersion: permission.RuleVersion, ChargeBasis: permission.ChargeBasis, RuleStartCharge: permission.RuleStartCharge}); err != nil {
		w.t.Fatal(err)
	}
	scene.Request.CommandID = v.ID("command-" + string(id))
	scene.Request.Target = scene.DecisionRef
	scene.Request.Payload.DecisionID = id
	scene.Request.Payload.Deadline = v.Time(deadline.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))
	commandQuery, err := v.DecodeCommand(scene.CommandGetJSON)
	if err != nil {
		w.t.Fatal(err)
	}
	commandQuery.Target.OwnerID = ownerID
	commandQuery.Target.ID = scene.Request.CommandID
	commandQuery.Payload.CommandRef.Owner.OwnerID = ownerID
	commandQuery.Payload.CommandRef.CommandID = scene.Request.CommandID
	scene.CommandGetJSON, err = v.Encode(commandQuery)
	if err != nil {
		w.t.Fatal(err)
	}
	scene.GetJSON, err = v.Encode(v.DecisionGetRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: "read-fixture-decision", Target: scene.DecisionRef, Method: "decision_engine.get", AcceptBefore: scene.Request.AcceptBefore, Payload: v.DecisionGetPayload{DecisionRef: scene.DecisionRef}})
	if err != nil {
		w.t.Fatal(err)
	}
	return scene
}
func (w *World) ServiceFor(owner v.OwnerRef, worker string, lease time.Duration) *decision.Service {
	w.t.Helper()
	service, err := decision.New(decision.Config{Owner: owner, Store: w.store, Authority: w.source, Source: w.source, Publisher: w.source, Component: w.scenario.Request.Payload.ComponentRef, Worker: worker, Lease: lease, PoolControl: true})
	if err != nil {
		w.t.Fatal(err)
	}
	return service
}
