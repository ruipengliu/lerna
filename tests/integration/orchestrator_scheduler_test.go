package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
	"github.com/ruipengliu/lerna/internal/host"
	o "github.com/ruipengliu/lerna/internal/orchestrator"
	pg "github.com/ruipengliu/lerna/internal/storage/postgres"
	filedb "github.com/ruipengliu/lerna/internal/storage/sqlite"
)

type schedulerActions struct {
	taskActions
	providerPerTask, resourcePerTask bool
}

func (a schedulerActions) Prepare(ctx context.Context, caller api.Caller, task api.Task, action api.ActionInvoke) (api.ActionPreparation, error) {
	prepared, err := a.taskActions.Prepare(ctx, caller, task, action)
	// Distinct executors can still contend for one provider or resource.
	prepared.ExecutorID = o.ID("executor", task.TaskID)
	if a.providerPerTask {
		prepared.ProviderID = o.ID("provider", task.TaskID)
	}
	if a.resourcePerTask {
		prepared.ResourceID = o.ID("resource", task.TaskID)
	}
	return prepared, err
}

func TestOrchestratorDispatchConcurrency(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		for _, scenario := range []struct {
			name                             string
			providerPerTask, resourcePerTask bool
			recoverLegacyRoutes              bool
			blocked                          bool
		}{
			{name: "shared_provider", resourcePerTask: true, blocked: true},
			{name: "shared_resource", providerPerTask: true, blocked: true},
			{name: "independent", providerPerTask: true, resourcePerTask: true},
			{name: "recover_legacy_routes", recoverLegacyRoutes: true, blocked: true},
		} {
			t.Run(driver+"/"+scenario.name, func(t *testing.T) {
				s := newSuite(t, driver)
				f := newTaskFixture()
				f.mode = "act"
				ports := f.ports()
				ports.Actions = schedulerActions{taskActions{f}, scenario.providerPerTask, scenario.resourcePerTask}
				queries := filedb.TaskQueries()
				if driver == "postgres" {
					queries = pg.TaskQueries()
				}
				service, err := host.NewTaskService(s.store, queries, ports, host.TaskServiceOptions{Scope: taskScope, ServiceID: o.ID("service", "tasks"), ProviderConcurrency: 1, ResourceConcurrency: 1})
				if err != nil {
					t.Fatal(err)
				}
				if err = service.Recover(ctx()); err != nil {
					t.Fatal(err)
				}
				p := newTaskProbe(t, s, f)
				p.c.Ports.Actions = ports.Actions
				for n := 0; n < 2; n++ {
					submitTask(t, service, f, "10")
					p.handle(t, "decide")
					p.handle(t, "poll")
					p.handle(t, "poll")
				}
				first := p.claim(t, "dispatch", 10*time.Second)
				if scenario.recoverLegacyRoutes {
					// Simulate the old batch writer's mismatched route keys on the
					// original ledger, including an already leased job. Repair the
					// routing projection without replacing that claim's identity.
					_, err = s.raw.ExecContext(ctx(), s.placeholders(`UPDATE orchestrator_work_routes SET responsibility_key='legacy/' || responsibility_key WHERE tenant_id=$1 AND owner_id=$2 AND kind='dispatch'`), taskScope.TenantID, taskScope.OwnerID)
					if err != nil {
						t.Fatal(err)
					}
					if err = service.Recover(ctx()); err != nil {
						t.Fatal(err)
					}
				}
				claim := func() []durable.Claim {
					t.Helper()
					claims, result := p.e.Claim(ctx(), taskScope, "dispatch", durable.NewID("boot"), 1, 10*time.Second)
					mustCommit(t, result)
					return claims
				}
				second := claim()
				if !scenario.blocked {
					if len(second) != 1 || second[0].SourceRef() == first.SourceRef() {
						t.Fatalf("independent work did not keep its own capacity: %v", second)
					}
					return
				}
				if len(second) != 0 {
					t.Fatalf("shared capacity allowed a second dispatch while %s was leased", first.JobID())
				}
				mustCommit(t, p.e.Within(ctx(), taskScope, nil, func(tx *durable.Tx) error {
					_, err := tx.Finish(first, durable.Done())
					return err
				}))
				second = claim()
				if len(second) != 1 || second[0].SourceRef() == first.SourceRef() {
					t.Fatalf("capacity was not released after guarded completion: %v", second)
				}
			})
		}
	}
}
