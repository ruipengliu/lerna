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

type DefaultReasonerRunner interface {
	RunDefaultReasoner(context.Context, *v1.Caller, *v1.RunModelCallCommand) (*v1.ProposalOutcome, error)
}

type ReasonerDriver interface {
	ConfigureReasonerDriver(context.Context, *v1.Caller, *v1.ConfigureReasonerDriverCommand) (*v1.CommandReceipt, error)
	AdvanceReasonerTask(context.Context, *v1.Caller, *v1.AdvanceReasonerTaskRequest) (*v1.ReasonerDriver, error)
}

type ProposalReader interface {
	ReadProposal(context.Context, *v1.Caller, *v1.Ref) (*v1.Proposal, error)
}

func (c CLI) reasonerCommand(ctx context.Context, args []string) (proto.Message, error) {
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	path := f.String("json", "", "fixed host command or exact proposal reference JSON")
	if e := f.Parse(args[1:]); e != nil {
		return nil, e
	}
	if *path == "" || f.NArg() != 0 {
		return nil, command.Fail("INVALID_INPUT")
	}
	body, e := os.ReadFile(*path)
	if e != nil {
		return nil, e
	}
	if args[0] == "configure-reasoner" || args[0] == "advance-task" {
		driver, ok := c.Tasks.(ReasonerDriver)
		if !ok {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		if args[0] == "configure-reasoner" {
			request := new(v1.ConfigureReasonerDriverCommand)
			if e = protojson.Unmarshal(body, request); e != nil {
				return nil, e
			}
			return driver.ConfigureReasonerDriver(ctx, c.Caller, request)
		}
		request := new(v1.AdvanceReasonerTaskRequest)
		if e = protojson.Unmarshal(body, request); e != nil {
			return nil, e
		}
		return driver.AdvanceReasonerTask(ctx, c.Caller, request)
	}
	if args[0] == "run-reasoner" {
		if c.Reasoner == nil {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		run := new(v1.RunModelCallCommand)
		if e = protojson.Unmarshal(body, run); e != nil {
			return nil, e
		}
		return c.Reasoner.RunDefaultReasoner(ctx, c.Caller, run)
	}
	reader, ok := c.Tasks.(ProposalReader)
	if !ok {
		return nil, command.Fail("UNSUPPORTED_FEATURE")
	}
	ref := new(v1.Ref)
	if e = protojson.Unmarshal(body, ref); e != nil {
		return nil, e
	}
	return reader.ReadProposal(ctx, c.Caller, ref)
}
