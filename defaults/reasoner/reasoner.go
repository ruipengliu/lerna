// Package reasoner 提供单次模型调用的默认推理实现。
package reasoner

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	port "github.com/ruipengliu/lerna/contracts/reasoner"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Reasoner struct{ Settings *v1.ModelSettings }

var _ port.Reasoner = (*Reasoner)(nil)

func (r *Reasoner) Propose(ctx context.Context, s *v1.ContextSnapshot, inv ...*port.Invocation) (*v1.Proposal, error) {
	if s == nil || s.TaskRef.GetName() == nil || len(inv) != 1 || inv[0] == nil || inv[0].Model == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	settings := &v1.ModelSettings{}
	if r.Settings != nil {
		settings = proto.Clone(r.Settings).(*v1.ModelSettings)
	}
	settings.ViewPolicy = port.ViewPolicy
	if settings.MaxInputBytes == 0 {
		settings.MaxInputBytes = 262144
	}
	if settings.MaxInputTokens == 0 {
		settings.MaxInputTokens = 65536
	}
	refs := append([]*v1.Ref(nil), s.ContentRefs...)
	for _, ref := range inv[0].InputRefs {
		found := false
		for _, current := range refs {
			found = found || proto.Equal(current, ref)
		}
		if !found {
			refs = append(refs, ref)
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		a, _ := json.Marshal(refs[i])
		b, _ := json.Marshal(refs[j])
		return string(a) < string(b)
	})
	response, e := inv[0].Model.Call(ctx, &port.ModelRequest{Settings: settings, InputRefs: refs})
	if e != nil {
		return nil, e
	}
	if response == nil || response.Result == nil {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	if response.Result.Status != "COMPLETED" {
		return nil, command.Fail(response.Result.Status)
	}
	p := &v1.Proposal{}
	if e = protojson.Unmarshal(response.Body, p); e != nil || p.Ref != nil || p.TaskId != nil || p.ContextSnapshotRef != nil || p.RequestRef != nil || p.ReasonerRef != nil || p.RequirementsVersion != 0 || p.InputVersion != 0 || p.ControlGeneration != 0 || p.PlanningGeneration != 0 || p.PromptVersion != "" || p.BodyContentRef != nil || len(p.CompletionEvidence) > 0 {
		return nil, command.Fail("INVALID_OUTPUT")
	}
	p.TaskId = s.TaskRef.Name
	p.ContextSnapshotRef = s.Ref
	p.RequestRef = s.RequestRef
	p.RequirementsVersion = s.RequirementsVersion
	p.InputVersion = s.InputVersion
	p.ControlGeneration = s.ControlGeneration
	p.PlanningGeneration = s.PlanningGeneration
	if e = port.Validate(s, p); e != nil {
		return nil, e
	}
	if inv[0].Requirements != nil {
		if e = port.ValidateContext(s, inv[0].Requirements, p); e != nil {
			return nil, e
		}
	}
	p.PromptVersion = "m1-default-v1"
	p.ReasonerRef = &v1.Ref{Name: &v1.GlobalName{UserId: s.TaskRef.Name.UserId, AuthorityDomainId: "default", ObjectKind: "reasoner", LocalId: "single-call"}, Revision: 1, SchemaId: "lerna.v1.Reasoner"}
	return p, nil
}

func (*Reasoner) DescribeReasoner(version uint32) (*v1.ReasonerDescription, error) {
	if version != 1 {
		return nil, command.Fail("UNSUPPORTED_CONTRACT")
	}
	return &v1.ReasonerDescription{ContractVersion: 1, ImplementationVersion: "default-v1", SupportedKinds: []string{"ACTION", "REQUIREMENTS", "QUESTION", "COMPLETE"}, MaxCallPositions: 1, MaxPhysicalSends: 1, MaxPlanLength: 1}, nil
}
