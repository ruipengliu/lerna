package content

import (
	"context"
	contentlocal "lerna/adapters/content/local"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"os"
	"path/filepath"
	"testing"
)

func TestInvalidCredentialDoesNotRevealExistence(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	out, _, err := h.put(ctx, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	b := h.binding
	b.Token = "invalid"
	client := sdk.NewContentClient(contentlocal.Bind(h.service, b), "local")
	_, first := client.Call(ctx, &wire.ContentRequest{Method: "GET", Ref: out.Record.Ref, Purpose: "research"})
	_, second := client.Call(ctx, &wire.ContentRequest{Method: "GET", Ref: &wire.ContentRef{Namespace: "local", Key: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", Revision: 1}, Purpose: "research"})
	if artifacts.Code(first) != artifacts.Code(second) {
		t.Fatalf("existence leak %v vs %v", first, second)
	}
}
func TestLookupAndReplayReportMissingBody(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	out, in, err := h.put(ctx, []byte("abcdefghijklmnopqrstuvwxyz"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(h.root, "content", out.Record.Ref.Key)); err != nil {
		t.Fatal(err)
	}
	for _, r := range []*wire.ContentRequest{{Method: "GET", Ref: out.Record.Ref, Purpose: "research"}, {Method: "LOOKUP", OperationId: in.OperationId, Purpose: "research"}, in} {
		got, err := h.client.Call(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		if got.Record.State != "missing" {
			t.Fatalf("%s falsely reported %s", r.Method, got.Record.State)
		}
	}
}
