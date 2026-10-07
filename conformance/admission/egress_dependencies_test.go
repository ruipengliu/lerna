package admission_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/egress"
	"github.com/ruipengliu/lerna/infra/egressio"
)

type declaredEgressStarts struct{ egress.Starts }
type declaredEgressLedger struct{ egress.Ledger }
type declaredEgressContent struct{ egress.Content }

func newDeclaredEgress(t *testing.T, f *fixture, physical egress.IO) *egress.Service {
	t.Helper()
	critical, err := egressio.NewFileLock(f.path)
	if err != nil {
		t.Fatal(err)
	}
	service, err := egress.New(declaredEgressStarts{Starts: f.h.Tasks}, declaredEgressLedger{Ledger: f.h.Ledger}, declaredEgressContent{Content: f.h.Content}, physical, critical)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type ordinaryOnlyIO struct{ calls int }

func (io *ordinaryOnlyIO) Perform(context.Context, *v1.PhysicalIORequest) (*v1.PhysicalIOResult, error) {
	io.calls++
	return nil, nil
}

// 规则：G4、G5、开始-5
func TestOptionalCheckedIOAbsenceCannotBypassManagedFileQualification(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	fileParameters(t, f, "", nil)
	configureFile(t, f, "READ", "managed://documents/report")
	admission, start := prepareStart(t, f)
	physical := &ordinaryOnlyIO{}
	service := newDeclaredEgress(t, f, physical)
	receipt, err := service.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	var failure *command.Failure
	if receipt == nil || receipt.Decision != v1.Decision_DECISION_ACCEPTED || !errors.As(err, &failure) || failure.Detail.GetCode() != "UNSUPPORTED_CAPABILITY" {
		t.Fatalf("optional FILE IO failure: %v %v", receipt, err)
	}
	if physical.calls != 0 || f.calls.Load() != 0 {
		t.Fatal("FILE used ordinary physical IO")
	}
	operation, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
	if err != nil || operation.GetExecution().GetSend().GetPhase() != "DISPATCH_POSSIBLE" || operation.GetEffect().GetOutcome() != "UNKNOWN" {
		t.Fatalf("original dispatch responsibility lost: %v %v", operation, err)
	}
}
