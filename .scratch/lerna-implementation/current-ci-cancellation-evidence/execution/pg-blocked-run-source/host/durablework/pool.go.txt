package durablework

import demo "github.com/ruipengliu/lerna/internal/durableworkdemo"

type PoolWorker = demo.PoolWorker
type Dispatch = demo.Dispatch

func NewPoolWorker(host *Host, workers []*Worker) (*PoolWorker, error) {
	return demo.NewPoolWorker(host, workers)
}
