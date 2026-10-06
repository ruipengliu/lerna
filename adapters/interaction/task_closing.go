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

type TaskClosingCommands interface {
	BeginTaskClose(context.Context, *v1.Caller, *v1.BeginTaskCloseCommand) (*v1.CommandReceipt, error)
	ProcessTaskClosings(context.Context, *v1.Caller) error
	QueryTaskClosingView(context.Context, *v1.Caller, *v1.GlobalName) (*v1.TaskClosingView, error)
}

func (c CLI) taskClosing(ctx context.Context, args []string) (proto.Message, error) {
	tasks, ok := c.Tasks.(TaskClosingCommands)
	if !ok {
		return nil, command.Fail("UNSUPPORTED_FEATURE")
	}
	if args[0] == "task-closing" {
		if len(args) != 2 {
			return nil, command.Fail("INVALID_INPUT")
		}
		return tasks.QueryTaskClosingView(ctx, c.Caller, &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain, ObjectKind: "task", LocalId: args[1]})
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	path := f.String("json", "", "public command JSON file")
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
	cmd := new(v1.BeginTaskCloseCommand)
	if e = protojson.Unmarshal(b, cmd); e != nil {
		return nil, e
	}
	return tasks.BeginTaskClose(ctx, c.Caller, cmd)
}

// ClosingFollowupLedger 只驱动原负责方保存的收尾责任。
type ClosingFollowupLedger interface {
	ProcessExecutionFollowups(context.Context, *v1.Caller) error
}
type ClosingFollowupBudget interface {
	ProcessSettlementFollowups(context.Context, *v1.Caller) error
}
