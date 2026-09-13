package fetchcheck

import (
	"context"
	"encoding/json"
	"lerna/adapters/contentpolicy"
	"lerna/adapters/fetchoutput"
	"lerna/answers"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type observedFailureContent struct {
	content contentService
	calls   atomic.Int32
}

func (o *observedFailureContent) Call(ctx context.Context, b artifacts.Binding, r *wire.ContentRequest) (*wire.ContentResponse, error) {
	o.calls.Add(1)
	return o.content.Call(ctx, b, r)
}
func TestFailureFactsReuseStillRequiresCurrentContentAuthority(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	raw, _ := json.Marshal(map[string]any{"url": server.URL + "/start", "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000})
	request, grant, err := h.request(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, request, grant); err != nil {
		t.Fatal(err)
	}
	if _, err = h.exec.Run(ctx, request.OperationID); err != nil {
		t.Fatal(err)
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		t.Fatal(err)
	}
	invocation, err := h.client.GetInvocation(ctx, request.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	observed := &observedFailureContent{content: h.content}
	adapter, err := fetchoutput.New(observed, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, h.clock)
	if err != nil {
		t.Fatal(err)
	}
	reader := adapter.Failures(h.token, h.cap)
	first, err := reader.ReadFailure(ctx, invocation.Reference)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reader.ReadFailure(ctx, invocation.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.Status != "denied" || observed.calls.Load() != 3 {
		t.Fatalf("immutable facts reread or changed: calls=%d", observed.calls.Load())
	}
	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			out, err := reader.ReadFailure(ctx, invocation.Reference)
			if err != nil || out != first {
				t.Errorf("concurrent governed reuse: %v", err)
			}
		}()
	}
	group.Wait()
	if observed.calls.Load() != 7 {
		t.Fatal("concurrent reuse skipped a current authority read")
	}
	rules := []contentpolicy.Rule{}
	for _, key := range []string{"start", "final"} {
		rules = append(rules, contentpolicy.Rule{Kind: "web", Key: key, Revision: 1, Actions: []string{"discover"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(time.Hour).Unix()})
	}
	if err = h.policy.Replace(rules); err != nil {
		t.Fatal(err)
	}
	parsed, err := answers.ParseReference(invocation.Reference)
	if err != nil {
		t.Fatal(err)
	}
	visible, err := h.content.Call(ctx, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, &wire.ContentRequest{Method: "GET", Ref: parsed, Purpose: "task"})
	if err != nil || visible.GetRecord().GetState() != "available" {
		t.Fatal("test must preserve discovery while revoking processing/disclosure")
	}
	denied, err := reader.ReadFailure(ctx, invocation.Reference)
	if err == nil || denied.Status != "" || observed.calls.Load() != 8 {
		t.Fatal("cached failure bypassed current source revocation")
	}
	for i := range rules {
		rules[i].Actions = []string{"discover", "process", "disclose"}
	}
	if err = h.policy.Replace(rules); err != nil {
		t.Fatal(err)
	}
	h.clock.advance(10 * time.Minute)
	expired, err := reader.ReadFailure(ctx, invocation.Reference)
	if artifacts.Code(err) != "CONTENT_UNAVAILABLE" || expired.Status != "" {
		t.Fatalf("cached facts survived expired retained artifact: %v", err)
	}

}
