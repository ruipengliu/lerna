package providers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
)

const validFail = `{"draft":{"kind":"fail","reason_local_id":"reason","reason_code":"insufficient_context"},"contents":[{"local_id":"reason","media_type":"text/plain","body":"需要澄清","disclosed_sources":[]}]}`

func TestClosedModelOutputCannotCreateAuthorityOrInvalidPublications(t *testing.T) {
	if _, err := providers.ParseGenerated([]byte(validFail)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(validFail, `"kind":"fail"`, `"kind":"fail","status":"success"`, 1),
		strings.Replace(validFail, `"kind":"fail"`, `"kind":"fail","grant_refs":[]`, 1),
		strings.Replace(validFail, `"contents":`, `"usage":[{"unit":"USD","value":"0"}],"contents":`, 1),
		strings.Replace(validFail, `"reason_code":"insufficient_context"`, `"reason_code":"one","reason_code":"two"`, 1),
		strings.Replace(validFail, `"reason_local_id":"reason"`, `"reason_local_id":"missing"`, 1),
		strings.Replace(validFail, `"media_type":"text/plain"`, `"media_type":"text/plain; charset=utf-8"`, 1),
		strings.Replace(validFail, `"disclosed_sources":[]`, `"disclosed_sources":[],"content_local_id":"not_published"`, 1),
		strings.Replace(validFail, `"draft":{`, `"budget":-1,"draft":{`, 1),
	} {
		if _, err := providers.ParseGenerated([]byte(bad)); err == nil {
			t.Fatalf("unclosed or impossible model output accepted: %s", bad)
		}
	}
}

func TestInvalidModelOutputRetainsActualUsageInsteadOfInventingFailureCharges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reply map[string]any
		_ = json.Unmarshal(providerReply(t), &reply)
		reply["choices"] = []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": strings.Replace(validFail, `"kind":"fail"`, `"kind":"fail","grant_refs":[]`, 1)}}}
		_, _ = w.Write(api.Raw(reply))
	}))
	defer server.Close()
	cfg := configuration(t, server.URL)
	cfg.BillingFinal = false
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	call := api.NewID("call")
	out, err := engine.Request(context.Background(), call, encoded(t, engine, cfg))
	if err != nil || len(out.Contents) != 0 || out.Draft.Kind != "" || len(out.Usage) != 1 || out.Usage[0] != (api.Amount{Unit: "USD", Value: "0.00024"}) || out.UsageFinal {
		t.Fatalf("keep actual usage and leave final bill unresolved: %+v %v", out, err)
	}
	view, err := engine.Call(context.Background(), call)
	if err != nil || view.FailureReason != "model_output_invalid" || view.Tokens.Total != 120 {
		t.Fatalf("original facts survive invalid proposal: %+v %v", view, err)
	}
}

func TestBoundedLargeOriginalReplySurvivesDatabaseEncodingAndRestore(t *testing.T) {
	text := strings.Repeat("a", 180000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		var reply map[string]any
		_ = json.Unmarshal(providerReply(t), &reply)
		reply["choices"] = []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": strings.Replace(validFail, "需要澄清", text, 1)}}}
		_, _ = w.Write(api.Raw(reply))
	}))
	defer server.Close()
	cfg := configuration(t, server.URL)
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	call := api.NewID("call")
	enc := encoded(t, engine, cfg)
	out, err := engine.Request(context.Background(), call, enc)
	if err != nil || len(out.Contents) != 1 || out.Contents[0].Body != text {
		t.Fatalf("bounded real reply failed durable storage: contents=%d %v", len(out.Contents), err)
	}
	restored, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restored.Lookup(context.Background(), call, enc)
	if err != nil || !api.Equal(out, recovered) {
		t.Fatalf("restore accurate large original: %v", err)
	}
}
