package fetchcheck

import (
	"context"
	"fmt"
	"lerna/fetch"
	"lerna/tasks"
	"reflect"
)

// Reopen only reads original identities and evidence; no acquisition or new
// evidence save is permitted as a substitute for the settled checkpoint.
func reopenResearchCheckpoint(ctx context.Context, prior *harness, ready tasks.RunSnapshot) (*harness, tasks.RunSnapshot, error) {
	outcomes := map[string]fetch.Outcome{}
	evidence := map[string]fetch.Result{}
	for _, action := range ready.Actions.Actions {
		out, known, err := prior.attempts.Outcome(ctx, ready.Task.Ref.Namespace, action.OperationID)
		if err != nil || !known {
			return nil, tasks.RunSnapshot{}, fmt.Errorf("checkpoint outcome missing")
		}
		outcomes[action.OperationID] = out
		if out.Status == "acquired" {
			content, err := prior.evidence.Read(ctx, out.Reference)
			if err != nil {
				return nil, tasks.RunSnapshot{}, err
			}
			evidence[out.Reference] = content
		}
	}
	budget, err := prior.attempts.Budget(ctx, ready.Task.Ref)
	if err != nil {
		return nil, tasks.RunSnapshot{}, err
	}
	prior.close()
	next, err := openWithAcquisition(ctx, prior.root, prior.token, prior.urls, prior.network, prior.pageMaxBytes)
	if err != nil {
		return nil, tasks.RunSnapshot{}, err
	}
	next.answerFromSearch = prior.answerFromSearch
	next.answerOutputTokens = prior.answerOutputTokens
	next.searchFormat = prior.searchFormat
	// Preserve the original page descriptor when classifying settled actions.
	// Reopening does not replace fixed-replay evidence with HTTP acquisition.
	next.cap = prior.cap
	valid := false
	defer func() {
		if !valid {
			next.close()
		}
	}()
	restored, err := next.core.Load(ctx, ready.Task.Ref)
	if err != nil {
		return nil, tasks.RunSnapshot{}, err
	}
	if !reflect.DeepEqual(restored.Actions, ready.Actions) || !reflect.DeepEqual(restored.ExecutionReports, ready.ExecutionReports) || restored.Task.ModelUsedRequests != ready.Task.ModelUsedRequests || restored.Task.ModelUsedTokens != ready.Task.ModelUsedTokens || restored.Task.ModelReservedRequests != 0 {
		return nil, tasks.RunSnapshot{}, fmt.Errorf("reopen changed settled actions, reports or model usage")
	}
	current, err := next.attempts.Budget(ctx, ready.Task.Ref)
	if err != nil || !reflect.DeepEqual(current, budget) {
		return nil, tasks.RunSnapshot{}, fmt.Errorf("reopen changed acquisition budget")
	}
	for operation, original := range outcomes {
		out, known, err := next.attempts.Outcome(ctx, ready.Task.Ref.Namespace, operation)
		if err != nil || !known || !reflect.DeepEqual(out, original) {
			return nil, tasks.RunSnapshot{}, fmt.Errorf("reopen changed original acquisition outcome")
		}
	}
	for ref, original := range evidence {
		out, err := next.evidence.Read(ctx, ref)
		if err != nil || !reflect.DeepEqual(out, original) {
			return nil, tasks.RunSnapshot{}, fmt.Errorf("reopen changed original content, digest or acquisition time")
		}
	}
	valid = true
	return next, restored, nil
}
