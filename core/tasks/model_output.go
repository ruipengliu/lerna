package tasks

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Service) collectModelOutput(ctx context.Context, caller *v1.Caller, call *v1.ModelCall, a *v1.Admission, op *v1.Operation) (*v1.ModelCallResult, error) {
	result := &v1.ModelCallResult{CallRef: call.Ref, Status: "UNKNOWN", OperationId: a.OperationId, InputRef: call.InputRef}
	x := op.Execution
	if x == nil || x.Send.ObservationRef == nil {
		return s.saveModelResult(ctx, caller, call, result)
	}
	raw, e := s.modelLedger.QueryObservation(ctx, caller, x.Send.ObservationRef)
	if e != nil {
		return nil, e
	}
	if raw == nil {
		return s.saveModelResult(ctx, caller, call, result)
	}
	result.ObservationRef = raw.Ref
	reports, e := s.modelLedger.QueryReports(ctx, caller, raw.Ref)
	if e != nil {
		return nil, e
	}
	if reports != nil && reports.UsageReceipt != nil {
		result.UsageRef = reports.UsageReceipt.ResultRef
	}
	if raw.TransportError != "" {
		return s.saveModelResult(ctx, caller, call, result)
	}
	prefix := "model-output:" + call.Ref.Name.LocalId + ":" + raw.Ref.Name.LocalId
	ch := func(suffix string) *v1.CommandHeader { return s.modelHeader(prefix+suffix, s.domain+"/content") }
	prepared, e := s.modelContent.PrepareDerivation(ctx, caller, &v1.PrepareDerivationCommand{Header: ch(":prepare"), TaskId: a.TaskId, OperationId: a.OperationId, AttemptId: x.Attempt.Ref.Name, GeneratorVersion: "reference-model-output-v1", OutputKind: "MODEL_OUTPUT", MediaType: "application/octet-stream"})
	if e = modelReceipt(prepared, e); e != nil {
		return nil, e
	}
	d, e := s.modelContent.QueryDerivation(ctx, caller, prepared.ResultRef)
	if e != nil {
		return nil, e
	}
	// 实际输出派生同时捕获模型输入和供应商原始响应；二者都不能由模型自报。
	refs := []*v1.Ref{call.InputRef, raw.BodyRef}
	var response []byte
	for i, ref := range refs {
		var content *v1.Content
		if d.State == "PREPARED" || d.State == "COMPUTING" {
			content, e = s.modelContent.ReadDerivationInput(ctx, caller, &v1.ReadDerivationInputCommand{Header: ch(fmt.Sprintf(":read:%d", i)), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, InputRef: ref})
		} else {
			content, e = s.modelContent.Read(ctx, caller, ref)
		}
		if e != nil {
			return nil, modelPreparationError(e)
		}
		if i == 1 {
			response = command.ContentBytes(content)
		}
	}
	status, output, _ := command.ReferenceModelOutput(raw, response, x.Attempt)
	result.Status = status
	sealed, e := s.modelContent.SealDerivation(ctx, caller, &v1.SealDerivationCommand{Header: ch(":seal"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, InputRefs: refs})
	if e = modelReceipt(sealed, e); e != nil {
		return nil, e
	}
	committed, e := s.modelContent.CommitDerivation(ctx, caller, &v1.CommitDerivationCommand{Header: ch(":commit"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, Body: output})
	if e = modelReceipt(committed, e); e != nil {
		return nil, e
	}
	if e = s.modelContent.ProcessRegistrations(ctx, caller); e != nil {
		return nil, e
	}
	result.OutputRef = committed.ResultRef
	result.OutputDerivationRef = d.Ref
	return s.saveModelResult(ctx, caller, call, result)
}
func (s *Service) saveModelResult(ctx context.Context, caller *v1.Caller, call *v1.ModelCall, result *v1.ModelCallResult) (*v1.ModelCallResult, error) {
	observationID := "pending"
	if result.ObservationRef != nil {
		observationID = result.ObservationRef.Name.LocalId
	}
	r, e := s.traceModelDecisions(call.RequestRef).Execute(ctx, caller, s.modelHeader("model-result:"+call.Ref.Name.LocalId+":"+observationID, s.domain), command.SemanticFingerprint("model-result", result), "tasks.model_result", func(tx context.Context) (*v1.Ref, error) {
		current, e := s.store.LoadModelCall(tx, call.RequestRef, call.Position)
		if e != nil {
			return nil, e
		}
		if current.Result != nil && current.Result.Status != "UNKNOWN" {
			if result.Status == "UNKNOWN" || proto.Equal(current.Result, result) {
				return current.Ref, nil
			}
			return nil, command.Fail("MODEL_RESULT_CONFLICT")
		}
		current.Result = result
		return current.Ref, s.saveModelCall(tx, current)
	})
	if e = modelReceipt(r, e); e != nil {
		return nil, e
	}
	current, e := s.store.LoadModelCall(ctx, call.RequestRef, call.Position)
	if e != nil {
		return nil, e
	}
	return current.Result, nil
}
