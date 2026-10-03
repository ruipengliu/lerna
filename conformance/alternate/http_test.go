package alternate_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

func TestIndependentMemoryActualGoHTTPEnvelopeAndOriginalReceipt(t *testing.T) {
	f := newFixture(t, "memory")
	transport := &harness.HTTPTransport{BaseURL: f.address, Token: f.token, HTTP: f.http}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	discovery, err := transport.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if discovery.MethodsDigest != f.discovery.MethodsDigest || discovery.IdentityScope != f.discovery.IdentityScope {
		t.Fatal("HTTP discovery changed the original component/identity contract")
	}
	journal, err := harness.OpenJournal(t.TempDir(), discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	client, err := harness.NewClient(transport, journal, discovery)
	if err != nil {
		t.Fatal(err)
	}
	query := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, QueryID: api.NewID("query"), Method: "memory.index.inspect", TargetID: f.owner, Payload: api.Raw(struct{}{})}
	raw, err := client.Query(ctx, query)
	if err != nil {
		t.Fatalf("actual Go HTTP query must decode the shared response envelope: %v", err)
	}
	var index memory.IndexStatus
	if err := api.Decode(raw, &index); err != nil || index.State != "ready" || index.ChangeHead != 0 {
		t.Fatalf("HTTP query lost the original Memory facts: %+v %v", index, err)
	}
	values := memory.PolicyValues{Subjects: []string{f.subject}, Purposes: []string{"content.write", "read"}, Locations: []string{"local"}, RetainUntil: api.Time(time.Now().Add(30 * time.Minute))}
	digest, err := api.Digest(values)
	if err != nil {
		t.Fatal(err)
	}
	ref := api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, CommandID: api.NewID("command"), Method: "content.policy.install", TargetID: ref.ComponentID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.Policy{PolicyRef: ref, Values: values, Revision: 1, State: "active"})}
	receipt, err := client.Send(ctx, command)
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("HTTP original policy/receipt: %+v %v", receipt, err)
	}
	original, err := client.Receipt(ctx, command.CommandID)
	if err != nil || !api.Equal(original, receipt) {
		t.Fatalf("HTTP receipt lookup changed the original decision: %+v %v", original, err)
	}
	query.QueryID, query.Method = api.NewID("query"), "memory.private.reset"
	if _, err := transport.Call(ctx, "query", api.Raw(query)); !api.IsCode(err, "unsupported") {
		t.Fatalf("HTTP error envelope must retain a bounded unsupported rejection: %v", err)
	}
}
