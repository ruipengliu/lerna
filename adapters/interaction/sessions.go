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

type Content interface {
	Stage(context.Context, *v1.Caller, *v1.SubmitGoalCommand) (*v1.Ref, error)
}
type SessionCommands interface {
	CreateSession(context.Context, *v1.Caller, *v1.CreateSessionCommand) (*v1.CommandReceipt, error)
	SubmitInput(context.Context, *v1.Caller, *v1.SubmitInputCommand) (*v1.CommandReceipt, error)
	PublishQuestion(context.Context, *v1.Caller, *v1.PublishQuestionCommand) (*v1.CommandReceipt, error)
	RouteInput(context.Context, *v1.Caller, *v1.RouteInputCommand) (*v1.CommandReceipt, error)
	QueryInput(context.Context, *v1.Caller, *v1.Ref) (*v1.InputDelivery, error)
	QueryQuestion(context.Context, *v1.Caller, *v1.Ref) (*v1.Question, error)
}

func (c CLI) runSessionCommand(ctx context.Context, args []string) (proto.Message, error) {
	s, ok := c.Sessions.(SessionCommands)
	if !ok {
		return nil, command.Fail("UNSUPPORTED_FEATURE")
	}
	name := func(kind, id string) *v1.GlobalName {
		return &v1.GlobalName{UserId: c.Caller.UserId, AuthorityDomainId: c.Domain, ObjectKind: kind, LocalId: id}
	}
	if args[0] == "query-input" || args[0] == "query-question" {
		if len(args) != 2 {
			return nil, command.Fail("INVALID_INPUT")
		}
		if args[0] == "query-input" {
			return s.QueryInput(ctx, c.Caller, &v1.Ref{Name: name("input", args[1]), Revision: 1, SchemaId: "lerna.v1.SessionInput"})
		}
		return s.QueryQuestion(ctx, c.Caller, &v1.Ref{Name: name("question", args[1]), Revision: 1, SchemaId: "lerna.v1.Question"})
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	id := f.String("command", "", "stable command identity")
	session := f.String("session", "", "explicit session id")
	kind := f.String("kind", "", "GOAL, MODIFY, ANSWER, RECORD, CONTROL")
	task := f.String("task", "", "explicit target task id")
	text := f.String("text", "", "input text")
	request := f.String("request", "", "exact question id")
	requestVersion := f.Uint64("request-version", 1, "question version")
	inputVersion := f.Uint64("input-version", 0, "observed task input version")
	requirementsVersion := f.Uint64("requirements-version", 0, "observed requirements version")
	control := f.String("control", "", "PAUSE, RESUME, CANCEL")
	generation := f.Uint64("control-generation", 0, "observed control generation")
	input := f.String("input", "", "waiting input id")
	jsonPath := f.String("json", "", "public command JSON file")
	if e := f.Parse(args[1:]); e != nil {
		return nil, e
	}
	if f.NArg() != 0 {
		return nil, command.Fail("INVALID_INPUT")
	}
	if *jsonPath != "" {
		b, e := os.ReadFile(*jsonPath)
		if e != nil {
			return nil, e
		}
		switch args[0] {
		case "question":
			q := new(v1.PublishQuestionCommand)
			if e = protojson.Unmarshal(b, q); e != nil {
				return nil, e
			}
			return s.PublishQuestion(ctx, c.Caller, q)
		case "input":
			q := new(v1.SubmitInputCommand)
			if e = protojson.Unmarshal(b, q); e != nil {
				return nil, e
			}
			return s.SubmitInput(ctx, c.Caller, q)
		default:
			return nil, command.Fail("INVALID_INPUT")
		}
	}
	if *id == "" {
		return nil, command.Fail("INVALID_INPUT")
	}
	h := &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: c.Caller.UserId, IssuerId: c.Caller.IssuerId, TargetDomainId: c.Domain, CommandId: *id}, ContractVersion: 1, SchemaId: "lerna.v1.AdmissionCommands", FingerprintVersion: 1}
	switch args[0] {
	case "session-create":
		return s.CreateSession(ctx, c.Caller, &v1.CreateSessionCommand{Header: h})
	case "route-input":
		return s.RouteInput(ctx, c.Caller, &v1.RouteInputCommand{Header: h, InputRef: &v1.Ref{Name: name("input", *input), Revision: 1, SchemaId: "lerna.v1.SessionInput"}})
	case "input":
		q := &v1.SubmitInputCommand{Header: h, SessionId: name("session", *session), InputKind: *kind, ExpectedInputVersion: *inputVersion, ExpectedRequirementsVersion: *requirementsVersion, Control: *control, ExpectedControlGeneration: *generation}
		if *task != "" {
			q.TaskId = name("task", *task)
		}
		if *request != "" {
			q.RequestRef = &v1.Ref{Name: name("question", *request), Revision: *requestVersion, SchemaId: "lerna.v1.Question"}
		}
		if *text != "" {
			if c.Content == nil {
				return nil, command.Fail("UNSUPPORTED_FEATURE")
			}
			r, e := c.Content.Stage(ctx, c.Caller, &v1.SubmitGoalCommand{Identity: h.Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: *text})
			if e != nil {
				return nil, e
			}
			q.ContentRef = r
		}
		return s.SubmitInput(ctx, c.Caller, q)
	default:
		return nil, command.Fail("INVALID_INPUT")
	}
}
