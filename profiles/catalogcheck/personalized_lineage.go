package catalogcheck

import (
	"context"
	"fmt"
	"google.golang.org/protobuf/proto"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
)

// checkDerived runs after business completion. It checks actual controlled
// artifacts, then revokes only Memory disclosure while keeping content rights.
func (a *actionHost) checkDerived(ctx context.Context, run tasks.RunSnapshot, report *ActionReport) error {
	refs := map[string]bool{run.Task.Result: true}
	for _, d := range run.Actions.Decisions {
		if d.Record != nil && d.Record.Evidence != "" {
			refs[d.Record.Evidence] = true
		}
	}
	for _, x := range run.Actions.Actions {
		refs[x.InputRef] = true
	}
	for _, r := range run.ExecutionReports {
		refs[r.Reference] = true
	}
	h := a.h
	binding := artifacts.Binding{Token: h.token, Namespace: h.namespace, Location: "local", Recipient: "local"}
	for ref := range refs {
		parsed, e := answers.ParseReference(ref)
		if e != nil {
			return e
		}
		out, e := h.content.Call(ctx, binding, &wire.ContentRequest{Method: "GET", Ref: parsed, Purpose: "task"})
		if e != nil {
			return e
		}
		count := 0
		for _, source := range out.Record.Spec.Sources {
			if source.Kind == "memory" {
				count++
			}
		}
		if count != 1 || out.Record.Spec.RetainUntil > a.personal.lineage.RetainUntil {
			return fmt.Errorf("derived artifact lost Memory restrictions")
		}
		report.GovernedArtifacts++
	}
	scope := proto.Clone(a.personal.scope).(*wire.AuthorizationScope)
	scope.Actions = nil
	for _, action := range a.personal.scope.Actions {
		if action != "memory.disclose" {
			scope.Actions = append(scope.Actions, action)
		}
	}
	op, e := h.operation(ctx)
	if e != nil {
		return e
	}
	state, e := h.db.Load(ctx)
	if e != nil {
		return e
	}
	_, e = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: h.namespace, OperationID: op, Command: &wire.AuthorizationCommand{ExpectedRevision: state.State.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "personalized", Scope: scope}}}}}})
	if e != nil {
		return e
	}
	for ref := range refs {
		parsed, e := answers.ParseReference(ref)
		if e != nil {
			return e
		}
		_, e = h.content.Call(ctx, binding, &wire.ContentRequest{Method: "READ", Ref: parsed, Purpose: "task", Limit: 1})
		if e != artifacts.Error("PERMISSION_DENIED") {
			return fmt.Errorf("revoked derived artifact read: %v", e)
		}
		report.RevokedArtifacts++
	}
	return nil
}
