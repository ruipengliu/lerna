package ark_test

import (
	"context"
	"encoding/json"
	"io"
	arkmodel "lerna/adapters/model/ark"
	"lerna/brain"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestArkRequestAndNoHiddenRetry(t *testing.T) {
	for _, status := range []int{200, 429, 307} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			m, e := arkmodel.New(arkmodel.Config{Model: arkmodel.ModelID, APIKey: "test-only", Timeout: time.Second}, roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != arkmodel.Endpoint || r.Header.Get("Authorization") != "Bearer test-only" {
					t.Fatal("wrong binding")
				}
				var p map[string]any
				if e := json.NewDecoder(r.Body).Decode(&p); e != nil {
					t.Fatal(e)
				}
				if p["max_completion_tokens"] != float64(512) || p["stream"] != false || p["thinking"].(map[string]any)["type"] != "disabled" || p["tools"] != nil {
					t.Fatal("unbounded request")
				}
				body := `{"model":"` + arkmodel.ModelID + `","choices":[{"finish_reason":"stop","message":{"content":"{\"answer\":\"x\",\"sources\":[]}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
				return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://elsewhere.invalid/"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			}))
			if e != nil {
				t.Fatal(e)
			}
			out, e := m.Generate(context.Background(), brain.Request{Input: brain.Input{Goal: "public"}, MaxInput: arkmodel.InputUpper, MaxOutput: 512})
			if calls != 1 {
				t.Fatal("hidden retry")
			}
			if status == 200 {
				if e != nil || !out.Usage.Known || out.Usage.Input != 10 {
					t.Fatal(e)
				}
			} else if e == nil || strings.Contains(e.Error(), "test-only") {
				t.Fatal("unsafe failure")
			}
		})
	}
}
func TestArkRejectsAbnormalUsage(t *testing.T) {
	m, _ := arkmodel.New(arkmodel.Config{Model: arkmodel.ModelID, APIKey: "test-only", Timeout: time.Second}, roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"model":"` + arkmodel.ModelID + `","choices":[{"finish_reason":"stop","message":{"content":"{}"}}],"usage":{"prompt_tokens":10,"completion_tokens":513,"total_tokens":523}}`))}, nil
	}))
	out, e := m.Generate(context.Background(), brain.Request{MaxInput: arkmodel.InputUpper, MaxOutput: 512})
	if e == nil || out.Usage.Known {
		t.Fatal("abnormal usage accepted")
	}
}

func TestMissingUsageIsNeverKnownZero(t *testing.T) {
	for _, usage := range []string{`{"prompt_tokens":10,"Prompt_Tokens":0,"completion_tokens":0,"total_tokens":0}`, `{}`, `{"prompt_tokens":null,"completion_tokens":0,"total_tokens":0}`, `{"prompt_tokens":0,"total_tokens":0}`, `{"prompt_tokens":0,"completion_tokens":0}`, `null`} {
		t.Run(usage, func(t *testing.T) {
			m, _ := arkmodel.New(arkmodel.Config{Model: arkmodel.ModelID, APIKey: "test-only", Timeout: time.Second}, roundTrip(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"model":"` + arkmodel.ModelID + `","choices":[{"finish_reason":"stop","message":{"content":"{}"}}],"usage":` + usage + `}`))}, nil
			}))
			out, _ := m.Generate(context.Background(), brain.Request{MaxInput: arkmodel.InputUpper, MaxOutput: 512})
			if out.Usage.Known {
				t.Fatal("missing/null usage released unknown reservation")
			}
		})
	}
}

func TestArkActionContractIsExplicitAndOutputBounded(t *testing.T) {
	reached := false
	m, e := arkmodel.New(arkmodel.Config{Model: arkmodel.ModelID, APIKey: "test-only", Timeout: time.Second}, roundTrip(func(r *http.Request) (*http.Response, error) {
		reached = true
		var p struct {
			Max    uint64 `json:"max_completion_tokens"`
			Format struct {
				Schema struct {
					Name   string         `json:"name"`
					Schema map[string]any `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if e := json.NewDecoder(r.Body).Decode(&p); e != nil {
			t.Fatal(e)
		}
		if p.Max != 1024 || p.Format.Schema.Name != brain.ActionContract || p.Format.Schema.Schema["properties"].(map[string]any)["actions"] == nil {
			t.Fatal("wrong structured contract")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"model":"` + arkmodel.ModelID + `","choices":[{"finish_reason":"stop","message":{"content":"{}"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	_, e = m.Generate(context.Background(), brain.Request{Contract: brain.ActionContract, MaxInput: arkmodel.InputUpper, MaxOutput: 1024})
	if e != nil || !reached {
		t.Fatalf("action request: %v", e)
	}
}
