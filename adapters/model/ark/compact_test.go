package ark_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	arkmodel "lerna/adapters/model/ark"
	"lerna/brain"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCompactEvidenceBindsExactUTF8Citation(t *testing.T)  { checkCompactCitation(t, false) }
func TestCompactEvidenceRejectsInventedCitation(t *testing.T) { checkCompactCitation(t, true) }
func TestSummaryCitationPreservesSentenceAfter512Bytes(t *testing.T) {
	checkCompactCitation(t, false, true)
}
func checkCompactCitation(t *testing.T, bad bool, summary ...bool) {
	body := "桥 opened in 2001."
	role := "external-evidence"
	expectedEnd := 19
	if len(summary) > 0 && summary[0] {
		role = "external-search-evidence"
		body = strings.Repeat("Background ", 50) + "Go is statically typed. " + strings.Repeat("Next ", 220)
		expectedEnd = 573
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	page, _ := json.Marshal(map[string]any{"Status": "acquired", "Body": body, "SHA256": hash, "FetchedAt": "2026-01-01T00:00:00Z"})
	input := brain.Input{Goal: "When?", Blocks: []brain.Block{{Ref: "source-1", Role: role, Text: string(page)}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ResponseFormat struct {
				JSONSchema struct {
					Schema struct {
						Properties struct {
							Claims struct {
								Items struct {
									Properties struct {
										Citations struct {
											MinItems int `json:"minItems"`
										} `json:"citations"`
									} `json:"properties"`
								} `json:"items"`
							} `json:"claims"`
						} `json:"properties"`
					} `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.ResponseFormat.JSONSchema.Schema.Properties.Claims.Items.Properties.Citations.MinItems != 1 {
			t.Error("provider contract allows unsupported claims")
		}
		w.Header().Set("Content-Type", "application/json")
		raw := `{"status":"answerable","answer":"It opened in 2001.","scope":"This record.","claims":[{"text":"Opened in 2001.","citations":[0]}],"gaps":[]}`
		if bad {
			raw = strings.Replace(raw, "[0]", "[1]", 1)
		}
		json.NewEncoder(w).Encode(map[string]any{"model": arkmodel.ModelID, "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": raw}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 60, "total_tokens": 160}})
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	m, err := arkmodel.New(arkmodel.Config{Model: arkmodel.ModelID, APIKey: "test-only", Timeout: time.Second, CompactEvidence: true}, roundTrip(func(r *http.Request) (*http.Response, error) {
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
	result, err := m.Generate(context.Background(), brain.Request{Contract: brain.EvidenceAnswerContract, Input: input, MaxInput: arkmodel.InputUpper, MaxOutput: 512})
	if bad {
		if err == nil || !result.Usage.Known || len(result.ProviderContent) == 0 {
			t.Fatal("invalid selection accepted or original response lost")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ProviderContent) == 0 {
		t.Fatal("provider output lost")
	}
	if err = brain.ValidateEvidenceAnswer(result.Content, input, 4096); err != nil {
		t.Fatal(err)
	}
	var answer brain.EvidenceAnswer
	json.Unmarshal(result.Content, &answer)
	c := answer.Claims[0].Citations[0]
	if c.Start != 0 || c.End != expectedEnd || c.Quote != body[:expectedEnd] || c.Source != "source-1" || c.SHA256 != hash {
		t.Fatalf("wrong exact citation: %+v", c)
	}
}
