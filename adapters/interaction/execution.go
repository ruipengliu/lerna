package interaction

import (
	"context"
	"os"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type ExecutionLedger interface {
	QueryOperation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Operation, error)
	QueryObservation(context.Context, *v1.Caller, *v1.Ref) (*v1.RawObservation, error)
	ProcessReports(context.Context, *v1.Caller) error
	ProcessInterpretations(context.Context, *v1.Caller) error
}
type Egress interface {
	Invoke(context.Context, *v1.Caller, *v1.StartExecutionCommand) (*v1.CommandReceipt, error)
}
type ObservationContent interface {
	ProcessObservations(context.Context, *v1.Caller) error
}

func (c CLI) execution(ctx context.Context, args []string) (proto.Message, error) {
	if len(args) != 2 {
		return nil, command.Fail("INVALID_INPUT")
	}
	switch args[0] {
	case "execute":
		if c.Egress == nil {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		b, e := os.ReadFile(args[1])
		if e != nil {
			return nil, e
		}
		request := new(v1.StartExecutionCommand)
		if e = protojson.Unmarshal(b, request); e != nil {
			return nil, e
		}
		return c.Egress.Invoke(ctx, c.Caller, request)
	case "operation":
		if c.Ledger == nil {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		return c.Ledger.QueryOperation(ctx, c.Caller, &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain + "/ledger", ObjectKind: "operation", LocalId: args[1]})
	case "observation":
		if c.Ledger == nil {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		return c.Ledger.QueryObservation(ctx, c.Caller, &v1.Ref{Name: &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain + "/ledger", ObjectKind: "observation", LocalId: args[1]}, Revision: 1, SchemaId: "lerna.v1.RawObservation"})
	}
	return nil, command.Fail("UNSUPPORTED_FEATURE")
}
