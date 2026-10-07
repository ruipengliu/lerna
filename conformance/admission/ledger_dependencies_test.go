package admission_test

import (
	"context"
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// declaredLedgerStore 只暴露消费模块声明的 Store，不能透传隐藏能力。
type declaredLedgerStore struct{ ledger.Store }

// declaredExecutionWork 限制测试 Adapter 只暴露执行管理声明的完整工作接口。
type declaredExecutionWork struct{ ledger.ExecutionWork }

var _ ledger.ExecutionWork = (*durable.Service)(nil)

type acceptedStartFacts struct {
	ledger.StartFacts
	admission *v1.Admission
}

func (f acceptedStartFacts) QueryAdmission(context.Context, *v1.Caller, *v1.Ref) (*v1.Admission, error) {
	return f.admission, nil
}

var _ ledger.Store = (*sqlite.Store)(nil)
var _ ledger.Store = declaredLedgerStore{}

// 规则：R6、R7、G1、G4、准入-1
func TestDeclaredLedgerStoreAcceptsOriginalHandoffWithoutHiddenCapability(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	proposal := f.propose(t, nil)
	receipt, err := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant})
	accepted(t, receipt, err)
	admission, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, receipt.ResultRef)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(f.path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	owner, err := ledger.New(declaredLedgerStore{Store: store}, "u", "d/ledger", "d")
	if err != nil {
		t.Fatal(err)
	}
	owner.WithStarts(acceptedStartFacts{admission: admission})
	caller := &v1.Caller{UserId: "u", IssuerId: "tasks-handoff"}
	command := &v1.AcceptOperationCommand{Header: &v1.CommandHeader{Identity: admission.HandoffIdentity, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}, Admission: admission}
	original, err := owner.Accept(f.ctx, caller, command)
	accepted(t, original, err)
	replayed, err := owner.Accept(f.ctx, caller, command)
	if err != nil || !proto.Equal(original, replayed) {
		t.Fatalf("original handoff receipt changed: %v %v", replayed, err)
	}
	operation, err := owner.QueryOperation(f.ctx, f.caller, admission.OperationId)
	if err != nil || operation.GetLifecycle() != "ACCEPTED" || operation.GetEffect().GetOutcome() != "NOT_APPLIED" || operation.GetExecution() != nil {
		t.Fatalf("accepted operation: %v %v", operation, err)
	}
	jobs, err := owner.QueryJobs(f.ctx, f.caller, admission.OperationId)
	if err != nil || len(jobs) != 1 || jobs[0].State != "READY" || !proto.Equal(jobs[0].Responsibility, admission.HandoffIdentity) {
		t.Fatalf("original execution responsibility: %v %v", jobs, err)
	}
	if f.calls.Load() != 0 {
		t.Fatal("handoff performed physical IO")
	}
}
