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
	Step               *v1.ActionStep
	CompletionEvidence []*v1.CompletionEvidence
}

func (r *Reasoner) Propose(_ context.Context, s *v1.ContextSnapshot) (*v1.Proposal, error) {
	if s == nil || s.TaskRef == nil || s.TaskRef.Name == nil || (r.Step == nil && r.CompletionEvidence == nil) || (r.Step != nil && r.CompletionEvidence != nil) {
		return nil, command.Fail("INVALID_INPUT")
	}
	p := &v1.Proposal{TaskId: s.TaskRef.Name, ContextSnapshotRef: s.Ref, RequestRef: s.RequestRef, RequirementsVersion: s.RequirementsVersion, InputVersion: s.InputVersion, ControlGeneration: s.ControlGeneration, PlanningGeneration: s.PlanningGeneration, Kind: "ACTION", ReasonerRef: &v1.Ref{Name: &v1.GlobalName{UserId: s.TaskRef.Name.UserId, AuthorityDomainId: "scripted", ObjectKind: "reasoner", LocalId: "single-step"}, Revision: 1, SchemaId: "lerna.v1.Reasoner"}}
	if r.Step != nil {
		p.Step = proto.Clone(r.Step).(*v1.ActionStep)
	} else {
		p.Kind = "COMPLETE"
		for _, evidence := range r.CompletionEvidence {
			p.CompletionEvidence = append(p.CompletionEvidence, proto.Clone(evidence).(*v1.CompletionEvidence))
		}
	}
	return p, nil
}
