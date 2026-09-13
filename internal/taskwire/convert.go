package taskwire

import (
	"google.golang.org/protobuf/reflect/protoreflect"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
)

func Encode(t tasks.Task) *wire.TaskSnapshot {
	return &wire.TaskSnapshot{Ref: &wire.TaskRef{Namespace: t.Ref.Namespace, TaskId: t.Ref.TaskID}, InputFacts: encodeFacts(t.InputFacts), Goal: t.Goal, GoalRef: t.GoalRef, GuidanceRef: t.GuidanceRef, InputRefs: t.InputRefs, Constraints: &wire.TaskConstraints{MaxSteps: t.Constraints.MaxSteps, DeadlineUnix: t.Constraints.DeadlineUnix, ModelRequests: t.Constraints.ModelRequests, ModelTokens: t.Constraints.ModelTokens}, State: t.State, Version: t.Version, Owner: t.Owner, OwnerEpoch: t.OwnerEpoch, Attempts: t.Attempts, Result: t.Result, StopReason: t.StopReason, Control: &wire.TaskControl{Intent: t.Control.Intent, Progress: t.Control.Progress, OperationId: t.Control.OperationID, AcceptedAt: t.Control.AcceptedAt}, WaitingReasons: t.WaitingReasons, ModelUsedRequests: t.ModelUsedRequests, ModelReservedRequests: t.ModelReservedRequests, ModelUsedTokens: t.ModelUsedTokens, ModelReservedTokens: t.ModelReservedTokens}
}
func Decode(t *wire.TaskSnapshot) tasks.Task {
	return tasks.Task{Ref: tasks.Ref{Namespace: t.GetRef().GetNamespace(), TaskID: t.GetRef().GetTaskId()}, InputFacts: decodeFacts(t.GetInputFacts()), Goal: t.GetGoal(), GoalRef: t.GetGoalRef(), GuidanceRef: t.GetGuidanceRef(), InputRefs: t.GetInputRefs(), Constraints: tasks.Constraints{MaxSteps: t.GetConstraints().GetMaxSteps(), DeadlineUnix: t.GetConstraints().GetDeadlineUnix(), ModelRequests: t.GetConstraints().GetModelRequests(), ModelTokens: t.GetConstraints().GetModelTokens()}, State: t.GetState(), Version: t.GetVersion(), Owner: t.GetOwner(), OwnerEpoch: t.GetOwnerEpoch(), Attempts: t.GetAttempts(), Result: t.GetResult(), StopReason: t.GetStopReason(), Control: tasks.ControlState{Intent: t.GetControl().GetIntent(), Progress: t.GetControl().GetProgress(), OperationID: t.GetControl().GetOperationId(), AcceptedAt: t.GetControl().GetAcceptedAt()}, WaitingReasons: t.GetWaitingReasons(), ModelUsedRequests: t.GetModelUsedRequests(), ModelReservedRequests: t.GetModelReservedRequests(), ModelUsedTokens: t.GetModelUsedTokens(), ModelReservedTokens: t.GetModelReservedTokens()}
}
func Known(m protoreflect.Message) bool {
	if len(m.GetUnknown()) > 0 {
		return false
	}
	ok := true
	m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.Message() != nil {
			if f.IsList() {
				l := v.List()
				for i := 0; i < l.Len(); i++ {
					if !Known(l.Get(i).Message()) {
						ok = false
						break
					}
				}
			} else {
				ok = Known(v.Message())
			}
		}
		return ok
	})
	return ok
}

func EncodeControl(r tasks.ControlReceipt) *wire.ControlReceipt {
	return &wire.ControlReceipt{OperationId: r.OperationID, Ref: &wire.TaskRef{Namespace: r.Ref.Namespace, TaskId: r.Ref.TaskID}, Intent: r.Intent, Outcome: r.Outcome, Version: r.Version}
}
func DecodeControl(r *wire.ControlReceipt) tasks.ControlReceipt {
	return tasks.ControlReceipt{OperationID: r.GetOperationId(), Ref: tasks.Ref{Namespace: r.GetRef().GetNamespace(), TaskID: r.GetRef().GetTaskId()}, Intent: r.GetIntent(), Outcome: r.GetOutcome(), Version: r.GetVersion()}
}
