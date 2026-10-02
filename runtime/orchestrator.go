package runtime

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
	"github.com/ruipengliu/lerna/internal/host"
	"github.com/ruipengliu/lerna/internal/storage/postgres"
	"github.com/ruipengliu/lerna/internal/storage/sqlite"
)

type OrchestratorConfig struct {
	TenantID, OrchestratorID, ServiceID      string
	Driver, Database                         string
	Connections, Capacity                    int
	ProviderConcurrency, ResourceConcurrency int
	Notifications                            bool
	OnError                                  func(error)
}

// OrchestratorRuntime owns an explicitly migrated original ledger. Opening it
// never runs migrations. Run completes domain recovery before new admission.
type OrchestratorRuntime struct {
	api.Orchestrator
	service    *host.TaskService
	closeStore func() error
	running    atomic.Bool
	exited     atomic.Bool
}

func OpenOrchestrator(ctx context.Context, config OrchestratorConfig, ports api.OrchestratorPorts) (*OrchestratorRuntime, error) {
	scope := durable.Scope{TenantID: config.TenantID, OwnerID: config.OrchestratorID}
	if !scope.Valid() || config.ServiceID == "" {
		return nil, durable.ErrPrecondition
	}
	var service *host.TaskService
	var closeStore func() error
	var e error
	options := host.TaskServiceOptions{Scope: scope, ServiceID: config.ServiceID, Capacity: config.Capacity, ProviderConcurrency: config.ProviderConcurrency, ResourceConcurrency: config.ResourceConcurrency, OnError: config.OnError}
	switch config.Driver {
	case "postgres":
		connections := config.Connections
		if connections == 0 {
			connections = 4
		}
		store, err := postgres.Open(ctx, config.Database, []durable.Scope{scope}, connections, config.Notifications)
		if err != nil {
			return nil, err
		}
		closeStore = store.Close
		service, e = host.NewTaskService(store, postgres.TaskQueries(), ports, options)
	case "sqlite":
		store, err := sqlite.Open(ctx, config.Database, []durable.Scope{scope})
		if err != nil {
			return nil, err
		}
		closeStore = store.Close
		service, e = host.NewTaskService(store, sqlite.TaskQueries(), ports, options)
	default:
		return nil, durable.ErrUnsupported
	}
	if e != nil {
		_ = closeStore()
		return nil, e
	}
	return &OrchestratorRuntime{Orchestrator: service, service: service, closeStore: closeStore}, nil
}
func (r *OrchestratorRuntime) Ready() bool { return r.service.Ready() }

func (r *OrchestratorRuntime) RegisterEvidenceDefect(ctx context.Context, caller api.Caller, d api.EvidenceDefect) error {
	return r.service.RegisterEvidenceDefect(ctx, caller, d)
}
func (r *OrchestratorRuntime) Run(ctx context.Context) error {
	if !r.running.CompareAndSwap(false, true) {
		return durable.ErrPrecondition
	}
	e := r.service.Run(ctx)
	r.exited.Store(true)
	return e
}
func (r *OrchestratorRuntime) Close(ctx context.Context) error {
	if r.running.Load() {
		if !r.exited.Load() {
			return errors.New("orchestrator runner has not stopped")
		}
		if e := r.service.Wait(ctx); e != nil {
			return e
		}
	}
	return r.closeStore()
}

// ReconcileBilling re-reads the original owner's trusted cumulative bill. It
// accepts no notification amount and does not reset automatic query budgets.
func (r *OrchestratorRuntime) ReconcileBilling(ctx context.Context, caller api.Caller, q api.BillingQuery) error {
	return r.service.ReconcileBilling(ctx, caller, q)
}
