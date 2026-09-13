package fetchcheck

import (
	"context"
	"fmt"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Protocol fixture at the model boundary, with the provider's large input
// reservation and an explicit non-local processing location. No external call.
type remoteResearchModel struct {
	calls int
	input uint64
}

func TestResearchRemoteModelCannotReadLocalFailure(t *testing.T) {
	ctx := context.Background()
	var h *harness
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			// Host setup for the next action, before this discovery request completes.
			// The copied search host and original task retain their goal-only inputs.
			h.inputSources = []*wire.ContentSource{{Kind: "web", Key: "start", Revision: 1}}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"results":[{"url":%q,"title":"Record","snippet":"Read record"}]}`, endpoint+"/page")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	endpoint = server.URL
	var err error
	h, err = fresh(ctx, []string{endpoint + "/search?q=query", endpoint + "/page"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	configureSearchRecipient(t, ctx, h, true, true)
	policy, err := h.policyStore.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range policy.Rules {
		if policy.Rules[i].Kind == "web" && policy.Rules[i].Key == "start" {
			policy.Rules[i].Locations = []string{"local"}
		}
	}
	if err := h.policy.Replace(policy.Rules); err != nil {
		t.Fatal(err)
	}
	h.inputSources = []*wire.ContentSource{{Kind: "task-goal", Key: "inline", Revision: 1}}
	model := &remoteResearchModel{}
	_, err = runPreparedResearch(ctx, h, researchRunSpec{Goal: "Read record", Query: "query", SearchEndpoint: endpoint + "/search", Queries: 128, NetworkLimit: 2, Steps: 3, AnswerModel: model, ModelTokens: 256 * 1024})
	if err == nil || model.calls != 0 {
		t.Fatalf("local failure disclosed to model: calls=%d err=%v", model.calls, err)
	}
}
func (m *remoteResearchModel) Capabilities() brain.Capabilities {
	return brain.Capabilities{Model: "remote-protocol", Version: "1", Location: "remote-search", Text: true, Structured: true, HardBounds: true, InputUpper: 224 * 1024, ContextTokens: 256 * 1024}
}
func (m *remoteResearchModel) Generate(ctx context.Context, r brain.Request) (brain.Result, error) {
	m.calls++
	m.input = r.MaxInput
	return (decisionFixtureModel{evidence: true}).Generate(ctx, r)
}

func TestPreparedResearchRoutesModelWithOriginalSourcePermissions(t *testing.T) {
	for _, permission := range []struct {
		name              string
		authority, source bool
	}{{"source_only", false, true}, {"authority_only", true, false}, {"both", true, true}} {
		t.Run(permission.name, func(t *testing.T) {
			ctx := context.Background()
			var endpoint string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/search" {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `{"results":[{"url":%q,"title":"Record","snippet":"Read record"}]}`, endpoint+"/page")
				} else {
					w.Header().Set("Content-Type", "text/plain")
					w.Write([]byte("The public record opened in 2001."))
				}
			}))
			defer server.Close()
			endpoint = server.URL
			h, err := fresh(ctx, []string{endpoint + "/search?q=query", endpoint + "/page"})
			if err != nil {
				t.Fatal(err)
			}
			defer h.destroy()
			configureSearchRecipient(t, ctx, h, permission.authority, permission.source)
			model := &remoteResearchModel{}
			record, err := runPreparedResearch(ctx, h, researchRunSpec{Goal: "When did it open?", Query: "query", SearchEndpoint: endpoint + "/search", Queries: 128, NetworkLimit: 2, Steps: 3, AnswerModel: model, ModelTokens: 256 * 1024})
			if !permission.authority || !permission.source {
				if err == nil || model.calls != 0 {
					t.Fatalf("model received unauthorized evidence: calls=%d err=%v", model.calls, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if model.calls != 1 || model.input != 224*1024 || record.AnswerModel.Location != "remote-search" || record.Answer.Status != "answerable" || record.Usage.NetworkCharged != 2 {
				t.Fatalf("model route or original budget lost: calls=%d input=%d record=%+v", model.calls, model.input, record)
			}
		})
	}
}
