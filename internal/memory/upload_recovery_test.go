package memory_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
)

func TestRecoverUploadPublishesOriginalReadyBytesWithoutSupplyingBody(t *testing.T) {
	f := newFixture(t)
	body := []byte("原 HTTP 来源，恢复不得重新获取")
	ref := api.ContentRef{TenantID: f.scope.TenantID, OwnerID: f.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(body), ByteLength: uint64(len(body)), MediaType: "text/plain"}
	request := memory.PublicationRequest{ContentRef: ref, TransferID: api.NewID("transfer"), ReserveCommandID: api.NewID("command"), PutCommandID: api.NewID("command"), PolicyRef: f.policy.PolicyRef, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(time.Now().Add(30 * time.Minute)), TransferDeadline: api.Time(time.Now().Add(time.Minute))}
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: request.ReserveCommandID, Method: "content.upload_reserve", TargetID: ref.ContentID, ExpiresAt: request.TransferDeadline, Payload: api.Raw(memory.ReserveInput{TransferID: request.TransferID, ContentRef: ref, PolicyRef: request.PolicyRef, ProcessedSources: request.ProcessedSources, RetentionUntil: request.RetentionUntil, TransferDeadline: request.TransferDeadline})}
	r, err := f.dispatcher.Command(f.ctx, f.auth, api.Raw(c))
	if err != nil || r.Stage != "applied" {
		t.Fatalf("reserve: %+v %v", r, err)
	}
	if _, err = f.service.ReceiveTransferBytes(f.ctx, f.scope, f.auth, request.TransferID, body); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err := f.service.RecoverUpload(f.ctx, f.scope, f.auth, request)
		if err != nil || got != ref {
			t.Fatalf("recover ready: %+v %v", got, err)
		}
	}
	got, err := f.service.Read(f.ctx, f.scope, f.auth, ref, "task.goal")
	if err != nil || string(got) != "原 HTTP 来源，恢复不得重新获取" {
		t.Fatalf("original bytes: %q %v", got, err)
	}
	changed := request
	changed.TransferDeadline = api.Time(time.Now().Add(2 * time.Minute))
	if _, err = f.service.RecoverUpload(f.ctx, f.scope, f.auth, changed); !api.IsCode(err, "idempotency_conflict") {
		t.Fatalf("refreshed recovery identity: %v", err)
	}
}
