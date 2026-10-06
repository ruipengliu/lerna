package interaction

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type Trace interface {
	Query(context.Context, *v1.Caller, *v1.TraceQuery) (*v1.TraceView, error)
	Recover(context.Context, *v1.Caller) error
}

func (c CLI) trace(ctx context.Context, args []string) (*v1.TraceView, error) {
	if len(args) != 2 {
		return nil, command.Fail("INVALID_INPUT")
	}
	if c.Trace == nil {
		return nil, command.Fail("UNSUPPORTED_FEATURE")
	}
	name := &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain, ObjectKind: "task", LocalId: args[1]}
	q := &v1.TraceQuery{TaskId: name, ProcessingPurpose: "DIAGNOSTIC"}
	if args[0] == "trace-operation" {
		name.AuthorityDomainId += "/ledger"
		name.ObjectKind = "operation"
		q.TaskId = nil
		q.OperationId = name
	}
	return c.Trace.Query(ctx, c.Caller, q)
}
