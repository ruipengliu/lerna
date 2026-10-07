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
type Metrics interface {
	QueryMetrics(context.Context, *v1.Caller) (*v1.LocalMetrics, error)
}
type ManualProgress interface {
	ManualProgress(context.Context, *v1.Caller) error
}
type CLI struct {
	Progress          ManualProgress
	Reasoner          DefaultReasonerRunner
	Metrics           Metrics
	Budget            Budget
	Trace             Trace
	Grants            Grants
	Confirmations     Confirmations
	ConfirmationTasks ConfirmationTasks
	Content           Content
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
		return fmt.Errorf("usage: submit --command ID --goal TEXT [--session ID] | receipt ID | task ID | cancellation TASK_ID | session ID | recover | execute START_JSON | prepare-resend RESEND_JSON | operation ID | observation ID | session-create --command ID | input --command ID --session ID --kind KIND [--text TEXT] | question --json FILE | process-input --json FILE | accept-requirements --json FILE | task-inputs ID | complete --json FILE | result TASK_ID | verification --json REF_FILE | recheck-completion --json FILE | close-task --json FILE | task-closing TASK_ID | request-proposal --json FILE | receive-proposal --json FILE | run-reasoner --json FILE | read-proposal --json REF_FILE | configure-reasoner --json FILE | advance-task --json FILE | request-reconciliation --json FILE | control-reconciliation --json FILE | reconciliation OPERATION_ID | reconciliation-query --json REF_FILE | reconciliation-finding --json REF_FILE | closure-confirmation --json FILE")
	}
	identity := func(id string) *v1.CommandIdentity {
		return &v1.CommandIdentity{UserId: c.Caller.UserId, IssuerId: c.Caller.IssuerId, TargetDomainId: c.Domain, CommandId: id}
	}
	var value proto.Message
	var err error
	switch args[0] {
	case "metrics":
		if len(args) != 1 || c.Metrics == nil {
			return command.Fail("INVALID_INPUT")
		}
		value, err = c.Metrics.QueryMetrics(ctx, c.Caller)
	case "trace-task", "trace-operation":
		value, err = c.trace(ctx, args)
	case "cancellation":
		value, err = c.cancellation(ctx, args)
	case "close-task", "task-closing":
		value, err = c.taskClosing(ctx, args)
	case "run-reasoner", "read-proposal", "configure-reasoner", "advance-task":
		value, err = c.reasonerCommand(ctx, args)
	case "complete", "recheck-completion", "verification", "result", "request-proposal", "receive-proposal":
		value, err = c.completion(ctx, args)
	case "budget", "budget-configure", "budget-limit", "budget-version", "bill-import", "budget-release", "billing-source", "billing-entry", "billing-conflict":
		value, err = c.budgetCommand(ctx, args)
	case "process-input", "accept-requirements", "task-inputs":
		value, err = c.runTaskInputCommand(ctx, args)
	case "session-create", "input", "question", "route-input", "query-input", "query-question":
		value, err = c.runSessionCommand(ctx, args)
	case "request-reconciliation", "control-reconciliation", "reconciliation", "reconciliation-query", "reconciliation-finding", "closure-confirmation":
		value, err = c.reconciliationCommand(ctx, args)
	case "execute", "prepare-resend", "operation", "observation":
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
		if c.Progress == nil {
			return command.Fail("DEPENDENCY_UNAVAILABLE")
		}
		return c.Progress.ManualProgress(ctx, c.Caller)
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
