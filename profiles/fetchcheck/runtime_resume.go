package fetchcheck

import (
	"context"
	"lerna/adapters/tasklocal"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/brain"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"reflect"
	"time"
)

// ResumeResearch continues an original task whose acquisitions have settled and
// whose answer is pending. Saved output is recovered without generating again.
// Completed tasks are only queried.
// Other checkpoints remain unresolved; they never become a fresh task or a
// replacement model request. The caller supplies the original model contract.
func ResumeResearch(ctx context.Context, root string, model brain.Model, credentials ...SearchCredential) (answers.View, error) {
	if len(credentials) > 1 {
		return answers.View{}, fetch.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	h, manifest, ref, err := openRecordedResearch(ctx, root)
	if err != nil {
		return answers.View{}, err
	}
	defer h.close()
	if len(credentials) == 1 {
		h.searchCredential = credentials[0]
	}
	client := sdk.NewTaskClient(tasklocal.Bind(h.core, h.token), "local")
	query := func() (answers.View, error) {
		return answers.Query(ctx, client, ref, h.content, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, "task")
	}
	view, err := query()
	if err != nil || view.Task.State == "COMPLETED" {
		return view, err
	}
	if model == nil || !reflect.DeepEqual(model.Capabilities(), manifest.Model) {
		return answers.View{}, fetch.Invalid
	}
	run, err := h.core.Load(ctx, ref)
	if err != nil {
		return answers.View{}, err
	}
	if run.Actions == nil || run.Actions.AnswerQualification == nil {
		cfg := manifest.Config
		h.inputSources = []*wire.ContentSource{{Kind: "task-goal", Key: "inline", Revision: 1}}
		_, err := runPreparedResearch(ctx, h, researchRunSpec{Resume: ref, Goal: cfg.Goal, Query: cfg.Query, SearchEndpoint: cfg.SearchEndpoint, SearchConfig: searchProviderConfig{MaxBytes: cfg.SearchMaxBytes, Recipient: cfg.SearchRecipient, TimeoutMS: cfg.SearchTimeoutMS, MaxResults: cfg.SearchMaxResults}, Queries: cfg.MaxQueries, NetworkLimit: cfg.NetworkLimit, Steps: cfg.MaxSteps, ModelTokens: cfg.ModelTokens, AnswerModel: model})
		if err != nil {
			return answers.View{}, err
		}
		return query()
	}
	if run.Task.State != "RUNNING" {
		return view, brain.Error("RESULT_UNRESOLVED")
	}
	port, err := h.work.Actions(run.Actions.Limits)
	if err != nil {
		return answers.View{}, err
	}
	if _, err := processResearchAnswer(ctx, h, port, run, model, false, false, nil, len(run.Generations) != 0); err != nil {
		return answers.View{}, err
	}
	return query()
}
