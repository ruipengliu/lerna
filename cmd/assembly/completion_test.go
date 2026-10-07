package assembly

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/budget"
	"github.com/ruipengliu/lerna/core/content"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/egress"
	"github.com/ruipengliu/lerna/core/grants"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/core/sessions"
	"github.com/ruipengliu/lerna/core/tasks"
	"github.com/ruipengliu/lerna/core/trace"
	"github.com/ruipengliu/lerna/infra/egressio"
	"github.com/ruipengliu/lerna/infra/hosting"
	"github.com/ruipengliu/lerna/infra/rules"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 只在批准的装配完成切面观察原 Store/Work 边界；所有调用转交真实实现。
type assemblyProbe struct {
	qualification, recovery map[string]int
	io                      int
}
type assemblyStore struct {
	*sqlite.Store
	probe *assemblyProbe
}
type assemblyWork struct {
	durable.Store
	probe  *assemblyProbe
	domain string
}

func (s assemblyStore) Transaction(ctx context.Context, point string, fn func(context.Context) error) error {
	s.probe.recovery[point]++
	return s.Store.Transaction(ctx, point, fn)
}
func (s assemblyWork) Transaction(ctx context.Context, point string, fn func(context.Context) error) error {
	s.probe.recovery[s.domain+":"+point]++
	return s.Store.Transaction(ctx, point, fn)
}
func (s assemblyWork) PendingJobs(ctx context.Context, user string) ([]*v1.Job, error) {
	s.probe.recovery[s.domain+":jobs"]++
	return s.Store.PendingJobs(ctx, user)
}
func (s assemblyStore) RecoveryModelCalls(ctx context.Context) ([]*v1.ModelCall, error) {
	s.probe.qualification["tasks.model"]++
	return s.Store.RecoveryModelCalls(ctx)
}
func (s assemblyStore) RecoveryReasonerDrivers(ctx context.Context) ([]*v1.ReasonerDriver, error) {
	s.probe.qualification["tasks.driver"]++
	return s.Store.RecoveryReasonerDrivers(ctx)
}
func (s assemblyStore) RecoveryOperations(ctx context.Context) ([]*v1.Operation, error) {
	s.probe.qualification["ledger"]++
	return s.Store.RecoveryOperations(ctx)
}
func (s assemblyStore) PendingContentRegistrations(ctx context.Context) ([]*v1.ContentRegistration, error) {
	s.probe.recovery["content.register"]++
	return s.Store.PendingContentRegistrations(ctx)
}
func (s assemblyStore) PendingObservations(ctx context.Context) ([]*v1.ObservationHandoff, error) {
	s.probe.recovery["content.observation"]++
	return s.Store.PendingObservations(ctx)
}
func (s assemblyStore) PendingGrantRevocations(ctx context.Context) ([]*v1.GrantRevocation, error) {
	s.probe.recovery["grants"]++
	return s.Store.PendingGrantRevocations(ctx)
}
func (s assemblyStore) AllReports(ctx context.Context) ([]*v1.ObservationReports, error) {
	s.probe.recovery["ledger.reports"]++
	return s.Store.AllReports(ctx)
}
func (s assemblyStore) AllTaskClosings(ctx context.Context) ([]*v1.TaskClosing, error) {
	s.probe.recovery["tasks.closing"]++
	return s.Store.AllTaskClosings(ctx)
}
func (s assemblyStore) AllReservations(ctx context.Context) ([]*v1.Reservation, error) {
	s.probe.recovery["budget.closure"]++
	return s.Store.AllReservations(ctx)
}
func (s assemblyStore) AllExecutionFollowups(ctx context.Context) ([]*v1.ExecutionFollowup, error) {
	s.probe.recovery["ledger.followup"]++
	return s.Store.AllExecutionFollowups(ctx)
}
func (s assemblyStore) AllSettlementFollowups(ctx context.Context) ([]*v1.SettlementFollowup, error) {
	s.probe.recovery["budget.followup"]++
	return s.Store.AllSettlementFollowups(ctx)
}
func (s assemblyStore) AllReasonerDrivers(ctx context.Context) ([]*v1.ReasonerDriver, error) {
	s.probe.recovery["tasks.driver"]++
	return s.Store.AllReasonerDrivers(ctx)
}
func (s assemblyStore) TraceSources(ctx context.Context) ([]*v1.TraceSourceRecord, error) {
	s.probe.recovery["trace"]++
	return s.Store.TraceSources(ctx)
}
func (s assemblyStore) OperationProgressHandoffs(ctx context.Context, id *v1.GlobalName) ([]*v1.OperationProgressHandoff, error) {
	s.probe.recovery["ledger.progress"]++
	return s.Store.OperationProgressHandoffs(ctx, id)
}

type assemblyIO struct {
	physicalIO
	probe *assemblyProbe
}

func (p assemblyIO) Preflight(ctx context.Context, d *v1.CallDescriptor) error {
	p.probe.io++
	return p.physicalIO.Preflight(ctx, d)
}
func (p assemblyIO) Perform(ctx context.Context, r *v1.PhysicalIORequest) (*v1.PhysicalIOResult, error) {
	p.probe.io++
	return p.physicalIO.Perform(ctx, r)
}
func (p assemblyIO) PerformChecked(ctx context.Context, r *v1.PhysicalIORequest, check func(context.Context) error) (*v1.PhysicalIOResult, error) {
	p.probe.io++
	return p.physicalIO.PerformChecked(ctx, r, check)
}

func newAssemblyFixture(t *testing.T) (*Harness, *assemblyProbe) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "assembly.db")
	actual, err := sqlite.Open(path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = actual.Close() })
	p := &assemblyProbe{qualification: map[string]int{}, recovery: map[string]int{}}
	store := assemblyStore{actual, p}
	h := &Harness{store: actual, Bodies: actual.BodyReceipts(), path: path, user: "u", domain: "d"}
	h.Durable, err = durable.New(assemblyWork{store, p, "adjudication"}, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	h.Tasks, err = tasks.New(store, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	h.contentWork, err = durable.New(assemblyWork{actual.ContentWork(), p, "content"}, "u", "d/content")
	if err != nil {
		t.Fatal(err)
	}
	h.Content, err = content.New(store, h.contentWork, "u", "d/content")
	if err != nil {
		t.Fatal(err)
	}
	h.Sessions, err = sessions.New(store, h.Durable, h.Tasks, h.Content, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	h.Grants, err = grants.New(store, h.Durable, "u", "d", "host")
	if err != nil {
		t.Fatal(err)
	}
	h.Budget, err = budget.New(store, h.Durable, "u", "d", "host")
	if err != nil {
		t.Fatal(err)
	}
	h.LedgerWork, err = durable.New(assemblyWork{actual.LedgerWork(), p, "ledger"}, "u", "d/ledger")
	if err != nil {
		t.Fatal(err)
	}
	h.Ledger, err = ledger.New(store, "u", "d/ledger", "d")
	if err != nil {
		t.Fatal(err)
	}
	h.traceWork, err = durable.New(assemblyWork{actual.TraceWork(), p, "trace"}, "u", "d/trace")
	if err != nil {
		t.Fatal(err)
	}
	h.Trace, err = trace.New(store, h.traceWork, h.Ledger, "u", "d/trace")
	if err != nil {
		t.Fatal(err)
	}
	critical, err := egressio.NewFileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	io := assemblyIO{physicalIO: physicalIO{files: egressio.NewFiles(nil, h.Ledger, h.Content), network: egressio.Router{}}, probe: p}
	h.Egress, err = egress.New(h.Tasks, h.Ledger, h.Content, io, critical)
	if err != nil {
		t.Fatal(err)
	}
	h.connect()
	if err := h.connectManualProgress(); err != nil {
		t.Fatal(err)
	}
	return h, p
}

// 规则：G1、G3、G4、G11、R6、V4
func TestAssemblyRejectsMissingTraceBeforeHistoricalQualificationAndRecovery(t *testing.T) {
	h, p := newAssemblyFixture(t)
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	ctx := context.Background()
	goal := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "u", IssuerId: "host", TargetDomainId: "d", CommandId: "original-ready"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "keep original READY responsibility"}
	original, err := h.Sessions.SubmitGoal(ctx, caller, goal)
	if err != nil {
		t.Fatal(err)
	}
	job, err := h.Durable.QueryJob(ctx, caller, original.JobRef.Name)
	if err != nil {
		t.Fatal(err)
	}
	clear(p.qualification)
	clear(p.recovery)
	p.io = 0
	h.Trace = nil
	var failure error
	func() {
		defer func() {
			if value := recover(); value != nil {
				failure = fmt.Errorf("startup panic: %v", value)
			}
		}()
		failure = h.start(ctx)
	}()
	if failure == nil || failure.Error() != "missing required dependency: assembly.trace" {
		t.Errorf("incomplete assembly: %v", failure)
	}
	if len(p.qualification) != 0 || len(p.recovery) != 0 || p.io != 0 {
		t.Fatalf("incomplete assembly reached business owners: qualification=%v recovery=%v io=%d", p.qualification, p.recovery, p.io)
	}
	saved, err := h.Durable.QueryReceipt(ctx, caller, goal.Identity)
	if err != nil || !proto.Equal(saved.GetReceipt(), original) {
		t.Fatalf("original receipt advanced: %v %v", saved, err)
	}
	current, err := h.Durable.QueryJob(ctx, caller, job.Ref.Name)
	if err != nil || !proto.Equal(current, job) || current.State != "READY" || current.ClaimEpoch != 0 {
		t.Fatalf("original responsibility advanced: %v %v", current, err)
	}
}

func assertAssemblyRefusesBeforeOwners(t *testing.T, h *Harness, p *assemblyProbe, want string) {
	t.Helper()
	clear(p.qualification)
	clear(p.recovery)
	p.io = 0
	if err := h.start(context.Background()); err == nil || err.Error() != "missing required dependency: "+want {
		t.Fatalf("incomplete assembly: %v", err)
	}
	if len(p.qualification) != 0 || len(p.recovery) != 0 || p.io != 0 {
		t.Fatalf("business boundary reached: qualification=%v recovery=%v io=%d", p.qualification, p.recovery, p.io)
	}
}

// 规则：G1、G3、G4、G11、R6、V4
func TestAssemblyRequiresEveryModuleAndDomainWorkBeforeOwners(t *testing.T) {
	for _, test := range []struct {
		name       string
		disconnect func(*Harness)
	}{
		{"store", func(h *Harness) { h.store = nil }},
		{"bodies", func(h *Harness) { h.Bodies = nil }},
		{"durable", func(h *Harness) { h.Durable = nil }},
		{"contentWork", func(h *Harness) { h.contentWork = nil }},
		{"ledgerWork", func(h *Harness) { h.LedgerWork = nil }},
		{"traceWork", func(h *Harness) { h.traceWork = nil }},
		{"tasks", func(h *Harness) { h.Tasks = nil }},
		{"content", func(h *Harness) { h.Content = nil }},
		{"sessions", func(h *Harness) { h.Sessions = nil }},
		{"grants", func(h *Harness) { h.Grants = nil }},
		{"budget", func(h *Harness) { h.Budget = nil }},
		{"ledger", func(h *Harness) { h.Ledger = nil }},
		{"trace", func(h *Harness) { h.Trace = nil }},
		{"egress", func(h *Harness) { h.Egress = nil }},
		{"recovery", func(h *Harness) { h.Recovery = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, p := newAssemblyFixture(t)
			test.disconnect(h)
			assertAssemblyRefusesBeforeOwners(t, h, p, "assembly."+test.name)
		})
	}
}

// 规则：G1、G3、G4、G11、R6、V4
func TestAssemblyRequiresRealCycleLinksBeforeHistoricalQualificationAndRecovery(t *testing.T) {
	for _, test := range []struct {
		name       string
		disconnect func(*Harness, bool)
	}{
		{"tasks.startExecution", func(h *Harness, typed bool) {
			if typed {
				h.Tasks.WithStart(h.Grants, h.Budget, (*ledger.Service)(nil))
			} else {
				h.Tasks.WithStart(h.Grants, h.Budget, nil)
			}
		}},
		{"tasks.closureSource", func(h *Harness, typed bool) {
			if typed {
				h.Tasks.WithClosureSource((*ledger.Service)(nil))
			} else {
				h.Tasks.WithClosureSource(nil)
			}
		}},
		{"tasks.modelLedger", func(h *Harness, typed bool) {
			if typed {
				h.Tasks.WithModelExecution((*ledger.Service)(nil), h.LedgerWork, h.Grants, h.Egress)
			} else {
				h.Tasks.WithModelExecution(nil, h.LedgerWork, h.Grants, h.Egress)
			}
		}},
		{"content.associations", func(h *Harness, typed bool) {
			if typed {
				h.Content.WithAssociations((*tasks.Service)(nil))
			} else {
				h.Content.WithAssociations(nil)
			}
		}},
		{"content.ledger", func(h *Harness, typed bool) {
			if typed {
				h.Content.WithObservations((*ledger.Service)(nil))
			} else {
				h.Content.WithObservations(nil)
			}
		}},
		{"sessions.operationFacts", func(h *Harness, typed bool) {
			if typed {
				h.Sessions.WithConfirmations((*tasks.Service)(nil), h.Grants)
			} else {
				h.Sessions.WithConfirmations(nil, h.Grants)
			}
		}},
		{"sessions.grantFacts", func(h *Harness, typed bool) {
			if typed {
				h.Sessions.WithConfirmations(h.Tasks, (*grants.Service)(nil))
			} else {
				h.Sessions.WithConfirmations(h.Tasks, nil)
			}
		}},
		{"grants.admissions", func(h *Harness, typed bool) {
			if typed {
				h.Grants.WithAdmissions((*tasks.Service)(nil))
			} else {
				h.Grants.WithAdmissions(nil)
			}
		}},
		{"grants.revocationExits", func(h *Harness, typed bool) {
			if typed {
				h.Grants.WithRevocationExits((*egress.Service)(nil))
			} else {
				h.Grants.WithRevocationExits(nil)
			}
		}},
		{"budget.usageSource", func(h *Harness, typed bool) {
			if typed {
				h.Budget.WithUsageSource((*ledger.Service)(nil))
			} else {
				h.Budget.WithUsageSource(nil)
			}
		}},
		{"budget.completionAuthority", func(h *Harness, typed bool) {
			if typed {
				h.Budget.WithCompletionAuthority((*tasks.Service)(nil))
			} else {
				h.Budget.WithCompletionAuthority(nil)
			}
		}},
		{"ledger.observations", func(h *Harness, typed bool) {
			if typed {
				h.Ledger.WithObservations((*content.Service)(nil))
			} else {
				h.Ledger.WithObservations(nil)
			}
		}},
		{"ledger.trace", func(h *Harness, typed bool) {
			if typed {
				h.Ledger.WithReports(h.Budget, h.Durable, (*trace.Service)(nil))
			} else {
				h.Ledger.WithReports(h.Budget, h.Durable, nil)
			}
		}},
		{"ledger.rules", func(h *Harness, typed bool) {
			if typed {
				h.Ledger.WithEvidenceRules((*rules.Fixed)(nil))
			} else {
				h.Ledger.WithEvidenceRules(nil)
			}
		}},
		{"ledger.starts", func(h *Harness, typed bool) {
			if typed {
				h.Ledger.WithStarts((*tasks.Service)(nil))
			} else {
				h.Ledger.WithStarts(nil)
			}
		}},
	} {
		for _, typed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/typed=%v", test.name, typed), func(t *testing.T) {
				h, p := newAssemblyFixture(t)
				test.disconnect(h, typed)
				assertAssemblyRefusesBeforeOwners(t, h, p, test.name)
			})
		}
	}
}

// 规则：G1、G3、G4、G11、R6、V4
func TestAssemblyRequiresManualHostConfigurationBeforeOwners(t *testing.T) {
	h, p := newAssemblyFixture(t)
	h.Recovery = &hosting.Service{}
	assertAssemblyRefusesBeforeOwners(t, h, p, "hosting.sessions")
}
