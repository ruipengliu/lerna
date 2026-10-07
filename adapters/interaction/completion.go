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

type CompletionCommands interface {
	BeginCompletion(context.Context, *v1.Caller, *v1.BeginCompletionCommand) (*v1.CommandReceipt, error)
	RecheckCompletion(context.Context, *v1.Caller, *v1.RecheckCompletionCommand) (*v1.CommandReceipt, error)
	QueryVerification(context.Context, *v1.Caller, *v1.Ref) (*v1.Verification, error)
	QueryResult(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Result, error)
	RequestProposal(context.Context, *v1.Caller, *v1.RequestProposalCommand) (*v1.ContextSnapshot, error)
	ReceiveProposal(context.Context, *v1.Caller, *v1.ReceiveProposalCommand) (*v1.CommandReceipt, error)
}

func (c CLI) completion(ctx context.Context, args []string) (proto.Message, error) {
	tasks, ok := c.Tasks.(CompletionCommands)
	if !ok {
		return nil, command.Fail("UNSUPPORTED_FEATURE")
	}
	if args[0] == "result" {
		if len(args) != 2 {
			return nil, command.Fail("INVALID_INPUT")
		}
		return tasks.QueryResult(ctx, c.Caller, &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain, ObjectKind: "task", LocalId: args[1]})
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
	switch args[0] {
	case "complete":
		cmd := new(v1.BeginCompletionCommand)
		if e = protojson.Unmarshal(b, cmd); e != nil {
			return nil, e
		}
		return tasks.BeginCompletion(ctx, c.Caller, cmd)
	case "recheck-completion":
		cmd := new(v1.RecheckCompletionCommand)
		if e = protojson.Unmarshal(b, cmd); e != nil {
			return nil, e
		}
		return tasks.RecheckCompletion(ctx, c.Caller, cmd)
	case "verification":
		ref := new(v1.Ref)
		if e = protojson.Unmarshal(b, ref); e != nil {
			return nil, e
		}
		return tasks.QueryVerification(ctx, c.Caller, ref)
	case "request-proposal":
		cmd := new(v1.RequestProposalCommand)
		if e = protojson.Unmarshal(b, cmd); e != nil {
			return nil, e
		}
		return tasks.RequestProposal(ctx, c.Caller, cmd)
	case "receive-proposal":
		cmd := new(v1.ReceiveProposalCommand)
		if e = protojson.Unmarshal(b, cmd); e != nil {
			return nil, e
		}
		return tasks.ReceiveProposal(ctx, c.Caller, cmd)
	}
	return nil, command.Fail("UNSUPPORTED_FEATURE")
}
