package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type ReasonerQuestions interface {
	PublishQuestion(context.Context, *v1.Caller, *v1.PublishQuestionCommand) (*v1.CommandReceipt, error)
}

func (s *Service) WithReasonerQuestions(q ReasonerQuestions) *Service {
	s.reasonerQuestions = q
	return s
}

// PublishProposalQuestion 把已保存的提问发布为会话输入请求；涉及确认的文字仍不是确认凭据。
func (s *Service) PublishProposalQuestion(ctx context.Context, caller *v1.Caller, c *v1.PublishProposalQuestionCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "host" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	q, e := s.ReadProposal(ctx, caller, c.ProposalRef)
	if e != nil {
		return nil, e
	}
	if q == nil || (q.Kind != "QUESTION" && q.Kind != "REQUIREMENTS") {
		return nil, command.Fail("INVALID_PROPOSAL")
	}
	snap, e := s.QuerySnapshot(ctx, caller, q.ContextSnapshotRef)
	if e != nil {
		return nil, e
	}
	call, e := s.QueryModelCall(ctx, caller, q.RequestRef, 0)
	if e != nil {
		return nil, e
	}
	source := snap.ContentRefs[0]
	if q.BodyContentRef != nil {
		source = q.BodyContentRef
	}
	if call != nil && q.BodyContentRef == nil {
		if call.Result.GetOutputRef() == nil {
			return nil, command.Fail("PREPARATION_UNRECOVERABLE")
		}
		source = call.Result.OutputRef
	}
	ref, e := s.deriveReasonerBody(ctx, caller, snap, source, "question", func(body []byte) ([]byte, error) {
		if call != nil || q.BodyContentRef != nil {
			original := &v1.Proposal{}
			if protojson.Unmarshal(body, original) != nil || !proto.Equal(original.Question, q.Question) || !proto.Equal(original.RequirementsChange, q.RequirementsChange) {
				return nil, command.Fail("INVALID_OUTPUT")
			}
		}
		if q.Kind == "REQUIREMENTS" {
			return protojson.Marshal(q.RequirementsChange)
		}
		return protojson.Marshal(q.Question)
	})
	if e != nil {
		return nil, e
	}
	return s.reasonerQuestions.PublishQuestion(ctx, caller, &v1.PublishQuestionCommand{Header: c.Header, SessionId: c.SessionId, TaskRef: snap.TaskRef, ContentRef: ref, ChangesBasis: q.Kind == "REQUIREMENTS" || q.Question.GetChangesBasis(), ExpiresAtUnixMs: snap.ExpiresAtUnixMs})
}
