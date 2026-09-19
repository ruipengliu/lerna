package fetchcheck

import (
	"context"
	"fmt"
	contentpolicy "lerna/adapters/content/policy"
	"lerna/artifacts"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
)

type interruptedRuntimeModel struct {
	brain.Model
	cancel        context.CancelFunc
	metadataCalls int
	cancelResult  bool
	calls         int
}

func (m *interruptedRuntimeModel) Capabilities() brain.Capabilities {
	m.metadataCalls++
	if m.metadataCalls == 2 && m.cancel != nil {
		m.cancel()
	}
	return m.Model.Capabilities()
}
func (m *interruptedRuntimeModel) Generate(ctx context.Context, r brain.Request) (brain.Result, error) {
	m.calls++
	out, err := m.Model.Generate(ctx, r)
	if m.cancelResult {
		m.cancel()
	}
	return out, err
}

type interruptSavedAnswer struct {
	contentService
	cancel context.CancelFunc
	saved  bool
}

func (c *interruptSavedAnswer) Call(ctx context.Context, b artifacts.Binding, r *wire.ContentRequest) (*wire.ContentResponse, error) {
	out, err := c.contentService.Call(ctx, b, r)
	if err == nil && r.Method == "PUT" && r.GetSpec().GetKind() == "artifact" {
		c.saved = true
		c.cancel()
	}
	return out, err
}

func TestRuntimeRecoversSavedAnswerWithoutGeneratingAgain(t *testing.T) {
	for _, mode := range []string{"saved", "response", "revoked"} {
		t.Run(mode, func(t *testing.T) { checkRuntimeSavedAnswer(t, mode) })
	}
}
func TestRuntimeResumesUnstartedAnswerReservation(t *testing.T) {
	for _, mode := range []string{"reserved", "begun", "settled", "reserved-revoked"} {
		t.Run(mode, func(t *testing.T) { checkRuntimeSavedAnswer(t, mode) })
	}
}

func checkRuntimeSavedAnswer(t *testing.T, mode string) {
	ctx := context.Background()
	var hits atomic.Int32
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/search" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"results":[{"url":%q,"title":"Record","snippet":"Read record"}]}`, endpoint+"/page")
		} else {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "The record opened in 2001.")
		}
	}))
	defer server.Close()
	endpoint = server.URL
	root := t.TempDir()
	first, cancel := context.WithCancel(ctx)
	model := &interruptedRuntimeModel{Model: decisionFixtureModel{evidence: true}, cancel: cancel}
	cfg := RuntimeConfig{Goal: "Read record", Query: "record", SearchEndpoint: endpoint + "/search", SearchFormat: "json", SearchRecipient: "local", SearchMaxBytes: 4096, URLs: []string{endpoint + "/search?q=record", endpoint + "/page"}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true, MaxQueries: 128, NetworkLimit: 2, MaxSteps: 3, ModelTokens: 32768}
	_, err := RunResearch(first, root, cfg, model)
	cancel()
	if err == nil || model.calls != 0 || hits.Load() != 2 {
		t.Fatal("did not stop before original answer")
	}
	h, _, ref, err := openRecordedResearch(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	run, err := h.core.Load(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	port, err := h.work.Actions(run.Actions.Limits)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "reserved" || mode == "begun" || mode == "settled" || mode == "reserved-revoked" {
		ready, err := port.EnsureLease(ctx, tasks.QualificationOf(run))
		if err != nil {
			t.Fatal(err)
		}
		generations, err := h.work.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 2048, OutputTokens: 512})
		if err != nil {
			t.Fatal(err)
		}
		before, err := generations.WithOutputIdentities(h.operation).ReserveDecision(ctx, ready)
		if err != nil {
			t.Fatal(err)
		}
		if len(before.Generations) != 1 || before.Generations[0].Started != 0 || before.Generations[0].Settled || before.Task.ModelReservedRequests != 1 {
			t.Fatal("unstarted reservation not established")
		}
		switch mode {
		case "begun":
			if err := generations.BeginRequest(ctx, tasks.QualificationOf(before), 0); err != nil {
				t.Fatal(err)
			}
		case "settled":
			if err := generations.Settle(ctx, tasks.QualificationOf(before), tasks.GenerationUsage{}); err != nil {
				t.Fatal(err)
			}
		case "reserved-revoked":
			if err := h.policy.Replace([]contentpolicy.Rule{}); err != nil {
				t.Fatal(err)
			}
		}
		before, err = h.core.Load(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		h.close()
		model.cancel = nil
		view, err := ResumeResearch(ctx, root, model)
		if mode != "reserved" {
			if err == nil || len(view.Body) != 0 || model.calls != 0 || hits.Load() != 2 {
				t.Fatal("begun, settled or revoked reservation dispatched a model request")
			}
			restored, _, _, openErr := openRecordedResearch(ctx, root)
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer restored.close()
			after, loadErr := restored.core.Load(ctx, ref)
			if loadErr != nil || len(after.Generations) != 1 || after.Generations[0].OutputOperation != before.Generations[0].OutputOperation || after.Generations[0].Qualification != before.Generations[0].Qualification || after.Generations[0].Started != before.Generations[0].Started || after.Task.ModelUsedRequests != before.Task.ModelUsedRequests {
				t.Fatal("recovery replaced original reservation")
			}
			if mode != "reserved-revoked" && (after.Task.ModelReservedRequests != before.Task.ModelReservedRequests || after.Task.ModelReservedTokens != before.Task.ModelReservedTokens) {
				t.Fatal("recovery changed original unknown or settled accounting")
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if view.Task.Ref != ref || view.Task.State != "COMPLETED" || len(view.Body) == 0 || model.calls != 1 || hits.Load() != 2 || view.Task.ModelUsedRequests != before.Task.ModelUsedRequests+1 || view.Task.ModelReservedRequests != 0 {
			t.Fatal("original reservation was not used exactly once")
		}
		restored, _, _, err := openRecordedResearch(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		defer restored.close()
		after, err := restored.core.Load(ctx, ref)
		if err != nil || len(after.Generations) != 1 || after.Generations[0].OutputOperation != before.Generations[0].OutputOperation || after.Generations[0].Qualification != before.Generations[0].Qualification || after.Generations[0].Started != 1 {
			t.Fatal("original generation identity replaced")
		}
		return
	}
	writeCtx, stop := context.WithCancel(ctx)
	fault := &interruptSavedAnswer{contentService: h.content, cancel: stop}
	h.content = fault
	model.cancel = nil
	if mode == "response" {
		model.cancel = stop
		model.cancelResult = true
	}
	_, err = processResearchAnswer(writeCtx, h, port, run, model, nil, false)
	stop()
	if err == nil || fault.saved != (mode != "response") || model.calls != 1 {
		t.Fatalf("saved-output window not reached: %v", err)
	}
	before, err := h.core.Load(ctx, ref)
	if err != nil || len(before.Generations) != 1 || before.Task.State == "COMPLETED" {
		t.Fatal("original generation lost")
	}
	if mode == "revoked" {
		if err := h.policy.Replace([]contentpolicy.Rule{}); err != nil {
			t.Fatal(err)
		}
	}
	h.close()
	view, err := ResumeResearch(ctx, root, model)
	if mode != "saved" {
		if err == nil || len(view.Body) != 0 || model.calls != 1 || hits.Load() != 2 {
			t.Fatal("unknown or revoked result was regenerated or disclosed")
		}
		restored, _, _, openErr := openRecordedResearch(ctx, root)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer restored.close()
		current, loadErr := restored.core.Load(ctx, ref)
		if loadErr != nil || current.Task.State == "COMPLETED" || current.Task.ModelReservedRequests != before.Task.ModelReservedRequests || current.Task.ModelReservedTokens != before.Task.ModelReservedTokens || current.Task.ModelUsedRequests != before.Task.ModelUsedRequests || len(current.Generations) != 1 || current.Generations[0].OutputOperation != before.Generations[0].OutputOperation {
			t.Fatal("recovery reset original accounting or identity")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if view.Task.Ref != ref || view.Task.State != "COMPLETED" || len(view.Body) == 0 || model.calls != 1 || hits.Load() != 2 || view.Task.ModelUsedRequests != before.Task.ModelUsedRequests || view.Task.ModelReservedRequests != before.Task.ModelReservedRequests {
		t.Fatal("recovery replaced saved answer or original usage")
	}
}
