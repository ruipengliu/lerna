package auth_test

import (
	"context"
	"google.golang.org/protobuf/proto"
	memorylocal "lerna/adapters/memory/local"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/sdk"
	"testing"
)

func checkGovernedDeletion(t *testing.T, service *memory.Service, reader *memory.Reader, auth *authorization.Service, b memory.Binding, spec *wire.MemorySpec, scope *wire.AuthorizationScope, change func(*wire.AuthorizationCommand), reporter memory.DeletionReporting) {
	t.Helper()
	ctx := context.Background()
	next := func() string {
		op, e := auth.NewOperation(ctx, b.Token)
		if e != nil {
			t.Fatal(e)
		}
		return op
	}
	ref := &wire.MemoryRef{Namespace: b.Namespace, Collection: "personal", Key: "delete-victim"}
	write := &wire.MemoryWrite{OperationId: next(), Ref: ref, Spec: proto.Clone(spec).(*wire.MemorySpec)}
	if _, e := service.Put(ctx, b, write); e != nil {
		t.Fatal(e)
	}
	in := memory.DeleteRequest{OperationID: next(), Ref: ref, ExpectedRevision: 1, Purpose: "assist"}
	denied := proto.Clone(scope).(*wire.AuthorizationScope)
	denied.Actions = nil
	for _, a := range scope.Actions {
		if a != "memory.delete" {
			denied.Actions = append(denied.Actions, a)
		}
	}
	replace := func(s *wire.AuthorizationScope) {
		change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "memory", Scope: s}}}}})
	}
	replace(denied)
	if _, e := service.Delete(ctx, b, in); e != memory.Denied {
		t.Fatalf("delete without permission: %v", e)
	}
	replace(scope)
	wrong := in
	wrong.Purpose = "other"
	if _, e := service.Delete(ctx, b, wrong); e != memory.Denied {
		t.Fatalf("delete changed purpose: %v", e)
	}
	client := sdk.NewMemoryClient(memorylocal.Bind(service, reader, b), b.Namespace)
	deleted, err := client.Exchange(ctx, &wire.MemoryRequest{Method: "DELETE", Delete: &wire.MemoryDelete{OperationId: in.OperationID, Ref: in.Ref, ExpectedRevision: in.ExpectedRevision, Purpose: in.Purpose}})
	if err != nil || deleted.GetReceipt().GetRevision() != 2 {
		t.Fatalf("SDK deletion: %+v %v", deleted, err)
	}
	receipt, e := service.Delete(ctx, b, in)
	if e != nil || receipt.Revision != 2 {
		t.Fatalf("authorized deletion: %+v %v", receipt, e)
	}
	repeat, e := service.Delete(ctx, b, in)
	if e != nil || repeat != receipt {
		t.Fatalf("delete repeat: %+v %v", repeat, e)
	}
	state, e := service.InspectOperation(ctx, b, in.OperationID)
	if e != nil || state.State != "committed" || state.ContentAvailability != "unavailable" {
		t.Fatalf("delete status: %+v %v", state, e)
	}
	query := memory.DeletionStatusRequest{OperationID: in.OperationID, Ref: ref, Purpose: "assist"}
	status, e := service.DeletionStatus(ctx, b, query)
	if e != nil || status.State != "committed" || status.Report == nil || status.Report.Authority != "committed" || status.Report.Replicas[0].State != "not_covered" {
		t.Fatalf("authorized deletion report: %+v %v", status, e)
	}
	wireQuery := &wire.MemoryRequest{Method: "DELETION_STATUS", DeletionQuery: &wire.MemoryDeletionQuery{OperationId: query.OperationID, Ref: query.Ref, Purpose: query.Purpose}}
	wireStatus, e := client.Exchange(ctx, wireQuery)
	if e != nil || wireStatus.GetDeletion().GetState() != "committed" || wireStatus.GetDeletion().GetReport().GetAuthority() != "committed" {
		t.Fatalf("SDK deletion status: %+v %v", wireStatus, e)
	}
	wrongStatus := query
	wrongStatus.Purpose = "other"
	if _, e = service.DeletionStatus(ctx, b, wrongStatus); e != memory.Denied {
		t.Fatalf("wrong purpose status disclosed: %v", e)
	}
	wrongStatus = query
	wrongStatus.Ref = &wire.MemoryRef{Namespace: "other", Collection: "personal", Key: ref.Key}
	if _, e = service.DeletionStatus(ctx, b, wrongStatus); e != memory.Denied {
		t.Fatalf("cross namespace status disclosed: %v", e)
	}
	wrongSubject := b
	wrongSubject.Subject = "other"
	if _, e = service.DeletionStatus(ctx, wrongSubject, query); e != memory.Denied {
		t.Fatalf("wrong subject status disclosed: %v", e)
	}
	// Revoke at the exact independent progress-read boundary. The service must
	// discard the fetched report, even though it was authorized at entry.
	racing, e := service.WithDeletionReporter(revokeAfterDeletionStatus{reporter: reporter, after: func() { replace(denied) }})
	if e != nil {
		t.Fatal(e)
	}
	if out, e := racing.DeletionStatus(ctx, b, query); e != memory.Denied || out.Report != nil {
		t.Fatalf("report escaped after revocation: %+v %v", out, e)
	}
	replace(scope)
	old, e := service.InspectOperation(ctx, b, write.OperationId)
	if e != nil || old.State != "committed" || old.ContentAvailability != "unavailable" || old.Receipt == nil || old.Receipt.SemanticSHA256 != "" {
		t.Fatalf("old status: %+v %v", old, e)
	}
	looked, err := client.Exchange(ctx, &wire.MemoryRequest{Method: "LOOKUP", OperationId: write.OperationId})
	if err != nil || looked.GetOperation().GetContentAvailability() != "unavailable" || looked.GetOperation().GetReceipt().GetSemanticSha256() != "" {
		t.Fatalf("SDK erased receipt: %+v %v", looked, err)
	}
	if _, err = client.Exchange(ctx, &wire.MemoryRequest{Method: "PUT", Write: write}); err != memory.ReplayUnavailable {
		t.Fatalf("SDK erased payload retry: %v", err)
	}
	if _, e = service.Put(ctx, b, write); e != memory.ReplayUnavailable {
		t.Fatalf("erased replay: %v", e)
	}
	replace(denied)
	if _, e = service.Delete(ctx, b, in); e != memory.Denied {
		t.Fatalf("revoked replay disclosed result: %v", e)
	}
	if _, e = service.InspectOperation(ctx, b, in.OperationID); e != memory.Denied {
		t.Fatalf("revoked status: %v", e)
	}
	if _, e = service.DeletionStatus(ctx, b, query); e != memory.Denied {
		t.Fatalf("revoked deletion report disclosed: %v", e)
	}
	if _, e = client.Exchange(ctx, wireQuery); e != memory.Denied {
		t.Fatalf("SDK revoked status: %v", e)
	}
	replace(scope)
}

// Fault injection is limited to policy mutation after the real progress read.
type revokeAfterDeletionStatus struct {
	reporter memory.DeletionReporting
	after    func()
}

func (r revokeAfterDeletionStatus) Status(ctx context.Context, e memory.SourceEvent) (memory.DeletionStatus, error) {
	result, err := r.reporter.Status(ctx, e)
	if err == nil {
		r.after()
	}
	return result, err
}
