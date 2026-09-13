package sdk_test

import (
	"context"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/sdk"
	"strings"
	"testing"
)

type memoryTransport struct {
	change func(*wire.MemoryResponse)
	calls  int
}

func (t *memoryTransport) Exchange(_ context.Context, data []byte) ([]byte, error) {
	t.calls++
	in := new(wire.MemoryRequest)
	if e := proto.Unmarshal(data, in); e != nil {
		return nil, e
	}
	out := &wire.MemoryResponse{MessageId: "response", ReplyTo: in.MessageId, Namespace: in.Namespace, Code: "OK", Result: &wire.MemoryReadResult{Coverage: "complete", Records: []*wire.MemoryRecord{{Ref: in.Get.Ref, Revision: in.Get.Revision, PreviousRevision: in.Get.Revision - 1, OperationId: "original", Spec: &wire.MemorySpec{Purpose: in.Get.Purpose}}}}}
	if t.change != nil {
		t.change(out)
	}
	return proto.Marshal(out)
}
func TestMemoryClientRejectsMisassociatedAndExpandedReadResponses(t *testing.T) {
	request := &wire.MemoryRequest{Method: "GET", Get: &wire.MemoryGet{ReadId: "read", Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "format"}, Revision: 1, Purpose: "assist"}, GrantMaterial: "opaque"}
	cases := []struct {
		name   string
		change func(*wire.MemoryResponse)
	}{
		{"association", func(r *wire.MemoryResponse) { r.ReplyTo = "other" }},
		{"namespace", func(r *wire.MemoryResponse) { r.Result.Records[0].Ref.Namespace = "other" }},
		{"latest-for-history", func(r *wire.MemoryResponse) {
			r.Result.Records[0].Revision = 2
			r.Result.Records[0].PreviousRevision = 1
		}},
		{"error-body", func(r *wire.MemoryResponse) { r.Code = "PERMISSION_DENIED" }},
		{"extra-results", func(r *wire.MemoryResponse) {
			r.Result.Records = append(r.Result.Records, proto.Clone(r.Result.Records[0]).(*wire.MemoryRecord))
		}},
		{"unknown-code", func(r *wire.MemoryResponse) { r.Code = "secret-server-diagnostic"; r.Result = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			transport := &memoryTransport{change: c.change}
			client := sdk.NewMemoryClient(transport, "local")
			if _, e := client.Exchange(context.Background(), request); e != memory.Invalid || strings.Contains(e.Error(), "secret") {
				t.Fatalf("response error %v", e)
			}
		})
	}
}
func TestMemoryClientRejectsMixedRequestBeforeTransport(t *testing.T) {
	transport := new(memoryTransport)
	client := sdk.NewMemoryClient(transport, "local")
	if _, e := client.Exchange(context.Background(), &wire.MemoryRequest{Method: "PUT", Write: &wire.MemoryWrite{}, Query: &wire.MemoryQuery{}}); e != memory.Invalid || transport.calls != 0 {
		t.Fatalf("mixed request %v calls=%d", e, transport.calls)
	}
}
