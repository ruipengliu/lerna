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

type TaskInputCommands interface {
	ProcessInput(context.Context, *v1.Caller, *v1.ProcessInputCommand) (*v1.CommandReceipt, error)
	AcceptRequirements(context.Context, *v1.Caller, *v1.AcceptRequirementsCommand) (*v1.CommandReceipt, error)
	QueryInputs(context.Context, *v1.Caller, *v1.GlobalName) (*v1.TaskInputHistory, error)
}

func (c CLI) runTaskInputCommand(ctx context.Context, args []string) (proto.Message, error) {
	tasks, ok := c.Tasks.(TaskInputCommands)
	if !ok {
		return nil, command.Fail("UNSUPPORTED_FEATURE")
	}
	if args[0] == "task-inputs" {
		if len(args) != 2 {
			return nil, command.Fail("INVALID_INPUT")
		}
		return tasks.QueryInputs(ctx, c.Caller, &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain, ObjectKind: "task", LocalId: args[1]})
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	path := f.String("json", "", "public command JSON file")
	if e := f.Parse(args[1:]); e != nil {
		return nil, e
	}
	if f.NArg() != 0 || *path == "" {
		return nil, command.Fail("INVALID_INPUT")
	}
	b, e := os.ReadFile(*path)
	if e != nil {
		return nil, e
	}
	if args[0] == "process-input" {
		cmd := new(v1.ProcessInputCommand)
		if e = protojson.Unmarshal(b, cmd); e != nil {
			return nil, e
		}
		return tasks.ProcessInput(ctx, c.Caller, cmd)
	}
	cmd := new(v1.AcceptRequirementsCommand)
	if e = protojson.Unmarshal(b, cmd); e != nil {
		return nil, e
	}
	return tasks.AcceptRequirements(ctx, c.Caller, cmd)
}
