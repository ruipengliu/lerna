package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/contracts/reasoner"
	"github.com/ruipengliu/lerna/core/budget"
	"github.com/ruipengliu/lerna/core/content"
	"github.com/ruipengliu/lerna/core/egress"
	"github.com/ruipengliu/lerna/core/grants"
	"github.com/ruipengliu/lerna/core/ledger"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/sessions"
	"github.com/ruipengliu/lerna/core/tasks"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 只暴露消费方声明的能力，不能提升 SQLite 的隐藏方法。
type declaredTasksStore struct{ tasks.Store }
type declaredTasksDecisions struct{ tasks.Decisions }
type declaredTaskStartGrants struct{ tasks.StartGrants }
type declaredTaskStartBudget struct{ tasks.StartBudget }
type declaredTaskStartExecution struct{ tasks.StartExecutionFacts }

var _ tasks.Store = (*sqlite.Store)(nil)
var _ tasks.Store = declaredTasksStore{}
var _ tasks.Decisions = declaredTasksDecisions{}
var _ tasks.Decisions = (*durable.Service)(nil)

// 规则：R6、G3
func TestTasksConstructorRejectsMissingStore(t *testing.T) {
	for _, store := range []tasks.Store{nil, (*declaredTasksStore)(nil)} {
		owner, err := tasks.New(store, "u", "d")
		if owner != nil || err == nil || err.Error() != "missing required dependency: tasks.store" {
			t.Fatalf("construction: %v %v", owner, err)
		}
	}
}

// 规则：R6、G3、G4、G11
func TestDeclaredTasksStoreCreatesOriginalProposalResponsibilityAndHistory(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	store, err := sqlite.Open(f.path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	decisions, err := durable.New(store, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := tasks.New(declaredTasksStore{store}, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	owner.WithDecisions(decisions).
		WithModelContent(f.h.Content).WithModelExecution(f.h.Ledger, f.h.LedgerWork, f.h.Grants, f.h.Egress)
	confirmationFacts, err := sessions.New(store, decisions, owner, f.h.Content, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	owner.WithConditionConfirmations(confirmationFacts)
	cmd := &v1.RequestProposalCommand{Header: header("declared-tasks-request"), TaskId: f.task.Name}
	snapshot, err := owner.RequestProposal(f.ctx, f.caller, cmd)
	if err != nil {
		t.Fatal(err)
	}
	request, err := owner.QueryProposalRequest(f.ctx, f.caller, snapshot.RequestRef)
	if err != nil || request.State != "PENDING" || request.Purpose != "PLAN" {
		t.Fatalf("request: %v %v", request, err)
	}
	history, err := owner.QueryInputs(f.ctx, f.caller, f.task.Name)
	if err != nil || len(history.Inputs) != 1 || history.Inputs[0].InputVersion != 1 || history.Inputs[0].ProcessingStatus != "PROCESSED" {
		t.Fatalf("original input history: %v %v", history, err)
	}
	replay, err := owner.RequestProposal(f.ctx, f.caller, cmd)
	if err != nil || !proto.Equal(snapshot, replay) || f.calls.Load() != 0 {
		t.Fatalf("original snapshot: %v %v target=%d", replay, err, f.calls.Load())
	}
}

// 规则：R6、G3、G4、G11
func TestTasksCompleteAssemblyRejectsMissingAndTypedNilLinks(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	owner := f.h.Tasks
	factory := tasks.ReasonerFactory(func(*v1.ModelSettings) reasoner.Reasoner { return nil })
	for _, link := range []struct {
		name            string
		connectNil      func()
		connectTypedNil func()
		connect         func()
	}{
		{"decisions", func() { owner.WithDecisions(nil) }, func() { owner.WithDecisions((*declaredTasksDecisions)(nil)) }, func() { owner.WithDecisions(declaredTasksDecisions{f.h.Durable}) }},
		{"grants", func() { owner.WithAdmission(nil, f.h.Budget, f.h.Content, f.h.Sessions, f.h.Durable, f.h.Ledger) }, func() {
			owner.WithAdmission((*grants.Service)(nil), f.h.Budget, f.h.Content, f.h.Sessions, f.h.Durable, f.h.Ledger)
		}, func() {
			owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, f.h.Sessions, f.h.Durable, f.h.Ledger)
		}},
		{"budget", func() { owner.WithAdmission(f.h.Grants, nil, f.h.Content, f.h.Sessions, f.h.Durable, f.h.Ledger) }, func() {
			owner.WithAdmission(f.h.Grants, (*budget.Service)(nil), f.h.Content, f.h.Sessions, f.h.Durable, f.h.Ledger)
		}, func() {
			owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, f.h.Sessions, f.h.Durable, f.h.Ledger)
		}},
		{"content", func() { owner.WithAdmission(f.h.Grants, f.h.Budget, nil, f.h.Sessions, f.h.Durable, f.h.Ledger) }, func() {
			owner.WithAdmission(f.h.Grants, f.h.Budget, (*content.Service)(nil), f.h.Sessions, f.h.Durable, f.h.Ledger)
		}, func() {
			owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, f.h.Sessions, f.h.Durable, f.h.Ledger)
		}},
		{"confirmations", func() { owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, nil, f.h.Durable, f.h.Ledger) }, func() {
			owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, (*sessions.Service)(nil), f.h.Durable, f.h.Ledger)
		}, func() {
			owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, f.h.Sessions, f.h.Durable, f.h.Ledger)
		}},
		{"scheduling", func() { owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, f.h.Sessions, nil, f.h.Ledger) }, func() {
			owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, f.h.Sessions, (*durable.Service)(nil), f.h.Ledger)
		}, func() {
			owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, f.h.Sessions, f.h.Durable, f.h.Ledger)
		}},
		{"execution", func() { owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, f.h.Sessions, f.h.Durable, nil) }, func() {
			owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, f.h.Sessions, f.h.Durable, (*ledger.Service)(nil))
		}, func() {
			owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, f.h.Sessions, f.h.Durable, f.h.Ledger)
		}},
		{"handoffJobs", func() { owner.WithHandoffs(nil, f.h.Ledger) }, func() { owner.WithHandoffs((*durable.Service)(nil), f.h.Ledger) }, func() { owner.WithHandoffs(f.h.Durable, f.h.Ledger) }},
		{"recipient", func() { owner.WithHandoffs(f.h.Durable, nil) }, func() { owner.WithHandoffs(f.h.Durable, (*ledger.Service)(nil)) }, func() { owner.WithHandoffs(f.h.Durable, f.h.Ledger) }},
		{"startGrants", func() {
			owner.WithStart(nil, declaredTaskStartBudget{f.h.Budget}, declaredTaskStartExecution{f.h.Ledger})
		}, func() {
			owner.WithStart((*declaredTaskStartGrants)(nil), declaredTaskStartBudget{f.h.Budget}, declaredTaskStartExecution{f.h.Ledger})
		}, func() {
			owner.WithStart(declaredTaskStartGrants{f.h.Grants}, declaredTaskStartBudget{f.h.Budget}, declaredTaskStartExecution{f.h.Ledger})
		}},
		{"startBudget", func() {
			owner.WithStart(declaredTaskStartGrants{f.h.Grants}, nil, declaredTaskStartExecution{f.h.Ledger})
		}, func() {
			owner.WithStart(declaredTaskStartGrants{f.h.Grants}, (*declaredTaskStartBudget)(nil), declaredTaskStartExecution{f.h.Ledger})
		}, func() {
			owner.WithStart(declaredTaskStartGrants{f.h.Grants}, declaredTaskStartBudget{f.h.Budget}, declaredTaskStartExecution{f.h.Ledger})
		}},
		{"startExecution", func() { owner.WithStart(declaredTaskStartGrants{f.h.Grants}, declaredTaskStartBudget{f.h.Budget}, nil) }, func() {
			owner.WithStart(declaredTaskStartGrants{f.h.Grants}, declaredTaskStartBudget{f.h.Budget}, (*declaredTaskStartExecution)(nil))
		}, func() {
			owner.WithStart(declaredTaskStartGrants{f.h.Grants}, declaredTaskStartBudget{f.h.Budget}, declaredTaskStartExecution{f.h.Ledger})
		}},
		{"confirmationPublisher", func() { owner.WithConfirmationRequests(nil, f.h.Content) }, func() { owner.WithConfirmationRequests((*sessions.Service)(nil), f.h.Content) }, func() { owner.WithConfirmationRequests(f.h.Sessions, f.h.Content) }},
		{"confirmationContent", func() { owner.WithConfirmationRequests(f.h.Sessions, nil) }, func() { owner.WithConfirmationRequests(f.h.Sessions, (*content.Service)(nil)) }, func() { owner.WithConfirmationRequests(f.h.Sessions, f.h.Content) }},
		{"modelContent", func() { owner.WithModelContent(nil) }, func() { owner.WithModelContent((*content.Service)(nil)) }, func() { owner.WithModelContent(f.h.Content) }},
		{"modelLedger", func() { owner.WithModelExecution(nil, f.h.LedgerWork, f.h.Grants, f.h.Egress) }, func() { owner.WithModelExecution((*ledger.Service)(nil), f.h.LedgerWork, f.h.Grants, f.h.Egress) }, func() { owner.WithModelExecution(f.h.Ledger, f.h.LedgerWork, f.h.Grants, f.h.Egress) }},
		{"modelWork", func() { owner.WithModelExecution(f.h.Ledger, nil, f.h.Grants, f.h.Egress) }, func() { owner.WithModelExecution(f.h.Ledger, (*durable.Service)(nil), f.h.Grants, f.h.Egress) }, func() { owner.WithModelExecution(f.h.Ledger, f.h.LedgerWork, f.h.Grants, f.h.Egress) }},
		{"modelCredentials", func() { owner.WithModelExecution(f.h.Ledger, f.h.LedgerWork, nil, f.h.Egress) }, func() { owner.WithModelExecution(f.h.Ledger, f.h.LedgerWork, (*grants.Service)(nil), f.h.Egress) }, func() { owner.WithModelExecution(f.h.Ledger, f.h.LedgerWork, f.h.Grants, f.h.Egress) }},
		{"modelEgress", func() { owner.WithModelExecution(f.h.Ledger, f.h.LedgerWork, f.h.Grants, nil) }, func() { owner.WithModelExecution(f.h.Ledger, f.h.LedgerWork, f.h.Grants, (*egress.Service)(nil)) }, func() { owner.WithModelExecution(f.h.Ledger, f.h.LedgerWork, f.h.Grants, f.h.Egress) }},
		{"completionFacts", func() { owner.WithCompletion(nil, f.h.Budget) }, func() { owner.WithCompletion((*ledger.Service)(nil), f.h.Budget) }, func() { owner.WithCompletion(f.h.Ledger, f.h.Budget) }},
		{"completionBudget", func() { owner.WithCompletion(f.h.Ledger, nil) }, func() { owner.WithCompletion(f.h.Ledger, (*budget.Service)(nil)) }, func() { owner.WithCompletion(f.h.Ledger, f.h.Budget) }},
		{"completionJobs", func() { owner.WithCompletionClosures(nil, f.h.Egress) }, func() { owner.WithCompletionClosures((*durable.Service)(nil), f.h.Egress) }, func() { owner.WithCompletionClosures(f.h.Durable, f.h.Egress) }},
		{"completionCloser", func() { owner.WithCompletionClosures(f.h.Durable, nil) }, func() { owner.WithCompletionClosures(f.h.Durable, (*egress.Service)(nil)) }, func() { owner.WithCompletionClosures(f.h.Durable, f.h.Egress) }},
		{"cancellationJobs", func() { owner.WithCancellationClosures(nil, f.h.Egress) }, func() { owner.WithCancellationClosures((*durable.Service)(nil), f.h.Egress) }, func() { owner.WithCancellationClosures(f.h.Durable, f.h.Egress) }},
		{"cancellationCloser", func() { owner.WithCancellationClosures(f.h.Durable, nil) }, func() { owner.WithCancellationClosures(f.h.Durable, (*egress.Service)(nil)) }, func() { owner.WithCancellationClosures(f.h.Durable, f.h.Egress) }},
		{"taskClosingJobs", func() { owner.WithTaskClosures(nil, f.h.Egress) }, func() { owner.WithTaskClosures((*durable.Service)(nil), f.h.Egress) }, func() { owner.WithTaskClosures(f.h.Durable, f.h.Egress) }},
		{"taskCloser", func() { owner.WithTaskClosures(f.h.Durable, nil) }, func() { owner.WithTaskClosures(f.h.Durable, (*egress.Service)(nil)) }, func() { owner.WithTaskClosures(f.h.Durable, f.h.Egress) }},
		{"taskCloseFacts", func() { owner.WithTaskClosingFacts(nil, f.h.Budget) }, func() { owner.WithTaskClosingFacts((*ledger.Service)(nil), f.h.Budget) }, func() { owner.WithTaskClosingFacts(f.h.Ledger, f.h.Budget) }},
		{"taskCloseBudget", func() { owner.WithTaskClosingFacts(f.h.Ledger, nil) }, func() { owner.WithTaskClosingFacts(f.h.Ledger, (*budget.Service)(nil)) }, func() { owner.WithTaskClosingFacts(f.h.Ledger, f.h.Budget) }},
		{"closureSource", func() { owner.WithClosureSource(nil) }, func() { owner.WithClosureSource((*ledger.Service)(nil)) }, func() { owner.WithClosureSource(f.h.Ledger) }},
		{"progressSource", func() { owner.WithOperationProgress(nil) }, func() { owner.WithOperationProgress((*ledger.Service)(nil)) }, func() { owner.WithOperationProgress(f.h.Ledger) }},
		{"reasonerQuestions", func() { owner.WithReasonerQuestions(nil) }, func() { owner.WithReasonerQuestions((*sessions.Service)(nil)) }, func() { owner.WithReasonerQuestions(f.h.Sessions) }},
		{"conditionConfirmations", func() { owner.WithConditionConfirmations(nil) }, func() { owner.WithConditionConfirmations((*sessions.Service)(nil)) }, func() { owner.WithConditionConfirmations(f.h.Sessions) }},
		{"reasonerWork", func() { owner.WithReasonerDriver(nil, factory) }, func() { owner.WithReasonerDriver((*durable.Service)(nil), factory) }, func() { owner.WithReasonerDriver(f.h.Durable, factory) }},
		{"reasonerFactory", func() { owner.WithReasonerDriver(f.h.Durable, nil) }, func() { owner.WithReasonerDriver(f.h.Durable, tasks.ReasonerFactory(nil)) }, func() { owner.WithReasonerDriver(f.h.Durable, factory) }},
	} {
		t.Run(link.name, func(t *testing.T) {
			for _, connect := range []func(){link.connectNil, link.connectTypedNil} {
				connect()
				if err := owner.ValidateDependencies(); err == nil || err.Error() != "missing required dependency: tasks."+link.name {
					t.Fatalf("completion: %v", err)
				}
			}
			link.connect()
		})
	}
	if err := owner.ValidateDependencies(); err != nil {
		t.Fatalf("complete assembly: %v", err)
	}
}
