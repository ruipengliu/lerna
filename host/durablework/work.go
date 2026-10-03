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
// All workers for that owner use this clock; default PostgreSQL time is provided
// by the adapter. Tests may inject one shared trusted controllable clock.
func NewWorker(owner contract.OwnerRef, runner runtime.TxRunner, claims runtime.ClaimStore, repository demo.WorkRepository, clock runtime.Clock) *Worker {
	return &demo.Worker{Owner: owner, Runner: runner, Claims: claims, Repository: repository, Clock: clock}
}
func Project(work Work) Projection { return demo.Project(work) }
