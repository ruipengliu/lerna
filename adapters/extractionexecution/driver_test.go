package extractionexecution_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"lerna/adapters/extractionauth"
	"lerna/adapters/extractionexecution"
	"lerna/adapters/localextraction"
	"lerna/adapters/localextractionsource"
	"lerna/adapters/sqliteauth"
	"lerna/adapters/sqliteextraction"
	"lerna/authorization"
	"lerna/execution"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/tasks"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type clock struct{}

func (clock) Now() (time.Time, error) { return time.Unix(1900000000, 0), nil }

func TestLocalExecutionRetainsCandidateAndReconcilesOriginalEffect(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "source.txt")
	if err := os.WriteFile(path, []byte("回答时，我偏好简洁的说明。"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sqliteauth.Open(filepath.Join(dir, "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	auth, err := authorization.New(db, clock{}, authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.Bootstrap(ctx, "local", "alice")
	if err != nil {
		t.Fatal(err)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"memory.source.read", "memory.extract", "memory.candidate.retain", "memory.candidate.disclose"}, Purposes: []string{"assist"}, Locations: []string{"device-a"}, ExpiresUnix: 1900003600}
	for i, c := range []*wire.AuthorizationCommand{{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "extract", Scope: scope}}}}}, {Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "extract", Subject: "alice", Scope: scope, Mode: "continuous"}}}} {
		op, e := auth.NewOperation(ctx, token)
		if e != nil {
			t.Fatal(e)
		}
		c.ExpectedRevision = uint64(i)
		if _, e = auth.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: op, Command: c}); e != nil {
			t.Fatal(e)
		}
	}
	policy, err := extractionauth.New(auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "assist", Location: "device-a"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqliteextraction.Open(filepath.Join(dir, "candidates.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	b := memory.Binding{Token: token, Namespace: "local", Subject: "alice", Location: "device-a", Recipient: "device-a"}
	source, err := localextractionsource.New([]localextractionsource.Entry{{Ref: &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}, Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("回答时，我偏好简洁的说明。"))), Speaker: "alice", Method: "authenticated-note", Fragment: "paragraph:1", Restrictions: extraction.Restrictions{Storage: []string{"device-a"}, Processing: []string{"device-a"}, Recipients: []string{"device-a"}, Purposes: []string{"assist"}, RetainUntil: 1900001000}}}, clock{})
	if err != nil {
		t.Fatal(err)
	}
	d, err := extractionexecution.New(store, policy, source, localextraction.Rules{}, clock{}, b, "assist")
	if err != nil {
		t.Fatal(err)
	}
	call := execution.Call{Request: execution.Request{OperationID: "original", Qualification: tasks.Qualification{Ref: tasks.Ref{Namespace: "local", TaskID: "task"}}, InputRef: "original-input"}, Input: []byte(`{"sources":[{"kind":"note","key":"one","revision":1}]}`)}
	if err = d.Start(ctx, call); err != nil {
		t.Fatal(err)
	}
	got, err := d.Inspect(ctx, call)
	if err != nil || got.Effect != "CONFIRMED" || got.Result != "SUCCESS" {
		t.Fatalf("observation: %+v %v", got, err)
	}
	r, err := store.Lookup(ctx, "local", "original")
	if err != nil || r.Candidate.Value != "concise" || r.InvocationSHA256 != call.Request.Fingerprint() {
		t.Fatalf("candidate: %+v %v", r, err)
	}
	if err = d.Start(ctx, call); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("changed source"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = d.Inspect(ctx, call)
	if err != nil || got.Effect != "CONFIRMED" || got.Result != "FAILURE" || len(got.Output) != 0 {
		t.Fatalf("changed source released reference: %+v %v", got, err)
	}
	if err = os.WriteFile(path, []byte("回答时，我偏好简洁的说明。"), 0600); err != nil {
		t.Fatal(err)
	}
	altered := call
	altered.Request.InputRef = "replacement"
	if _, err = d.Inspect(ctx, altered); err != memory.IdentityConflict {
		t.Fatalf("changed invocation: %v", err)
	}
	if err = store.Retire(ctx, "local", "alice", "original"); err != nil {
		t.Fatal(err)
	}
	got, err = d.Inspect(ctx, call)
	if err != nil || got.Effect != "CONFIRMED" || got.Result != "FAILURE" || len(got.Output) != 0 {
		t.Fatalf("retired effect: %+v %v", got, err)
	}
}
