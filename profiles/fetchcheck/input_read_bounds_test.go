package fetchcheck

import (
	"context"
	"encoding/json"
	contentpolicy "lerna/adapters/content/policy"
	executioncontent "lerna/adapters/execution/content"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRepeatedExecutionInputReadRechecksAuthorityForEveryChunk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, err := fresh(ctx, []string{"https://example.test/start", "https://example.test/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	h.inputSources = []*wire.ContentSource{{Kind: "web", Key: "start", Revision: 1}}
	body, _ := json.Marshal(map[string]string{"text": strings.Repeat("x", 20000)})
	ref, err := h.put(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	observed := &observedFailureContent{content: h.content}
	reader := executioncontent.New(observed, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, h.clock)
	for i := 0; i < 2; i++ {
		got, err := reader.Read(ctx, h.token, ref, h.cap)
		if err != nil || string(got) != string(body) {
			t.Fatalf("input: %v", err)
		}
	}
	if observed.calls.Load() != 5 {
		t.Fatalf("two-chunk reads duplicated metadata: %d", observed.calls.Load())
	}
	var readers sync.WaitGroup
	for i := 0; i < 3; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			got, err := reader.WithContent(observed).Read(ctx, h.token, ref, h.cap)
			if err != nil || string(got) != string(body) {
				t.Errorf("concurrent input: %v", err)
			}
		}()
	}
	readers.Wait()
	if observed.calls.Load() != 11 {
		t.Fatal("shared read lengths suppressed actual chunk reads")
	}
	if err = h.policy.Replace([]contentpolicy.Rule{{Kind: "web", Key: "start", Revision: 1, Actions: []string{"discover"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(time.Hour).Unix()}}); err != nil {
		t.Fatal(err)
	}
	got, err := reader.Read(ctx, h.token, ref, h.cap)
	if err == nil || len(got) != 0 || observed.calls.Load() != 12 {
		t.Fatal("known input length bypassed current process/disclose permission")
	}
	if err = h.policy.Replace([]contentpolicy.Rule{{Kind: "web", Key: "start", Revision: 1, Actions: []string{"discover", "process", "disclose"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(time.Hour).Unix()}}); err != nil {
		t.Fatal(err)
	}
	different := h.cap
	different.Resource = "different"
	if got, err = reader.Read(ctx, h.token, ref, different); err == nil || len(got) != 0 {
		t.Fatal("cached length bypassed capability resource restriction")
	}
	h.clock.advance(10 * time.Minute)
	if got, err = reader.Read(ctx, h.token, ref, h.cap); err == nil || len(got) != 0 {
		t.Fatal("cached length bypassed input expiry")
	}

}
