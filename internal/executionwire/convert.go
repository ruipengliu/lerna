package executionwire

import (
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
)

func Encode(r execution.Request) *wire.CapabilityInvocation {
	q := r.Qualification
	return &wire.CapabilityInvocation{ControlVersion: r.ControlVersion, OperationId: r.OperationID, Qualification: &wire.ExecutionQualification{Task: &wire.TaskRef{Namespace: q.Ref.Namespace, TaskId: q.Ref.TaskID}, Owner: q.Owner, Epoch: q.Epoch, Version: q.Version, WorkId: q.WorkID, Generation: q.Generation}, Capability: r.Capability, Version: r.Version, Implementation: r.Implementation, ImplementationVersion: r.ImplementationVersion, DescriptorSha256: r.DescriptorSHA256, InputRef: r.InputRef, ResourceVersion: r.ResourceVersion}
}
func Decode(r *wire.CapabilityInvocation) execution.Request {
	q := r.GetQualification()
	return execution.Request{ControlVersion: r.GetControlVersion(), OperationID: r.GetOperationId(), Qualification: tasks.Qualification{Ref: tasks.Ref{Namespace: q.GetTask().GetNamespace(), TaskID: q.GetTask().GetTaskId()}, Owner: q.GetOwner(), Epoch: q.GetEpoch(), Version: q.GetVersion(), WorkID: q.GetWorkId(), Generation: q.GetGeneration()}, Capability: r.GetCapability(), Version: r.GetVersion(), Implementation: r.GetImplementation(), ImplementationVersion: r.GetImplementationVersion(), DescriptorSHA256: r.GetDescriptorSha256(), InputRef: r.GetInputRef(), ResourceVersion: r.GetResourceVersion()}
}
func Snapshot(r execution.Record) *wire.InvocationSnapshot {
	return &wire.InvocationSnapshot{ConflictingEvidence: r.ConflictingEvidence, CancelOperationId: r.CancelID, Invocation: Encode(r.Request), Revision: r.Revision, Started: r.Started, Phase: r.Phase, Result: r.Result, Effect: r.Effect, Reference: r.Reference, Checks: r.Checks, AppliedRevision: r.Applied}
}
func Record(r *wire.InvocationSnapshot) execution.Record {
	return execution.Record{ConflictingEvidence: r.GetConflictingEvidence(), CancelID: r.GetCancelOperationId(), Request: Decode(r.GetInvocation()), Revision: r.GetRevision(), Started: r.GetStarted(), Phase: r.GetPhase(), Result: r.GetResult(), Effect: r.GetEffect(), Reference: r.GetReference(), Checks: r.GetChecks(), Applied: r.GetAppliedRevision()}
}

func Resource(r execution.ResourceRef) *wire.ControlledResourceRef {
	return &wire.ControlledResourceRef{Namespace: r.Namespace, Kind: r.Kind, Key: r.Key}
}
func ResourceRef(r *wire.ControlledResourceRef) execution.ResourceRef {
	return execution.ResourceRef{Namespace: r.GetNamespace(), Kind: r.GetKind(), Key: r.GetKey()}
}
func ResourceReceipt(r execution.ResourceControlReceipt) *wire.ResourceControlReceipt {
	return &wire.ResourceControlReceipt{OperationId: r.OperationID, Resource: Resource(r.Resource), Version: r.Version}
}
func ResourceSnapshot(r execution.ResourceControl) *wire.ResourceControlSnapshot {
	return &wire.ResourceControlSnapshot{Resource: Resource(r.Resource), Version: r.Version, Intent: r.Intent, Progress: r.Progress, ResourceVersion: r.ResourceVersion, Blocking: uint32(r.Blocking), Checks: r.Checks, Limitation: r.Limitation}
}
