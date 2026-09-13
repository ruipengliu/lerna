package arkmodel_test

import (
	"context"
	"encoding/json"
	"lerna/adapters/arkmodel"
	"lerna/brain"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestArkEvidenceContractOverHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Max    uint64 `json:"max_completion_tokens"`
			Format struct {
				Schema struct {
					Name   string         `json:"name"`
					Strict bool           `json:"strict"`
					Schema map[string]any `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid request")
			w.WriteHeader(400)
			return
		}
		s := request.Format.Schema
		if request.Max != 4096 || s.Name != brain.EvidenceAnswerContract || !s.Strict {
			t.Error("wrong evidence envelope")
		}
		props, ok := s.Schema["properties"].(map[string]any)
		if !ok || props["claims"] == nil || props["gaps"] == nil || props["scope"] == nil || props["status"] == nil || s.Schema["additionalProperties"] != false {
			t.Error("missing evidence schema")
		}
		json.NewEncoder(w).Encode(map[string]any{"model": arkmodel.ModelID, "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"status":"insufficient","answer":"No acquired evidence.","scope":"This request only.","claims":[],"gaps":[{"kind":"insufficient","detail":"No evidence supplied.","sources":[]}]}`}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 600, "total_tokens": 610}})
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	model, err := arkmodel.New(arkmodel.Config{Model: arkmodel.ModelID, APIKey: "test-only", Timeout: time.Second}, roundTrip(func(r *http.Request) (*http.Response, error) {
		// Explicit test-only network routing; production endpoint remains pinned.
		r = r.Clone(r.Context())
		u := *r.URL
		u.Scheme = target.Scheme
		u.Host = target.Host
		r.URL = &u
		return transport.RoundTrip(r)
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := model.Generate(context.Background(), brain.Request{Contract: brain.EvidenceAnswerContract, MaxInput: arkmodel.InputUpper, MaxOutput: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || !result.Usage.Known || result.Usage.Output != 600 {
		t.Fatal("wrong response accounting")
	}
	if err = brain.ValidateEvidenceAnswer(result.Content, brain.Input{}, 8192); err != nil {
		t.Fatal(err)
	}
	for _, request := range []brain.Request{{Contract: brain.EvidenceAnswerContract, MaxInput: arkmodel.InputUpper, MaxOutput: arkmodel.ContextTokens - arkmodel.InputUpper + 1}, {Contract: "unrecognized", MaxInput: arkmodel.InputUpper, MaxOutput: 512}} {
		if _, err = model.Generate(context.Background(), request); err == nil {
			t.Fatal("accepted unsupported contract or limit")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("invalid request reached service")
	}
}
