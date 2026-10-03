package memory_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
)

func TestMirrorKeepsFrozenPurposeLocationAndSingleCopyIdentity(t *testing.T) {
	f := newFixture(t)
	body := "本地介质准确镜像登记"
	ref := f.upload(t, body)
	transfer := api.NewID("transfer")
	in := memory.MirrorInput{TransferID: transfer, ContentRef: ref, SourceHolder: f.auth.Ref(f.scope.OwnerID), TargetHolder: f.auth.Ref(f.scope.OwnerID), Purpose: "memory.read", Location: "local", MaxBytes: ref.ByteLength, ExpiresAt: api.Time(time.Now().Add(time.Minute)), ReferenceIntentRef: f.scope.Ref(api.NewID("intent"), 1)}
	r := f.command(t, "content.mirror_reserve", ref.ContentID, nil, in)
	if r.Stage != "applied" {
		t.Fatalf("mirror reserve: %+v", r)
	}
	status, err := f.service.ReceiveTransferBytes(f.ctx, f.scope, f.auth, transfer, []byte(body))
	if err != nil || status.Phase != "ready" {
		t.Fatalf("mirror accurate bytes: %+v %v", status, err)
	}
	copyID := api.NewID("copy")
	complete := memory.CompleteMirrorInput{TransferID: transfer, CopyID: copyID, Purpose: "memory.query", Location: "local"}
	r = f.command(t, "content.mirror_complete", ref.ContentID, nil, complete)
	if r.Stage != "rejected" {
		t.Fatalf("changed original mirror purpose accepted: %+v", r)
	}
	complete.Purpose = in.Purpose
	r = f.command(t, "content.mirror_complete", ref.ContentID, nil, complete)
	if r.Stage != "applied" {
		t.Fatalf("complete original mirror: %+v", r)
	}
	complete.CopyID = api.NewID("copy")
	r = f.command(t, "content.mirror_complete", ref.ContentID, nil, complete)
	if r.Stage != "rejected" {
		t.Fatalf("second semantic copy from same original transfer accepted: %+v", r)
	}
}
