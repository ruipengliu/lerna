package interaction

import (
	"context"
	"flag"
	"io"
	"os"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type ReconciliationLedger interface {
	RequestReconciliation(context.Context, *v1.Caller, *v1.RequestReconciliationCommand) (*v1.CommandReceipt, error)
	ControlReconciliation(context.Context, *v1.Caller, *v1.ControlReconciliationCommand) (*v1.CommandReceipt, error)
	QueryReconciliation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Reconciliation, error)
	QueryReconciliationQuery(context.Context, *v1.Caller, *v1.Ref) (*v1.ReconciliationQuery, error)
	QueryReconciliationFinding(context.Context, *v1.Caller, *v1.Ref) (*v1.ReconciliationFinding, error)
	RecoverReconciliations(context.Context, *v1.Caller) error
	ProcessOperationProgress(context.Context, *v1.Caller) error
}

func (c CLI) reconciliationCommand(ctx context.Context, args []string) (proto.Message, error) {
	ledger, ok := c.Ledger.(ReconciliationLedger)
	if !ok {
		return nil, command.Fail("UNSUPPORTED_FEATURE")
	}
	if args[0] == "reconciliation" {
		if len(args) != 2 {
			return nil, command.Fail("INVALID_INPUT")
		}
		return ledger.QueryReconciliation(ctx, c.Caller, &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain + "/ledger", ObjectKind: "operation", LocalId: args[1]})
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	path := f.String("json", "", "public command or reference JSON file")
	if e := f.Parse(args[1:]); e != nil {
		return nil, e
	}
	if *path == "" || f.NArg() != 0 {
		return nil, command.Fail("INVALID_INPUT")
	}
	b, e := os.ReadFile(*path)
	if e != nil {
		return nil, e
	}
	switch args[0] {
	case "closure-confirmation":
		tasks, ok := c.Tasks.(interface {
			RequestClosureConfirmation(context.Context, *v1.Caller, *v1.RequestClosureConfirmationCommand) (*v1.CommandReceipt, error)
		})
		if !ok {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		cmd := new(v1.RequestClosureConfirmationCommand)
		if e = protojson.Unmarshal(b, cmd); e != nil {
			return nil, e
		}
		return tasks.RequestClosureConfirmation(ctx, c.Caller, cmd)
	case "request-reconciliation":
		cmd := new(v1.RequestReconciliationCommand)
		if e = protojson.Unmarshal(b, cmd); e != nil {
			return nil, e
		}
		return ledger.RequestReconciliation(ctx, c.Caller, cmd)
	case "control-reconciliation":
		cmd := new(v1.ControlReconciliationCommand)
		if e = protojson.Unmarshal(b, cmd); e != nil {
			return nil, e
		}
		return ledger.ControlReconciliation(ctx, c.Caller, cmd)
	case "reconciliation-query", "reconciliation-finding":
		ref := new(v1.Ref)
		if e = protojson.Unmarshal(b, ref); e != nil {
			return nil, e
		}
		if args[0] == "reconciliation-query" {
			return ledger.QueryReconciliationQuery(ctx, c.Caller, ref)
		}
		return ledger.QueryReconciliationFinding(ctx, c.Caller, ref)
	}
	return nil, command.Fail("UNSUPPORTED_FEATURE")
}
