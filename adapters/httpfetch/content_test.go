package httpfetch_test

import (
	"context"
	"lerna/adapters/contentpolicy"
	"lerna/adapters/fetchcontent"
	"lerna/adapters/filecontent"
	"lerna/adapters/httpfetch"
	"lerna/adapters/sqlitefetch"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchedEvidenceRetainsRedirectProvenanceUnderCurrentContentPolicy(t *testing.T) {
	ctx := context.Background()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello"))
	}))
	defer server.Close()
	urls := []string{server.URL + "/start", server.URL + "/final"}
	authority, f := newAuthority(t, urls)
	adapter, err := httpfetch.New(httpfetch.Config{Authority: authority, URLs: urls, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Fetch(ctx, fetch.Request{URL: urls[0], MaxBytes: 64, MaxRequests: 2, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Sources, urls) {
		t.Fatalf("missing redirect provenance: %v", result.Sources)
	}
	actions := []string{"content.store", "content.process", "content.discover", "content.disclose", "content.retain"}
	until := time.Now().Add(5 * time.Minute).Unix()
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: actions, Purposes: []string{"answer"}, Locations: []string{"device"}, ExpiresUnix: until}
	for _, c := range []*wire.AuthorizationCommand{
		{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "content", Scope: scope}}}}},
		{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "content", Subject: "alice", Mode: "continuous", Scope: scope}}},
	} {
		if err = f.apply(c); err != nil {
			t.Fatal(err)
		}
	}
	rules := []contentpolicy.Rule{}
	mapping := map[string]*wire.ContentSource{}
	for i, key := range []string{"start", "final"} {
		rules = append(rules, contentpolicy.Rule{Kind: "web", Key: key, Revision: 1, Actions: []string{"store", "process", "discover", "disclose", "retain"}, Purposes: []string{"answer"}, Locations: []string{"device"}, RetainUntil: until})
		mapping[urls[i]] = &wire.ContentSource{Kind: "web", Key: key, Revision: 1}
	}
	policy, err := contentpolicy.New(rules)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := filecontent.Open(filepath.Join(t.TempDir(), "content"))
	if err != nil {
		t.Fatal(err)
	}
	defer blobs.Close()
	content, err := artifacts.New(f.service, blobs, policy, artifacts.Config{Inline: 16, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 32, MaxChunk: 65536, MaxFiles: 64, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := fetchcontent.New(content, artifacts.Binding{Token: f.token, Namespace: "local", Location: "device", Recipient: "device"}, fetchcontent.Config{Clock: authorization.SystemClock{}, Resource: "root", Purpose: "answer", RetainUntil: until, Sources: mapping})
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.service.NewOperation(ctx, f.token)
	if err != nil {
		t.Fatal(err)
	}
	// Readability of the source does not grant permission to retain it.
	limited := append([]contentpolicy.Rule(nil), rules...)
	limited[0].Actions = []string{"store", "process", "discover", "disclose"}
	if err = policy.Replace(limited); err != nil {
		t.Fatal(err)
	}
	if denied, e := evidence.Save(ctx, op, result); e != fetch.Denied || denied != "" {
		t.Fatalf("retention without authority: %v", e)
	}
	if err = policy.Replace(rules); err != nil {
		t.Fatal(err)
	}
	ref, err := evidence.Save(ctx, op, result)
	if err != nil {
		t.Fatal(err)
	}
	again, e := evidence.Save(ctx, op, result)
	if e != nil || again != ref {
		t.Fatalf("original PUT not idempotent: %v", e)
	}
	changed := result
	changed.FetchedAt = changed.FetchedAt.Add(-time.Second)
	if altered, e := evidence.Save(ctx, op, changed); e == nil || altered != "" {
		t.Fatal("original operation accepted replacement acquisition facts")
	}
	replay, err := evidence.Lookup(ctx, op)
	if err != nil || replay != ref {
		t.Fatalf("original save not recoverable: %v", err)
	}
	got, err := evidence.Read(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, result) || string(got.Body) != "hello" || got.SHA256 != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatal("stored evidence differs from actual acquisition")
	}
	// Restore independent fetch permissions alongside retention for recovery testing.
	scope.Actions = append(scope.Actions, "fetch.read", "fetch.process", "fetch.disclose")
	if err = f.apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "all", Scope: scope}}}}}); err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(t.TempDir(), "fetch.db")
	ledger, err := sqlitefetch.Open(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	saveOp, err := f.service.NewOperation(ctx, f.token)
	if err != nil {
		t.Fatal(err)
	}
	dropped, err := fetchcontent.New(lostPutReply{content}, artifacts.Binding{Token: f.token, Namespace: "local", Location: "device", Recipient: "device"}, fetchcontent.Config{Clock: authorization.SystemClock{}, Resource: "root", Purpose: "answer", RetainUntil: until, Sources: mapping})
	if err != nil {
		t.Fatal(err)
	}
	acquisition, err := fetch.NewAcquirer(adapter, ledger, dropped)
	if err != nil {
		t.Fatal(err)
	}
	intent := fetch.AttemptIntent{Task: tasks.Ref{Namespace: "local", TaskID: "original-task"}, Subject: "alice", OperationID: "original-fetch", Fingerprint: strings.Repeat("a", 64), MaxRequests: 2, TaskLimit: 2, EvidenceOperation: saveOp}
	before := hits.Load()
	_, known, err := acquisition.Acquire(ctx, intent, fetch.Request{URL: urls[0], MaxBytes: 64, MaxRequests: 2, Timeout: time.Second})
	if err == nil || known || hits.Load() != before+2 {
		t.Fatalf("lost PUT reply manufactured completion: known=%v err=%v", known, err)
	}
	ledger.Close()
	ledger, err = sqlitefetch.Open(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	acquisition, err = fetch.NewAcquirer(adapter, ledger, evidence)
	if err != nil {
		t.Fatal(err)
	}
	recovered, known, err := acquisition.Acquire(ctx, intent, fetch.Request{URL: urls[0], MaxBytes: 64, MaxRequests: 2, Timeout: time.Second})
	if err != nil || !known || recovered.Status != "acquired" || recovered.Requests != 2 || hits.Load() != before+2 {
		t.Fatalf("recovery repeated fetch or lost facts: %+v known=%v err=%v", recovered, known, err)
	}
	originalRef, err := evidence.Lookup(ctx, saveOp)
	if err != nil || originalRef != recovered.Reference {
		t.Fatal("recovery replaced original Content operation")
	}
	budget, err := ledger.Budget(ctx, intent.Task)
	if err != nil || budget.Charged != 2 {
		t.Fatalf("recovery reset budget: %+v %v", budget, err)
	}
	// A durable reservation without a saved result never grants redispatch.
	pending := intent
	pending.Task.TaskID = "interrupted-task"
	pending.OperationID = "interrupted-fetch"
	pending.EvidenceOperation, err = f.service.NewOperation(ctx, f.token)
	if err != nil {
		t.Fatal(err)
	}
	if fresh, e := ledger.Begin(ctx, pending); e != nil || !fresh {
		t.Fatalf("pending reservation: %v", e)
	}
	_, known, err = acquisition.Acquire(ctx, pending, fetch.Request{URL: urls[0], MaxBytes: 64, MaxRequests: 2, Timeout: time.Second})
	if err == nil || known || hits.Load() != before+2 {
		t.Fatal("unresolved original attempt dispatched a replacement")
	}
	// Revoke only the intermediate source; final-source permission cannot replace it.
	if err = policy.Replace(rules[1:]); err != nil {
		t.Fatal(err)
	}
	_, known, err = acquisition.Recover(ctx, intent)
	if err != fetch.Denied || known || hits.Load() != before+2 {
		t.Fatalf("recovery bypassed current source authority: %v", err)
	}
	stored, exists, e := ledger.Outcome(ctx, intent.Task.Namespace, intent.OperationID)
	if e != nil || !exists || stored != recovered {
		t.Fatal("revocation changed durable acquisition facts")
	}
	got, err = evidence.Read(ctx, ref)
	if err != fetch.Denied || len(got.Body) != 0 {
		t.Fatalf("revoked intermediate source released evidence: %v", err)
	}
}

// Fault injection only at the committed PUT acknowledgement boundary.
type lostPutReply struct{ content *artifacts.Service }

func (f lostPutReply) Call(ctx context.Context, b artifacts.Binding, in *wire.ContentRequest) (*wire.ContentResponse, error) {
	out, err := f.content.Call(ctx, b, in)
	if err == nil && in.Method == "PUT" {
		return nil, fetch.Unavailable
	}
	return out, err
}
