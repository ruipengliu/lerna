// Package interaction 只收集命令与呈现公共契约，不写核心事实。
package interaction

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Sessions interface {
	SubmitGoal(context.Context, *v1.Caller, *v1.SubmitGoalCommand) (*v1.CommandReceipt, error)
	ProcessPending(context.Context, *v1.Caller) error
	QuerySession(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Session, error)
}
type Tasks interface {
	QueryTask(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Task, error)
}
type Durable interface {
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
}
type CLI struct {
	Grants            Grants
	Confirmations     Confirmations
	ConfirmationTasks ConfirmationTasks
	Content           Content
	Observations      ObservationContent
	Ledger            ExecutionLedger
	Egress            Egress
	Sessions          Sessions
	Tasks             Tasks
	Durable           Durable
	Caller            *v1.Caller
	Domain            string
}

func (c CLI) Run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: submit --command ID --goal TEXT [--session ID] | receipt ID | task ID | session ID | recover | execute START_JSON | operation ID | observation ID | session-create --command ID | input --command ID --session ID --kind KIND [--text TEXT] | question --json FILE | process-input --json FILE | accept-requirements --json FILE | task-inputs ID | complete --json FILE | result TASK_ID | verification --json REF_FILE | recheck-completion --json FILE | request-proposal --json FILE | receive-proposal --json FILE")
	}
	identity := func(id string) *v1.CommandIdentity {
		return &v1.CommandIdentity{UserId: c.Caller.UserId, IssuerId: c.Caller.IssuerId, TargetDomainId: c.Domain, CommandId: id}
	}
	var value proto.Message
	var err error
	switch args[0] {
	case "complete", "recheck-completion", "verification", "result", "request-proposal", "receive-proposal":
		value, err = c.completion(ctx, args)
	case "process-input", "accept-requirements", "task-inputs":
		value, err = c.runTaskInputCommand(ctx, args)
	case "session-create", "input", "question", "route-input", "query-input", "query-question":
		value, err = c.runSessionCommand(ctx, args)
	case "execute", "operation", "observation":
		value, err = c.execution(ctx, args)
	case "grant-request", "admission-confirmation", "grant-issue", "confirmation", "confirm", "withdraw-confirmation", "grant", "revoke-grant", "revocation":
		value, err = c.confirmationCommand(ctx, args)

	case "submit":
		flags := flag.NewFlagSet("submit", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		id := flags.String("command", "", "stable caller command id")
		goal := flags.String("goal", "", "goal text")
		session := flags.String("session", "", "existing session id")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" || *goal == "" || flags.NArg() != 0 {
			return command.Fail("INVALID_INPUT")
		}
		cmd := &v1.SubmitGoalCommand{Identity: identity(*id), ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: *goal}
		if *session != "" {
			cmd.Session = &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain, ObjectKind: "session", LocalId: *session}
		}
		if _, err := c.Sessions.SubmitGoal(ctx, c.Caller, cmd); err != nil {
			return err
		}
		if err := c.Sessions.ProcessPending(ctx, c.Caller); err != nil {
			return err
		}
		q, e := c.Durable.QueryReceipt(ctx, c.Caller, cmd.Identity)
		err = e
		if q != nil {
			value = q.Receipt
		}
	case "receipt":
		if len(args) != 2 {
			return command.Fail("INVALID_INPUT")
		}
		value, err = c.Durable.QueryReceipt(ctx, c.Caller, identity(args[1]))
	case "task", "session":
		if len(args) != 2 {
			return command.Fail("INVALID_INPUT")
		}
		name := &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain, ObjectKind: args[0], LocalId: args[1]}
		if args[0] == "task" {
			value, err = c.Tasks.QueryTask(ctx, c.Caller, name)
		} else {
			value, err = c.Sessions.QuerySession(ctx, c.Caller, name)
		}
	case "recover":
		if len(args) != 1 {
			return command.Fail("INVALID_INPUT")
		}
		if e := c.Sessions.ProcessPending(ctx, c.Caller); e != nil {
			return e
		}
		if c.Observations != nil {
			if e := c.Observations.ProcessObservations(ctx, c.Caller); e != nil {
				return e
			}
		}
		if c.Ledger != nil {
			if e := c.Ledger.ProcessReports(ctx, c.Caller); e != nil {
				return e
			}
			if e := c.Ledger.ProcessInterpretations(ctx, c.Caller); e != nil {
				return e
			}
		}
		if c.Grants != nil {
			if e := c.Grants.ProcessRevocations(ctx); e != nil {
				return e
			}
		}
		if tasks, ok := c.Tasks.(CompletionCommands); ok {
			return tasks.ProcessCompletions(ctx, c.Caller)
		}
		return nil
	default:
		return command.Fail("UNSUPPORTED_FEATURE")
	}
	if err != nil {
		return err
	}
	if value == nil || !value.ProtoReflect().IsValid() {
		return command.Fail("INVALID_INPUT")
	}
	b, err := (protojson.MarshalOptions{Multiline: true, EmitUnpopulated: true}).Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(b))
	return err
}
