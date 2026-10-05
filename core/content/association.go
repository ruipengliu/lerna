package content

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type AssociationFacts interface {
	QueryTask(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Task, error)
	QueryAdmission(context.Context, *v1.Caller, *v1.Ref) (*v1.Admission, error)
}

func (s *Service) WithAssociations(f AssociationFacts) *Service { s.facts = f; return s }
func (s *Service) checkAssociation(ctx context.Context, caller *v1.Caller, task, operation, attempt *v1.GlobalName) error {
	if task != nil {
		t, e := s.facts.QueryTask(ctx, caller, task)
		if e != nil {
			return e
		}
		if t == nil {
			return command.Fail("INVALID_REFERENCE")
		}
	}
	if operation != nil {
		if task == nil {
			return command.Fail("INVALID_REFERENCE")
		}
		op, e := s.ledger.QueryOperation(ctx, caller, operation)
		if e != nil {
			return e
		}
		if op == nil {
			return command.Fail("INVALID_REFERENCE")
		}
		a, e := s.facts.QueryAdmission(ctx, caller, op.AdmissionRef)
		if e != nil {
			return e
		}
		if a == nil || !proto.Equal(a.TaskId, task) {
			return command.Fail("INVALID_REFERENCE")
		}
	}
	if attempt != nil {
		if operation == nil {
			return command.Fail("INVALID_REFERENCE")
		}
		x, e := s.ledger.QueryExecution(ctx, caller, operation)
		if e != nil {
			return e
		}
		if x == nil || !proto.Equal(x.Attempt.Ref.Name, attempt) {
			return command.Fail("INVALID_REFERENCE")
		}
	}
	return nil
}
