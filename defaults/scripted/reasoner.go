// Package scripted 提供无网络、无执行权限的确定性推理实现。
package scripted

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	"github.com/ruipengliu/lerna/contracts/reasoner"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

var _ reasoner.Reasoner = (*Reasoner)(nil)

type Reasoner struct {
	Template           *v1.Proposal
	Question           *v1.QuestionProposal
	Step               *v1.ActionStep
	CompletionEvidence []*v1.CompletionEvidence
}

func (r *Reasoner) Propose(_ context.Context, s *v1.ContextSnapshot, invocation ...*reasoner.Invocation) (*v1.Proposal, error) {
	if r.Template != nil {
		if s == nil || s.TaskRef.GetName() == nil {
			return nil, command.Fail("INVALID_INPUT")
		}
		p := proto.Clone(r.Template).(*v1.Proposal)
		if e := reasoner.Validate(s, p); e != nil {
			return nil, e
		}
		p.Ref = nil
		p.TaskId = s.TaskRef.Name
		p.ContextSnapshotRef = s.Ref
		p.RequestRef = s.RequestRef
		p.RequirementsVersion = s.RequirementsVersion
		p.InputVersion = s.InputVersion
		p.ControlGeneration = s.ControlGeneration
		p.PlanningGeneration = s.PlanningGeneration
		p.ReasonerRef = &v1.Ref{Name: &v1.GlobalName{UserId: s.TaskRef.Name.UserId, AuthorityDomainId: "scripted", ObjectKind: "reasoner", LocalId: "single-step"}, Revision: 1, SchemaId: "lerna.v1.Reasoner"}
		return p, nil
	}
	if s == nil || s.TaskRef == nil || s.TaskRef.Name == nil || (r.Step == nil && r.CompletionEvidence == nil && r.Question == nil) || (r.Step != nil && r.CompletionEvidence != nil) {
		return nil, command.Fail("INVALID_INPUT")
	}
	p := &v1.Proposal{TaskId: s.TaskRef.Name, ContextSnapshotRef: s.Ref, RequestRef: s.RequestRef, RequirementsVersion: s.RequirementsVersion, InputVersion: s.InputVersion, ControlGeneration: s.ControlGeneration, PlanningGeneration: s.PlanningGeneration, Kind: "ACTION", ReasonerRef: &v1.Ref{Name: &v1.GlobalName{UserId: s.TaskRef.Name.UserId, AuthorityDomainId: "scripted", ObjectKind: "reasoner", LocalId: "single-step"}, Revision: 1, SchemaId: "lerna.v1.Reasoner"}}
	if r.Question != nil {
		p.Kind = "QUESTION"
		p.Question = proto.Clone(r.Question).(*v1.QuestionProposal)
	} else if r.Step != nil {
		p.Step = proto.Clone(r.Step).(*v1.ActionStep)
	} else {
		p.Kind = "COMPLETE"
		for _, evidence := range r.CompletionEvidence {
			p.CompletionEvidence = append(p.CompletionEvidence, proto.Clone(evidence).(*v1.CompletionEvidence))
		}
	}
	if len(invocation) > 0 {
		if e := reasoner.Validate(s, p); e != nil {
			return nil, e
		}
	}
	return p, nil
}

func (*Reasoner) DescribeReasoner(version uint32) (*v1.ReasonerDescription, error) {
	if version != 1 {
		return nil, command.Fail("UNSUPPORTED_CONTRACT")
	}
	return &v1.ReasonerDescription{ContractVersion: 1, ImplementationVersion: "scripted-v1", SupportedKinds: []string{"ACTION", "REQUIREMENTS", "QUESTION", "COMPLETE"}, MaxCallPositions: 1, MaxPhysicalSends: 1, MaxPlanLength: 1}, nil
}
