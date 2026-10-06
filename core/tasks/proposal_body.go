package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// proposalProjection 保留裁决与审计的结构引用，正文只由内容治理持有。
func proposalProjection(p *v1.Proposal) *v1.Proposal {
	p = proto.Clone(p).(*v1.Proposal)
	p.Gaps = nil
	p.ResultDraft = ""
	if p.Step != nil {
		p.Step.ArgumentsJson = nil
		p.Step.ExpectedEvidence = nil
	}
	if p.Question != nil {
		p.Question.Question = ""
		p.Question.Options = nil
	}
	if p.RequirementsChange != nil {
		p.RequirementsChange.Reason = ""
		p.RequirementsChange.Impact = ""
	}
	for _, verdict := range p.Verdicts {
		verdict.Gaps = nil
	}
	return p
}
func (s *Service) storeReasonerBody(ctx context.Context, caller *v1.Caller, snap *v1.ContextSnapshot, result *v1.ModelCallResult, p *v1.Proposal) (*v1.Proposal, error) {
	if len(snap.ContentRefs) == 0 {
		return nil, command.Fail("PREPARATION_UNRECOVERABLE")
	}
	source := snap.ContentRefs[0]
	extra := append([]*v1.Ref(nil), snap.ContentRefs...)
	if result != nil && result.OutputRef != nil {
		source = result.OutputRef
		extra = nil
	}
	if p.Step != nil && p.Step.ParametersRef != nil {
		extra = append(extra, p.Step.ParametersRef)
	}
	if p.RequirementsChange != nil {
		for _, condition := range p.RequirementsChange.Conditions {
			extra = append(extra, condition.DescriptionRef)
			if condition.TargetRecord != nil {
				extra = append(extra, condition.TargetRecord.ParametersRef)
			}
		}
	}
	ref, e := s.deriveReasonerBody(ctx, caller, snap, source, "proposal", func(_ []byte) ([]byte, error) { return protojson.Marshal(p) }, extra...)
	if e != nil {
		return nil, e
	}
	p = proposalProjection(p)
	p.BodyContentRef = ref
	return p, nil
}

// ReadProposal 只有正文当前可用时才返回完整载荷；QueryProposal 始终提供结构审计记录。
func (s *Service) ReadProposal(ctx context.Context, caller *v1.Caller, ref *v1.Ref) (*v1.Proposal, error) {
	p, e := s.QueryProposal(ctx, caller, ref)
	if e != nil || p == nil {
		return p, e
	}
	return s.readProposalBody(ctx, caller, p)
}
func (s *Service) readProposalBody(ctx context.Context, caller *v1.Caller, p *v1.Proposal) (*v1.Proposal, error) {
	if p.BodyContentRef == nil {
		return p, nil
	}
	if e := s.content.CheckUsable(ctx, caller, p.BodyContentRef); e != nil {
		return nil, e
	}
	content, e := s.modelContent.Read(ctx, caller, p.BodyContentRef)
	if e != nil {
		return nil, e
	}
	if content == nil || content.Kind != "MODEL_OUTPUT" || content.GetSourceDescriptor().GetKind() != "DERIVED" || content.GetSourceDescriptor().GetProviderVersion() != "m1-reasoner-proposal-v1" || !proto.Equal(content.TaskId, p.TaskId) || len(content.DerivedFrom) == 0 || content.GetSource().GetCommandId() != "reasoner-proposal:"+p.GetRequestRef().GetName().GetLocalId()+":commit" {
		return nil, command.Fail("INVALID_PROPOSAL")
	}
	call, e := s.store.(modelCallStore).LoadModelCall(ctx, p.RequestRef, 0)
	if e != nil {
		return nil, e
	}
	if call != nil {
		found := false
		for _, parent := range content.DerivedFrom {
			found = found || proto.Equal(parent, call.GetResult().GetOutputRef())
		}
		if call.GetResult().GetOutputRef() == nil || !found {
			return nil, command.Fail("INVALID_PROPOSAL")
		}
	}
	full := &v1.Proposal{}
	if protojson.Unmarshal(command.ContentBytes(content), full) != nil {
		return nil, command.Fail("INVALID_PROPOSAL")
	}
	full.Ref = p.Ref
	full.BodyContentRef = p.BodyContentRef
	if !proto.Equal(proposalProjection(full), p) {
		return nil, command.Fail("INVALID_PROPOSAL")
	}
	return full, nil
}
