package memoryauth

import (
	"context"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

func admissionError(e error) error {
	switch {
	case authorization.Is(e, authorization.ResultOnly):
		return memory.ReplayUnavailable
	case authorization.Is(e, authorization.Expired):
		return memory.AdmissionExpired
	case authorization.Is(e, authorization.IdentityConflict):
		return memory.IdentityConflict
	case authorization.Is(e, authorization.Unavailable) || authorization.Is(e, authorization.OutcomeUnknown):
		return memory.Unavailable
	default:
		return memory.Denied
	}
}
func (a *Authority) Admit(ctx context.Context, b memory.Binding, id, semantic string, ref *wire.MemoryRef, spec *wire.MemorySpec, method string) (int64, error) {
	if method != "put" && method != "correct" {
		return 0, memory.Denied
	}
	if e := a.Check(ctx, b, ref, spec, method); e != nil {
		return 0, e
	}
	collection := a.collections[key{b.Namespace, ref.Collection}]
	admission, e := a.views.ReserveMemoryOperation(ctx, b.Token, id, semantic, &wire.AuthorizationAction{Resource: collection.Resource, Action: "memory." + method, Purpose: collection.Purpose, Location: b.Location})
	if e != nil {
		return 0, admissionError(e)
	}
	if admission.Subject != b.Subject || admission.SemanticSHA256 != semantic {
		return 0, memory.Denied
	}
	return admission.ExpiresUnixNano, nil
}
func (a *Authority) Inspect(ctx context.Context, b memory.Binding, id string) (memory.AdmissionState, error) {
	view, e := a.views.ViewActions(ctx, b.Token, nil)
	if e != nil || view.Identity.Subject != b.Subject || view.Identity.Namespace != b.Namespace {
		return memory.AdmissionState{}, memory.Denied
	}
	state, e := a.views.InspectMemoryOperation(ctx, b.Token, id)
	if e != nil {
		return memory.AdmissionState{}, admissionError(e)
	}
	return memory.AdmissionState{State: state.State, Reserved: state.Admission.Subject != ""}, nil
}
