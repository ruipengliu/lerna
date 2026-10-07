package assembly

import "github.com/ruipengliu/lerna/core/durable"

// complete 只检查模块及其必需链接；不得读取业务历史或推进任何负责方。
func (h *Harness) complete() error {
	if err := durable.RequireDependencies("assembly",
		durable.Dependency{Name: "store", Value: h.store},
		durable.Dependency{Name: "bodies", Value: h.Bodies},
		durable.Dependency{Name: "durable", Value: h.Durable},
		durable.Dependency{Name: "contentWork", Value: h.contentWork},
		durable.Dependency{Name: "ledgerWork", Value: h.LedgerWork},
		durable.Dependency{Name: "traceWork", Value: h.traceWork},
		durable.Dependency{Name: "tasks", Value: h.Tasks},
		durable.Dependency{Name: "content", Value: h.Content},
		durable.Dependency{Name: "sessions", Value: h.Sessions},
		durable.Dependency{Name: "grants", Value: h.Grants},
		durable.Dependency{Name: "budget", Value: h.Budget},
		durable.Dependency{Name: "ledger", Value: h.Ledger},
		durable.Dependency{Name: "trace", Value: h.Trace},
		durable.Dependency{Name: "egress", Value: h.Egress},
		durable.Dependency{Name: "recovery", Value: h.Recovery},
	); err != nil {
		return err
	}
	for _, owner := range []interface{ ValidateDependencies() error }{
		h.Durable, h.contentWork, h.LedgerWork, h.traceWork,
		h.Tasks, h.Content, h.Sessions, h.Grants, h.Budget, h.Ledger, h.Trace, h.Egress, h.Recovery,
	} {
		if err := owner.ValidateDependencies(); err != nil {
			return err
		}
	}
	return nil
}
