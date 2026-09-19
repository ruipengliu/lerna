package answer

import (
	"context"
	"encoding/json"
	arkmodel "lerna/adapters/model/ark"
	"lerna/answers"
	"lerna/brain"
	"lerna/conformance"
	"lerna/tasks"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RealReport struct {
	Build          conformance.Build      `json:"build"`
	ExecutedAt     time.Time              `json:"executed_at"`
	StateDirectory string                 `json:"state_directory"`
	InputFixture   string                 `json:"input_fixture"`
	Profile        string                 `json:"profile"`
	Status         string                 `json:"status"`
	Error          string                 `json:"error,omitempty"`
	Endpoint       string                 `json:"endpoint"`
	Capabilities   brain.Capabilities     `json:"capabilities"`
	Limits         tasks.GenerationLimits `json:"limits"`
	ActualRequests int                    `json:"actual_requests"`
	Usage          brain.Usage            `json:"last_usage"`
	Finish         string                 `json:"finish"`
	Task           tasks.Task             `json:"task"`
	Answer         *brain.Answer          `json:"answer,omitempty"`
	Availability   string                 `json:"availability"`
	Delivered      bool                   `json:"delivered"`
	Method         string                 `json:"method"`
	Limitations    []string               `json:"limitations"`
}
type observedModel struct {
	brain.Model
	report *RealReport
}

func (m observedModel) Generate(ctx context.Context, in brain.Request) (brain.Result, error) {
	m.report.ActualRequests++
	out, e := m.Model.Generate(ctx, in)
	m.report.Usage = out.Usage
	m.report.Finish = out.Finish
	return out, e
}

// Real executes exactly one budgeted generation, with no format repair or model
// fallback. The caller supplies a private empty state directory and credentials.
func Real(ctx context.Context, root, key string) RealReport {
	l := tasks.GenerationLimits{Requests: 1, InputTokens: arkmodel.InputUpper, OutputTokens: 512}
	r := RealReport{Build: conformance.BuildInfo(), ExecutedAt: time.Now().UTC(), StateDirectory: root, InputFixture: publicFact, Profile: "real-answer-v1", Status: "not_run", Endpoint: arkmodel.Endpoint, Limits: l, Method: "answercheck -env-file .env (one generation; public fixed materials)", Limitations: []string{"One public simple-answer check; not a general accuracy or API-selection benchmark.", "Input reservation uses the pinned provider input maximum, including protocol overhead; actual usage settles after response. No monetary-price guarantee.", "Completed task is Core admission; availability is current authorized read; no UI delivery acknowledgement.", "State directory contains local credentials and SQLite evidence; keep private, do not commit."}}
	m, e := arkmodel.New(arkmodel.Config{Model: arkmodel.ModelID, APIKey: key, Timeout: 40 * time.Second}, nil)
	if e != nil {
		r.Error = e.Error()
		return r
	}
	r.Capabilities = m.Capabilities()
	h, e := open(ctx, root, "", m.Capabilities().Location, l)
	if e != nil {
		r.Error = e.Error()
		return r
	}
	defer h.close()
	if e = os.WriteFile(filepath.Join(root, "credential"), []byte(h.token), 0600); e != nil {
		r.Error = "CREDENTIAL_STORE_UNAVAILABLE"
		return r
	}
	s, e := h.submit(ctx, l)
	if e != nil {
		r.Error = e.Error()
		return r
	}
	out, e := h.run(ctx, s, observedModel{m, &r})
	r.Task = out.Task
	r.Status = "failed"
	if e != nil {
		r.Error = e.Error()
		return r
	}
	if out.Task.State != "COMPLETED" {
		r.Error = out.Task.StopReason
		return r
	}
	v, e := answers.Query(ctx, h.client, s.Task.Ref, h.content, h.binding(), "task")
	if e != nil {
		r.Error = e.Error()
		return r
	}
	r.Availability = v.Availability
	r.Delivered = v.Delivered
	var a brain.Answer
	if e = json.Unmarshal(v.Body, &a); e != nil {
		r.Error = "OUTPUT_INVALID"
		return r
	}
	r.Answer = &a
	if !strings.Contains(a.Text, "Memory") || !strings.Contains(a.Text, "Brain") || !strings.Contains(a.Text, "Execution") || len(a.Sources) != 1 || a.Sources[0] != s.Task.InputRefs[0] {
		r.Error = "PUBLIC_FACT_CHECK_FAILED"
		return r
	}
	r.Status = "passed"
	return r
}
