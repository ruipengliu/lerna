package fetchcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	tasklocal "lerna/adapters/tasks/local"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/brain"
	"lerna/fetch"
	"lerna/sdk"
	"lerna/tasks"
)

type runtimeManifest struct {
	Version int
	Token   string
	Config  RuntimeConfig
	Model   brain.Capabilities
}

type runtimeTaskManifest struct {
	Version int
	Ref     tasks.Ref
}

// QueryResearch reopens the recorded task and queries its current publication
// under current authority. It does not execute work, load model credentials or
// reinstall grants. A missing checkpoint fails closed; it never submits again.
func QueryResearch(ctx context.Context, root string) (answers.View, error) {
	h, _, ref, err := openRecordedResearch(ctx, root)
	if err != nil {
		return answers.View{}, err
	}
	defer h.close()
	client := sdk.NewTaskClient(tasklocal.Bind(h.core, h.token), "local")
	return answers.Query(ctx, client, ref, h.content, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, "task")
}

func openRecordedResearch(ctx context.Context, root string) (*harness, runtimeManifest, tasks.Ref, error) {
	var host runtimeManifest
	var task runtimeTaskManifest
	for name, target := range map[string]any{"research-host.json": &host, "research-task.json": &task} {
		raw, err := readRunConfiguration(ctx, root, name)
		if err != nil {
			return nil, runtimeManifest{}, tasks.Ref{}, err
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
			return nil, runtimeManifest{}, tasks.Ref{}, fetch.Invalid
		}
	}
	if host.Version != 1 || host.Token == "" || task.Version != 1 || task.Ref.Namespace != "local" || task.Ref.TaskID == "" || host.Config.PageMaxBytes == 0 || host.Config.PageMaxBytes > 1<<20 {
		return nil, runtimeManifest{}, tasks.Ref{}, fetch.Invalid
	}
	cfg := host.Config
	if err := validateResearchDisclosure(host.Model, cfg.ModelTokens, cfg.DiscloseTo, researchOutputLimit(cfg.AnswerOutputTokens)); err != nil {
		return nil, runtimeManifest{}, tasks.Ref{}, err
	}
	h, err := openWithAcquisition(ctx, root, host.Token, cfg.URLs, networkConfig{Networks: cfg.Networks, AllowLoopbackHTTP: cfg.AllowLoopbackHTTP}, cfg.PageMaxBytes)
	if err != nil {
		return nil, runtimeManifest{}, tasks.Ref{}, err
	}
	h.answerFromSearch = cfg.AnswerFromSearch
	h.answerOutputTokens = cfg.AnswerOutputTokens
	h.searchFormat = cfg.SearchFormat
	return h, host, task.Ref, nil
}
