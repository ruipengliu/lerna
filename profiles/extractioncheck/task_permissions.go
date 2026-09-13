package extractioncheck

import (
	"context"
	"fmt"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"

	"google.golang.org/protobuf/proto"
)

// CheckTaskPermission revokes one phase permission after SDK admission. The
// real task, grant, source reader, candidate store and driver remain connected.
func CheckTaskPermission(ctx context.Context, action string) error {
	switch action {
	case "memory.source.read", "memory.extract", "memory.candidate.retain", "memory.candidate.disclose":
	default:
		return fmt.Errorf("unsupported permission case")
	}
	h, err := fresh(ctx)
	if err != nil {
		return err
	}
	defer h.destroy()
	request, material, err := h.request(ctx)
	if err != nil {
		return err
	}
	if _, err = h.client.Invoke(ctx, request, material); err != nil {
		return err
	}
	snapshot, err := h.db.Load(ctx)
	if err != nil {
		return err
	}
	scope := proto.Clone(snapshot.State.Rules[0].Scope).(*wire.AuthorizationScope)
	filtered := make([]string, 0, len(scope.Actions))
	for _, name := range scope.Actions {
		if name != action {
			filtered = append(filtered, name)
		}
	}
	if len(filtered) != len(scope.Actions)-1 {
		return fmt.Errorf("permission fixture did not contain one matching action")
	}
	scope.Actions = filtered
	op, err := h.operation(ctx)
	if err != nil {
		return err
	}
	_, err = h.auth.Execute(ctx, h.token, authorization.Mutation{
		Namespace: "local", OperationID: op,
		Command: &wire.AuthorizationCommand{ExpectedRevision: snapshot.State.Revision,
			Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{
				Rules: []*wire.PolicyRule{{Id: "phase-revoked", Scope: scope}},
			}}},
	})
	if err != nil {
		return err
	}
	outcome, err := h.exec.Run(ctx, request.OperationID)
	if err != nil {
		return err
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		return err
	}
	task, err := h.core.Get(ctx, h.token, request.Qualification.Ref)
	if err != nil {
		return err
	}
	if task.State == "COMPLETED" || outcome.Reference != "" || outcome.Result == "SUCCESS" {
		return fmt.Errorf("revoked phase published successful task output")
	}
	state, err := h.candidates.Inspect(ctx, "local", request.OperationID)
	if action == "memory.candidate.disclose" {
		if err != nil || !state.Committed || outcome.Effect != "CONFIRMED" || outcome.Result != "FAILURE" {
			return fmt.Errorf("disclosure denial lost confirmed retention fact: %v", err)
		}
	} else if err != memory.Missing || outcome.Effect == "CONFIRMED" {
		return fmt.Errorf("revoked phase retained candidate: %v", err)
	}
	return nil
}
