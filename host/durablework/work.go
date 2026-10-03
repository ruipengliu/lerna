package durablework

import (
	"github.com/ruipengliu/lerna/contract"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

type Worker = demo.Worker
type Work = demo.Work
type Projection = demo.Projection

// NewWorker explicitly assembles the same owner/database and its trusted clock.
// All workers for that owner use this clock; PostgreSQL provides database time
// and SQLite provides the trusted device Host clock. Tests may inject one shared trusted controllable clock.
func NewWorker(owner contract.OwnerRef, runner runtime.TxRunner, claims runtime.ClaimStore, repository demo.WorkRepository, clock runtime.Clock) *Worker {
	return &demo.Worker{Owner: owner, Runner: runner, Claims: claims, Repository: repository, Clock: clock}
}
func Project(work Work) Projection { return demo.Project(work) }

// NewScheduledWorker assembles processing with current trusted worker eligibility.
func NewScheduledWorker(owner contract.OwnerRef, runner runtime.TxRunner, claims runtime.ClaimStore, repository demo.WorkRepository, clock runtime.Clock, permissions *demo.WorkerPermissions) (*Worker, error) {
	if permissions == nil || runner == nil || claims == nil || repository == nil || clock == nil {
		return nil, demo.ErrPolicy
	}
	if _, err := contract.Encode(owner); err != nil {
		return nil, demo.ErrPolicy
	}
	if _, ok := claims.(runtime.ScheduleStore); !ok {
		return nil, demo.ErrPolicy
	}
	if _, ok := repository.(demo.ScheduleRepository); !ok {
		return nil, demo.ErrPolicy
	}
	if _, ok := repository.(demo.PoolRepository); !ok {
		return nil, demo.ErrPoolMissing
	}
	worker := NewWorker(owner, runner, claims, repository, clock)
	worker.Permissions = permissions
	return worker, nil
}
