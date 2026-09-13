package sdk_test

import (
	"context"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/sdk"
	"testing"
)

type deletionTransport struct{ change func(*wire.MemoryResponse) }

func (t deletionTransport) Exchange(_ context.Context, raw []byte) ([]byte, error) {
	in := new(wire.MemoryRequest)
	if err := proto.Unmarshal(raw, in); err != nil {
		return nil, err
	}
	out := &wire.MemoryResponse{MessageId: "reply", ReplyTo: in.MessageId, Namespace: "local", Code: "OK", Deletion: &wire.MemoryDeletionState{OperationId: "delete-original", Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "source"}, Purpose: "assist", State: "committed", Report: &wire.MemoryDeletionReport{Authority: "committed", Revision: 2, ChangePosition: 7, Local: []*wire.MemoryCleanupProgress{{Name: "local", State: "pending", Position: 6}}, Replicas: []*wire.MemoryCleanupProgress{{Name: "remote", State: "not_covered"}}, DerivedArchives: []*wire.MemoryCleanupProgress{{Name: "contexts", State: "applied", Position: 7}}}}}
	if t.change != nil {
		t.change(out)
	}
	return proto.Marshal(out)
}
func TestMemorySDKRejectsMisleadingDeletionReports(t *testing.T) {
	request := &wire.MemoryRequest{Method: "DELETION_STATUS", DeletionQuery: &wire.MemoryDeletionQuery{OperationId: "delete-original", Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "source"}, Purpose: "assist"}}
	if _, err := sdk.NewMemoryClient(deletionTransport{}, "local").Exchange(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*wire.MemoryResponse)
	}{
		{"other-operation", func(r *wire.MemoryResponse) { r.Deletion.OperationId = "other" }},
		{"other-source", func(r *wire.MemoryResponse) { r.Deletion.Ref.Key = "other" }},
		{"other-purpose", func(r *wire.MemoryResponse) { r.Deletion.Purpose = "other" }},
		{"premature-applied", func(r *wire.MemoryResponse) { r.Deletion.Report.DerivedArchives[0].Position = 6 }},
		{"uncovered-with-progress", func(r *wire.MemoryResponse) { r.Deletion.Report.Replicas[0].Position = 7 }},
		{"omitted-replicas", func(r *wire.MemoryResponse) { r.Deletion.Report.Replicas = nil }},
		{"invented-completion", func(r *wire.MemoryResponse) { r.Deletion.Report.Local[0].State = "complete" }},
		{"unknown-with-report", func(r *wire.MemoryResponse) { r.Deletion.State = "unknown" }},
		{"failure-with-report", func(r *wire.MemoryResponse) { r.Code = "PERMISSION_DENIED" }},
		{"mixed-result", func(r *wire.MemoryResponse) { r.Result = &wire.MemoryReadResult{Coverage: "complete"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := sdk.NewMemoryClient(deletionTransport{tc.change}, "local").Exchange(context.Background(), request); err != memory.Invalid {
				t.Fatalf("accepted invalid report: %v", err)
			}
		})
	}
}
