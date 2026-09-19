package fetchcheck

import (
	"context"
	"crypto/sha256"
	"fmt"
	contentpolicy "lerna/adapters/content/policy"
	fetchcontent "lerna/adapters/research/content"
	"lerna/artifacts"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"sync"
	"testing"
	"time"
)

func TestRepeatedEvidenceReadRetainsAuthorityWithoutRepeatedMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url := "https://evidence.example/start"
	h, err := fresh(ctx, []string{url, "https://evidence.example/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	observed := &observedFailureContent{content: h.content}
	evidence, err := fetchcontent.New(observed, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, fetchcontent.Config{Clock: h.clock, Resource: "root", Purpose: "task", RetainUntil: h.now().Add(5 * time.Minute).Unix(), Sources: map[string]*wire.ContentSource{url: {Kind: "web", Key: "start", Revision: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("Actual retained evidence.")
	result := fetch.Result{RequestedURL: url, FinalURL: url, Sources: []string{url}, FetchedAt: h.now(), MediaType: "text/plain", Body: body, SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Mode: "fixed-replay"}
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := evidence.Save(ctx, op, result)
	if err != nil {
		t.Fatal(err)
	}
	observed.calls.Store(0)
	for i := 0; i < 2; i++ {
		bound, err := evidence.WithContent(observed)
		if err != nil {
			t.Fatal(err)
		}
		out, err := bound.Read(ctx, ref)
		if err != nil || string(out.Body) != string(body) {
			t.Fatalf("retained bytes: %v", err)
		}
	}
	if observed.calls.Load() != 2 {
		t.Fatalf("duplicate metadata observation: %d calls", observed.calls.Load())
	}
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			out, err := evidence.Read(ctx, ref)
			if err != nil || string(out.Body) != string(body) {
				t.Errorf("concurrent current read: %v", err)
			}
		}()
	}
	readers.Wait()
	if observed.calls.Load() != 6 {
		t.Fatal("concurrent reuse omitted a current content read")
	}
	if err = h.policy.Replace([]contentpolicy.Rule{{Kind: "web", Key: "start", Revision: 1, Actions: []string{"discover"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(time.Hour).Unix()}}); err != nil {
		t.Fatal(err)
	}
	out, err := evidence.Read(ctx, ref)
	if err == nil || len(out.Body) != 0 || observed.calls.Load() != 7 {
		t.Fatal("remembered read bounds bypassed current process/disclose permission")
	}
	if err = h.policy.Replace([]contentpolicy.Rule{{Kind: "web", Key: "start", Revision: 1, Actions: []string{"discover", "process", "disclose"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(time.Hour).Unix()}}); err != nil {
		t.Fatal(err)
	}
	h.clock.advance(10 * time.Minute)
	out, err = evidence.Read(ctx, ref)
	if err != fetch.Expired || len(out.Body) != 0 {
		t.Fatalf("remembered bounds lost governed expiry: %v", err)
	}
}
