package taskwire

import (
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
)

func encodeRef(r tasks.Ref) *wire.TaskRef {
	return &wire.TaskRef{Namespace: r.Namespace, TaskId: r.TaskID}
}
func decodeRef(r *wire.TaskRef) tasks.Ref {
	return tasks.Ref{Namespace: r.GetNamespace(), TaskID: r.GetTaskId()}
}
func EncodeUpdate(r tasks.UpdateRequest) *wire.TaskUpdate {
	return &wire.TaskUpdate{OperationId: r.OperationID, Ref: encodeRef(r.Ref), ExpectedVersion: r.ExpectedVersion, Intent: r.Intent, InputRefs: r.InputRefs, GoalRef: r.GoalRef, GuidanceRef: r.GuidanceRef}
}
func DecodeUpdate(r *wire.TaskUpdate) tasks.UpdateRequest {
	return tasks.UpdateRequest{OperationID: r.GetOperationId(), Ref: decodeRef(r.GetRef()), ExpectedVersion: r.GetExpectedVersion(), Intent: r.GetIntent(), InputRefs: r.GetInputRefs(), GoalRef: r.GetGoalRef(), GuidanceRef: r.GetGuidanceRef()}
}
func EncodeInput(r tasks.InputRequest) *wire.TaskInput {
	return &wire.TaskInput{OperationId: r.OperationID, Ref: encodeRef(r.Ref), ExpectedVersion: r.ExpectedVersion, InteractionId: r.InteractionID, AnswerRef: r.AnswerRef}
}
func DecodeInput(r *wire.TaskInput) tasks.InputRequest {
	return tasks.InputRequest{OperationID: r.GetOperationId(), Ref: decodeRef(r.GetRef()), ExpectedVersion: r.GetExpectedVersion(), InteractionID: r.GetInteractionId(), AnswerRef: r.GetAnswerRef()}
}
func ev(v tasks.LimitValue) *wire.LimitValue {
	return &wire.LimitValue{Present: v.Present, Value: v.Value}
}
func dv(v *wire.LimitValue) tasks.LimitValue {
	return tasks.LimitValue{Present: v.GetPresent(), Value: v.GetValue()}
}
func EncodeAdjustment(r tasks.AdjustRequest) *wire.TaskLimitsAdjustment {
	return &wire.TaskLimitsAdjustment{OperationId: r.OperationID, Ref: encodeRef(r.Ref), ExpectedVersion: r.ExpectedVersion, Patch: &wire.TaskLimitPatch{Steps: ev(r.Patch.Steps), Requests: ev(r.Patch.Requests), Tokens: ev(r.Patch.Tokens), Deadline: ev(r.Patch.Deadline)}}
}
func DecodeAdjustment(r *wire.TaskLimitsAdjustment) tasks.AdjustRequest {
	p := r.GetPatch()
	return tasks.AdjustRequest{OperationID: r.GetOperationId(), Ref: decodeRef(r.GetRef()), ExpectedVersion: r.GetExpectedVersion(), Patch: tasks.LimitPatch{Steps: dv(p.GetSteps()), Requests: dv(p.GetRequests()), Tokens: dv(p.GetTokens()), Deadline: dv(p.GetDeadline())}}
}
func EncodeInputReceipt(r tasks.InputReceipt) *wire.TaskInputReceipt {
	return &wire.TaskInputReceipt{OperationId: r.OperationID, Ref: encodeRef(r.Ref), Kind: r.Kind, InteractionId: r.InteractionID, Version: r.Version}
}
func DecodeInputReceipt(r *wire.TaskInputReceipt) tasks.InputReceipt {
	return tasks.InputReceipt{OperationID: r.GetOperationId(), Ref: decodeRef(r.GetRef()), Kind: r.GetKind(), InteractionID: r.GetInteractionId(), Version: r.GetVersion()}
}
func EncodeInteractions(r tasks.InteractionPage) *wire.TaskInteractions {
	out := &wire.TaskInteractions{Ref: encodeRef(r.Ref), Version: r.Version}
	for _, v := range r.Interactions {
		q := v.Question
		out.Interactions = append(out.Interactions, &wire.TaskInteraction{Id: v.ID, QuestionRef: q.QuestionRef, Constraint: &wire.AnswerConstraint{Kind: q.Constraint.Kind, MaxBytes: q.Constraint.MaxBytes, Choices: q.Constraint.Choices}, Dependency: q.Dependency, DependencyRef: q.DependencyRef, Responder: q.Responder, ExpiresUnix: q.ExpiresUnix, State: v.State, AnswerRef: v.AnswerRef, AnswerOperation: v.AnswerOperation})
	}
	out.Inputs = encodeFacts(r.Inputs)
	return out
}
func DecodeInteractions(r *wire.TaskInteractions) tasks.InteractionPage {
	out := tasks.InteractionPage{Ref: decodeRef(r.GetRef()), Version: r.GetVersion()}
	for _, v := range r.GetInteractions() {
		c := v.GetConstraint()
		out.Interactions = append(out.Interactions, tasks.Interaction{ID: v.GetId(), Question: tasks.Question{QuestionRef: v.GetQuestionRef(), Constraint: tasks.AnswerConstraint{Kind: c.GetKind(), MaxBytes: c.GetMaxBytes(), Choices: c.GetChoices()}, Dependency: v.GetDependency(), DependencyRef: v.GetDependencyRef(), Responder: v.GetResponder(), ExpiresUnix: v.GetExpiresUnix()}, State: v.GetState(), AnswerRef: v.GetAnswerRef(), AnswerOperation: v.GetAnswerOperation()})
	}
	out.Inputs = decodeFacts(r.GetInputs())
	return out
}

func encodeFacts(in []tasks.InputFact) []*wire.TaskInputFact {
	out := []*wire.TaskInputFact{}
	for _, v := range in {
		out = append(out, &wire.TaskInputFact{OperationId: v.OperationID, Subject: v.Subject, Kind: v.Kind, Reference: v.Reference, InteractionId: v.InteractionID, QuestionRef: v.QuestionRef})
	}
	return out
}
func decodeFacts(in []*wire.TaskInputFact) []tasks.InputFact {
	out := []tasks.InputFact{}
	for _, v := range in {
		out = append(out, tasks.InputFact{OperationID: v.GetOperationId(), Subject: v.GetSubject(), Kind: v.GetKind(), Reference: v.GetReference(), InteractionID: v.GetInteractionId(), QuestionRef: v.GetQuestionRef()})
	}
	return out
}
