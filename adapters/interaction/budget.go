package interaction

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Budget interface {
	Configure(context.Context, *v1.Caller, *v1.ConfigureBudgetCommand) (*v1.CommandReceipt, error)
	AdjustLimit(context.Context, *v1.Caller, *v1.AdjustBudgetLimitCommand) (*v1.CommandReceipt, error)
	ImportBill(context.Context, *v1.Caller, *v1.ImportBillCommand) (*v1.CommandReceipt, error)
	ReleaseUnused(context.Context, *v1.Caller, *v1.ReleaseReservationCommand) (*v1.CommandReceipt, error)
	QueryBudget(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Budget, error)
	QueryBudgetVersion(context.Context, *v1.Caller, *v1.Ref) (*v1.Budget, error)
	QueryBillingSource(context.Context, *v1.Caller, *v1.Ref) (*v1.BillingSource, error)
	QueryBillingEntry(context.Context, *v1.Caller, *v1.Ref) (*v1.BillingEntry, error)
	QueryBillingConflict(context.Context, *v1.Caller, *v1.Ref) (*v1.BillingConflict, error)
	ProcessClosures(context.Context) error
}

func (c CLI) budgetCommand(ctx context.Context, args []string) (proto.Message, error) {
	if c.Budget == nil {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	if args[0] == "budget" {
		if len(args) == 1 {
			return c.Budget.QueryBudget(ctx, c.Caller, nil)
		}
		if len(args) != 2 {
			return nil, command.Fail("INVALID_INPUT")
		}
		return c.Budget.QueryBudget(ctx, c.Caller, &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain, ObjectKind: "task", LocalId: args[1]})
	}
	if len(args) != 2 {
		return nil, command.Fail("INVALID_INPUT")
	}
	switch args[0] {
	case "budget-configure":
		cmd := new(v1.ConfigureBudgetCommand)
		if e := protojson.Unmarshal([]byte(args[1]), cmd); e != nil {
			return nil, e
		}
		return c.Budget.Configure(ctx, c.Caller, cmd)
	case "budget-limit":
		cmd := new(v1.AdjustBudgetLimitCommand)
		if e := protojson.Unmarshal([]byte(args[1]), cmd); e != nil {
			return nil, e
		}
		return c.Budget.AdjustLimit(ctx, c.Caller, cmd)
	case "bill-import":
		cmd := new(v1.ImportBillCommand)
		if e := protojson.Unmarshal([]byte(args[1]), cmd); e != nil {
			return nil, e
		}
		return c.Budget.ImportBill(ctx, c.Caller, cmd)
	case "budget-release":
		cmd := new(v1.ReleaseReservationCommand)
		if e := protojson.Unmarshal([]byte(args[1]), cmd); e != nil {
			return nil, e
		}
		return c.Budget.ReleaseUnused(ctx, c.Caller, cmd)
	}
	ref := new(v1.Ref)
	if e := protojson.Unmarshal([]byte(args[1]), ref); e != nil {
		return nil, e
	}
	switch args[0] {
	case "budget-version":
		return c.Budget.QueryBudgetVersion(ctx, c.Caller, ref)
	case "billing-source":
		return c.Budget.QueryBillingSource(ctx, c.Caller, ref)
	case "billing-entry":
		return c.Budget.QueryBillingEntry(ctx, c.Caller, ref)
	case "billing-conflict":
		return c.Budget.QueryBillingConflict(ctx, c.Caller, ref)
	}
	return nil, command.Fail("UNSUPPORTED_FEATURE")
}
