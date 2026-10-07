package assembly

import (
	"fmt"
	"testing"

	"github.com/ruipengliu/lerna/core/budget"
	"github.com/ruipengliu/lerna/core/content"
	"github.com/ruipengliu/lerna/core/grants"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/core/sessions"
	"github.com/ruipengliu/lerna/core/tasks"
	"github.com/ruipengliu/lerna/core/trace"
	"github.com/ruipengliu/lerna/infra/hosting"
)

// 仅复用批准的真实模块配置切面；完整业务行为仍由公开 Open 验收。
func startupConfiguration(h *Harness) hosting.StartupDependencies {
	return hosting.StartupDependencies{
		User: h.user, TasksCompatibility: h.Tasks, LedgerCompatibility: h.Ledger,
		Sessions: h.Sessions, Handoffs: h.Tasks, Registrations: h.Content,
		Observations: h.Content, Reports: h.Ledger, Revocations: h.Grants,
		Cancellations: h.Tasks, Completions: h.Tasks, Reconciliations: h.Ledger,
		TaskClosures: h.Tasks, TaskClosings: h.Tasks, BudgetClosures: h.Budget,
		ExecutionFollowups: h.Ledger, SettlementFollowups: h.Budget,
		Drivers: h.Tasks, Trace: h.Trace,
	}
}

// 规则：G1、G3、G4、G11、R6、V4
func TestStartupConstructorRequiresEveryDeclaredPortBeforeOwners(t *testing.T) {
	for _, test := range []struct {
		name       string
		disconnect func(*hosting.StartupDependencies, bool)
	}{
		{"tasksCompatibility", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.TasksCompatibility = (*tasks.Service)(nil)
			} else {
				d.TasksCompatibility = nil
			}
		}},
		{"ledgerCompatibility", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.LedgerCompatibility = (*ledger.Service)(nil)
			} else {
				d.LedgerCompatibility = nil
			}
		}},
		{"sessions", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.Sessions = (*sessions.Service)(nil)
			} else {
				d.Sessions = nil
			}
		}},
		{"handoffs", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.Handoffs = (*tasks.Service)(nil)
			} else {
				d.Handoffs = nil
			}
		}},
		{"registrations", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.Registrations = (*content.Service)(nil)
			} else {
				d.Registrations = nil
			}
		}},
		{"observations", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.Observations = (*content.Service)(nil)
			} else {
				d.Observations = nil
			}
		}},
		{"reports", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.Reports = (*ledger.Service)(nil)
			} else {
				d.Reports = nil
			}
		}},
		{"revocations", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.Revocations = (*grants.Service)(nil)
			} else {
				d.Revocations = nil
			}
		}},
		{"cancellations", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.Cancellations = (*tasks.Service)(nil)
			} else {
				d.Cancellations = nil
			}
		}},
		{"completions", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.Completions = (*tasks.Service)(nil)
			} else {
				d.Completions = nil
			}
		}},
		{"reconciliations", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.Reconciliations = (*ledger.Service)(nil)
			} else {
				d.Reconciliations = nil
			}
		}},
		{"taskClosures", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.TaskClosures = (*tasks.Service)(nil)
			} else {
				d.TaskClosures = nil
			}
		}},
		{"taskClosings", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.TaskClosings = (*tasks.Service)(nil)
			} else {
				d.TaskClosings = nil
			}
		}},
		{"budgetClosures", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.BudgetClosures = (*budget.Service)(nil)
			} else {
				d.BudgetClosures = nil
			}
		}},
		{"executionFollowups", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.ExecutionFollowups = (*ledger.Service)(nil)
			} else {
				d.ExecutionFollowups = nil
			}
		}},
		{"settlementFollowups", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.SettlementFollowups = (*budget.Service)(nil)
			} else {
				d.SettlementFollowups = nil
			}
		}},
		{"drivers", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.Drivers = (*tasks.Service)(nil)
			} else {
				d.Drivers = nil
			}
		}},
		{"trace", func(d *hosting.StartupDependencies, typed bool) {
			if typed {
				d.Trace = (*trace.Service)(nil)
			} else {
				d.Trace = nil
			}
		}},
	} {
		for _, typed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/typed=%v", test.name, typed), func(t *testing.T) {
				h, p := newAssemblyFixture(t)
				d := startupConfiguration(h)
				test.disconnect(&d, typed)
				host, err := hosting.New(hosting.ManualDependencies{Sessions: h.Sessions}, d)
				if host != nil || err == nil || err.Error() != "missing required dependency: hosting.startup."+test.name {
					t.Fatalf("incomplete startup constructor: %v %v", host, err)
				}
				if len(p.qualification) != 0 || len(p.recovery) != 0 || p.io != 0 {
					t.Fatalf("constructor reached owners: qualification=%v recovery=%v io=%d", p.qualification, p.recovery, p.io)
				}
			})
		}
	}
	t.Run("user", func(t *testing.T) {
		h, p := newAssemblyFixture(t)
		d := startupConfiguration(h)
		d.User = ""
		host, err := hosting.New(hosting.ManualDependencies{Sessions: h.Sessions}, d)
		if host != nil || err == nil || err.Error() != "missing required dependency: hosting.startup.user" {
			t.Fatalf("missing fixed startup user: %v %v", host, err)
		}
		if len(p.qualification) != 0 || len(p.recovery) != 0 || p.io != 0 {
			t.Fatal("missing user reached owners")
		}
	})
}
