package interaction

import (
	"context"
	"flag"
	"io"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Confirmations interface {
	ReadCurrentConfirmation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Confirmation, error)
	RespondConfirmation(context.Context, *v1.Caller, *v1.RespondConfirmationCommand) (*v1.CommandReceipt, error)
	WithdrawConfirmation(context.Context, *v1.Caller, *v1.WithdrawConfirmationCommand) (*v1.CommandReceipt, error)
}
type ConfirmationTasks interface {
	RequestAdmissionConfirmation(context.Context, *v1.Caller, *v1.RequestAdmissionConfirmationCommand) (*v1.CommandReceipt, error)
}
type Grants interface {
	ProcessRevocations(context.Context) error
	RequestGrantConfirmation(context.Context, *v1.Caller, *v1.RequestGrantConfirmationCommand) (*v1.CommandReceipt, error)
	IssueGrant(context.Context, *v1.Caller, *v1.IssueGrantCommand) (*v1.CommandReceipt, error)
	QueryGrantStatus(context.Context, *v1.Caller, *v1.GlobalName) (*v1.GrantStatus, error)
	Revoke(context.Context, *v1.Caller, *v1.RevokeGrantCommand) (*v1.CommandReceipt, error)
	QueryCurrentRevocation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.GrantRevocation, error)
}

func (c CLI) confirmationCommand(ctx context.Context, args []string) (proto.Message, error) {
	name := func(kind, id string) *v1.GlobalName {
		return &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain, ObjectKind: kind, LocalId: id}
	}
	switch args[0] {
	case "confirmation", "grant", "revocation":
		if len(args) != 2 || args[1] == "" {
			return nil, command.Fail("INVALID_INPUT")
		}
		switch args[0] {
		case "confirmation":
			return c.Confirmations.ReadCurrentConfirmation(ctx, c.Caller, name("confirmation", args[1]))
		case "grant":
			return c.Grants.QueryGrantStatus(ctx, c.Caller, name("grant", args[1]))
		default:
			return c.Grants.QueryCurrentRevocation(ctx, c.Caller, name("grant-revocation", args[1]))
		}
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	id := flags.String("command", "", "stable caller command id")
	var jsonBody, session, confirmation, digest, decision, grant *string
	var revision *uint64
	switch args[0] {
	case "grant-request":
		jsonBody = flags.String("json", "", "Grant JSON")
		session = flags.String("session", "", "session id")
	case "admission-confirmation":
		jsonBody = flags.String("json", "", "RequestAdmissionConfirmationCommand JSON without header")
	case "confirm", "withdraw-confirmation", "grant-issue":
		confirmation = flags.String("confirmation", "", "confirmation id")
		revision = flags.Uint64("revision", 0, "displayed confirmation revision")
		if args[0] == "confirm" {
			digest = flags.String("digest", "", "displayed binding digest")
			decision = flags.String("decision", "", "APPROVE or REJECT")
		}
	case "revoke-grant":
		grant = flags.String("grant", "", "grant id")
	default:
		return nil, command.Fail("UNSUPPORTED_FEATURE")
	}
	if e := flags.Parse(args[1:]); e != nil {
		return nil, e
	}
	if *id == "" || flags.NArg() != 0 {
		return nil, command.Fail("INVALID_INPUT")
	}
	h := &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: c.Caller.UserId, IssuerId: c.Caller.IssuerId, TargetDomainId: c.Domain, CommandId: *id}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
	var ref *v1.Ref
	if confirmation != nil {
		if *confirmation == "" || *revision == 0 {
			return nil, command.Fail("INVALID_INPUT")
		}
		ref = &v1.Ref{Name: name("confirmation", *confirmation), Revision: *revision, SchemaId: "lerna.v1.Confirmation"}
	}
	switch args[0] {
	case "grant-request":
		g := &v1.Grant{}
		if e := protojson.Unmarshal([]byte(*jsonBody), g); e != nil {
			return nil, e
		}
		cmd := &v1.RequestGrantConfirmationCommand{Header: h, Grant: g}
		if *session != "" {
			cmd.SessionId = name("session", *session)
		}
		return c.Grants.RequestGrantConfirmation(ctx, c.Caller, cmd)
	case "admission-confirmation":
		cmd := &v1.RequestAdmissionConfirmationCommand{}
		if e := protojson.Unmarshal([]byte(*jsonBody), cmd); e != nil {
			return nil, e
		}
		if cmd.Header != nil {
			return nil, command.Fail("INVALID_INPUT")
		}
		cmd.Header = h
		return c.ConfirmationTasks.RequestAdmissionConfirmation(ctx, c.Caller, cmd)
	case "confirm":
		if *digest == "" || (*decision != "APPROVE" && *decision != "REJECT") {
			return nil, command.Fail("INVALID_INPUT")
		}
		return c.Confirmations.RespondConfirmation(ctx, c.Caller, &v1.RespondConfirmationCommand{Header: h, ConfirmationRef: ref, BindingDigest: *digest, Decision: *decision})
	case "withdraw-confirmation":
		return c.Confirmations.WithdrawConfirmation(ctx, c.Caller, &v1.WithdrawConfirmationCommand{Header: h, ConfirmationRef: ref})
	case "grant-issue":
		return c.Grants.IssueGrant(ctx, c.Caller, &v1.IssueGrantCommand{Header: h, ConfirmationRef: ref})
	default:
		if *grant == "" {
			return nil, command.Fail("INVALID_INPUT")
		}
		return c.Grants.Revoke(ctx, c.Caller, &v1.RevokeGrantCommand{Header: h, GrantId: name("grant", *grant)})
	}
}
