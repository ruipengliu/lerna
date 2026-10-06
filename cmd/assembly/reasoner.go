package assembly

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	port "github.com/ruipengliu/lerna/contracts/reasoner"
	"github.com/ruipengliu/lerna/defaults/reasoner"
)

// RunDefaultReasoner 装配默认实现；调用方及原请求的固定控制信息交由任务编排核验。
func (h *Harness) RunDefaultReasoner(ctx context.Context, caller *v1.Caller, run *v1.RunModelCallCommand) (*v1.ProposalOutcome, error) {
	return h.Tasks.RunReasoner(ctx, caller, run, &reasoner.Reasoner{Settings: run.GetPreparation().GetSettings()})
}

func defaultReasoner(settings *v1.ModelSettings) port.Reasoner {
	return &reasoner.Reasoner{Settings: settings}
}
