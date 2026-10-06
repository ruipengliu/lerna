package tasks

import (
	"context"
	"fmt"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/reasoner"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func (s *Service) prepareReasonerProposal(ctx context.Context, caller *v1.Caller, snap *v1.ContextSnapshot, result *v1.ModelCallResult, p *v1.Proposal) error {
	if e := reasoner.Validate(snap, p); e != nil {
		return e
	}
	var requirements *v1.Requirements
	if snap.RequirementsRef != nil {
		var e error
		requirements, e = s.QueryRequirements(ctx, caller, snap.RequirementsRef)
		if e != nil {
			return e
		}
	}
	if e := reasoner.ValidateContext(snap, requirements, p); e != nil {
		return e
	}
	if p.Kind == "COMPLETE" {
		for _, verdict := range p.Verdicts {
			if verdict.Conclusion == "SATISFIED" || verdict.Conclusion == "UNSATISFIED" && verdict.OperationId != nil {
				p.CompletionEvidence = append(p.CompletionEvidence, &v1.CompletionEvidence{ConditionId: verdict.ConditionId, OperationId: verdict.OperationId, ConfirmationRef: verdict.ConfirmationRef})
			}
		}
		return nil
	}
	if p.Kind == "REQUIREMENTS" {
		if e := s.validateConditions(ctx, caller, p.RequirementsChange.Conditions); e != nil {
			return command.Fail("INVALID_OUTPUT")
		}
	}
	if p.Kind != "ACTION" {
		return nil
	}
	step := p.Step
	known := false
	for _, ref := range snap.CapabilityRefs {
		known = known || proto.Equal(ref, step.CapabilityRef)
	}
	if !known {
		return command.Fail("INVALID_OUTPUT")
	}
	capability, e := s.QueryCapability(ctx, caller, step.CapabilityRef)
	if e != nil {
		return e
	}
	if capability == nil || capability.Action == "MODEL_INFER" || capability.SchemaDigest == "" || capability.SchemaDigest != step.SchemaDigest {
		return command.Fail("INVALID_OUTPUT")
	}
	if len(step.ArgumentsJson) > 0 {
		if step.ParametersRef != nil || result == nil || result.OutputRef == nil {
			return command.Fail("INVALID_OUTPUT")
		}
		if e = reasoner.ValidateArguments(capability.ParameterSchemaJson, step.ArgumentsJson); e != nil {
			return command.Fail("INVALID_OUTPUT")
		}
		ref, e := s.deriveReasonerBody(ctx, caller, snap, result.OutputRef, "arguments", func(raw []byte) ([]byte, error) {
			original := &v1.Proposal{}
			if protojson.Unmarshal(raw, original) != nil || original.Step == nil || !proto.Equal(original.Step, step) {
				return nil, command.Fail("INVALID_OUTPUT")
			}
			return original.Step.ArgumentsJson, nil
		})
		if e != nil {
			return e
		}
		step.ParametersRef = ref
	} else {
		known = false
		for _, ref := range snap.ContentRefs {
			known = known || proto.Equal(ref, step.ParametersRef)
		}
		if !known {
			return command.Fail("INVALID_OUTPUT")
		}
		content, e := s.modelContent.Read(ctx, caller, step.ParametersRef)
		if e != nil {
			return e
		}
		if e = reasoner.ValidateArguments(capability.ParameterSchemaJson, command.ContentBytes(content)); e != nil {
			return command.Fail("INVALID_OUTPUT")
		}
	}
	return nil
}

// deriveReasonerBody 从实际模型输出提取正文，不能以推理自报的父引用代替读取。
func (s *Service) deriveReasonerBody(ctx context.Context, caller *v1.Caller, snap *v1.ContextSnapshot, output *v1.Ref, kind string, extract func([]byte) ([]byte, error), extras ...*v1.Ref) (*v1.Ref, error) {
	prefix := "reasoner-" + kind + ":" + snap.RequestRef.Name.LocalId
	header := func(suffix string) *v1.CommandHeader { return s.modelHeader(prefix+suffix, s.domain+"/content") }
	prepared, e := s.modelContent.PrepareDerivation(ctx, caller, &v1.PrepareDerivationCommand{Header: header(":prepare"), TaskId: snap.TaskRef.Name, GeneratorVersion: "m1-reasoner-" + kind + "-v1", OutputKind: "MODEL_OUTPUT", MediaType: "application/json"})
	if e = modelReceipt(prepared, e); e != nil {
		return nil, e
	}
	d, e := s.modelContent.QueryDerivation(ctx, caller, prepared.ResultRef)
	if e != nil {
		return nil, e
	}
	sources := uniqueRefs(append([]*v1.Ref{output}, extras...))
	var input *v1.Content
	for i, source := range sources {
		var read *v1.Content
		if d.State == "PREPARED" || d.State == "COMPUTING" {
			read, e = s.modelContent.ReadDerivationInput(ctx, caller, &v1.ReadDerivationInputCommand{Header: header(fmt.Sprintf(":read:%d", i)), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, InputRef: source})
		} else {
			read, e = s.modelContent.Read(ctx, caller, source)
		}
		if e != nil {
			return nil, e
		}
		if i == 0 {
			input = read
		}
	}
	body, e := extract(command.ContentBytes(input))
	if e != nil {
		return nil, e
	}
	if len(body) == 0 {
		return nil, command.Fail("INVALID_OUTPUT")
	}
	sealed, e := s.modelContent.SealDerivation(ctx, caller, &v1.SealDerivationCommand{Header: header(":seal"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, InputRefs: sources})
	if e = modelReceipt(sealed, e); e != nil {
		return nil, e
	}
	committed, e := s.modelContent.CommitDerivation(ctx, caller, &v1.CommitDerivationCommand{Header: header(":commit"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, Body: body})
	if e = modelReceipt(committed, e); e != nil {
		return nil, e
	}
	if e = s.modelContent.ProcessRegistrations(ctx, caller); e != nil {
		return nil, e
	}
	return committed.ResultRef, nil
}
