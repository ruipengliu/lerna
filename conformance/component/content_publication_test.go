//go:build integration

package component_test

import (
	"context"
	"encoding/base64"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var contentOwner = v.OwnerRef{TenantID: "content-tenant", OwnerID: "content-owner"}
var contentPrincipal = v.SubjectBinding{TenantID: contentOwner.TenantID, SubjectID: "verified-subject", DelegationChain: []v.DelegatedSubject{}}
var alphaRef = v.ContentRef{Owner: contentOwner, ContentID: "alpha", Version: "1", Hash: "sha256:b6a98d9ce9a2d9149288fa3df42d377c3e42737afdcdaf714e33c0a100b51060", MediaType: "text/plain", ByteLength: "6"}

func contentService(t *testing.T, w *fixture.World) *content.Service {
	t.Helper()
	s, err := content.New(content.Config{Owner: contentOwner, Store: w.Store(), Objects: w.Objects, Limits: content.Limits{MaxPreparingVersions: 16, MaxStagingBytes: 4 * 262144, Lease: time.Minute, WorkTimeout: 5 * time.Second}, PublishBudget: time.Minute, MaxPublicationAttempts: 3, Worker: "content-conformance"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func installContentPolicy(t *testing.T, ctx context.Context, w *fixture.World, ref v.ContentRef) {
	t.Helper()
	until := time.Now().UTC().Add(time.Hour)
	if err := w.Store().InstallFixturePolicy(ctx, content.FixturePolicy{Ref: ref, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: until, RetainUntil: until, Read: true, Process: true, Save: true, Disclose: true}, 0); err != nil {
		t.Fatal(err)
	}
	if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, until); err != nil {
		t.Fatal(err)
	}
}
func contentPut(t *testing.T, ref v.ContentRef, commandID string, bytes string) v.ContentPutRequest {
	t.Helper()
	until := v.Time(time.Now().UTC().Add(30 * time.Minute).Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))
	return v.ContentPutRequest{ContractVersion: v.Version, Profile: "content", CommandID: v.ID(commandID), Target: v.ContentTarget{TenantID: ref.Owner.TenantID, OwnerID: ref.Owner.OwnerID, Kind: "content", ID: ref.ContentID}, Method: "content.put", AcceptBefore: until, Payload: v.ContentPutPayload{ContentRef: ref, Sources: []v.ContentRef{}, Purpose: "verification", BytesBase64: bytes, RetainUntil: until}}
}
func contentGetWire(t *testing.T, ref v.ContentRef, part *v.ContentRange) []byte {
	t.Helper()
	request := v.ContentGetRequest{ContractVersion: v.Version, Profile: "content", CommandID: "read-content", Target: v.ContentTarget{TenantID: ref.Owner.TenantID, OwnerID: ref.Owner.OwnerID, Kind: "content", ID: ref.ContentID}, Method: "content.get", AcceptBefore: v.Time(time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z")), Payload: v.ContentGetPayload{ContentRef: ref, Purpose: "verification", Range: part}}
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func contentCommandGetWire(t *testing.T, id v.ID) []byte {
	t.Helper()
	ref := v.CommandRef{Owner: contentOwner, CommandID: id}
	raw, err := v.Encode(v.CommandGetRequest{ContractVersion: v.Version, Profile: "command", CommandID: "read-command", Target: v.CommandTarget{TenantID: contentOwner.TenantID, OwnerID: contentOwner.OwnerID, Kind: "command", ID: id}, Method: "command.get", AcceptBefore: v.Time(time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z")), Payload: v.CommandGetPayload{CommandRef: ref}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestContentCorrectBytesDurablyAccepted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.New(t, ctx)
	installContentPolicy(t, ctx, world, alphaRef)
	service := contentService(t, world)
	request := contentPut(t, alphaRef, "put-alpha", "YWxwaGEK")
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Put(ctx, raw, &contentPrincipal)
	if err != nil {
		t.Fatalf("correct finite bytes should be durably accepted: %v", err)
	}
	received, ok := got.AsReceived()
	if !ok {
		t.Fatal("normal put not durably received")
	}
	fixed, ok := received.Receipt.AsAccepted()
	if !ok {
		t.Fatal("correct finite bytes not accepted")
	}
	if fixed.ContentRef != alphaRef {
		t.Fatal("accepted wrong exact version")
	}
	preparing, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := preparing.AsPreparing(); !ok {
		t.Fatal("accepted content claimed published before object write")
	}
	world.Reopen(ctx)
	service = contentService(t, world)
	processed, err := service.Step(ctx)
	if err != nil || !processed {
		t.Fatalf("durable original publication: %v %v", processed, err)
	}
	view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	published, ok := view.AsPublished()
	if !ok {
		t.Fatalf("real publication unavailable: %+v", view)
	}
	bytes, err := base64.StdEncoding.DecodeString(published.BytesBase64)
	if err != nil || string(bytes) != "alpha\n" || published.ContentRef != alphaRef {
		t.Fatal("published bytes differ from independent original alpha bytes")
	}
	entries, err := os.ReadDir(world.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("normal install left %d entries", len(entries))
	}
	independent, err := os.ReadFile(filepath.Join(world.Directory, entries[0].Name()))
	if err != nil || string(independent) != "alpha\n" {
		t.Fatal("independent filesystem bytes differ")
	}
	world.Reopen(ctx)
	service = contentService(t, world)
	original, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := original.AsFound()
	if !ok {
		t.Fatal("reopened original receipt unavailable")
	}
	prior, _ := v.Encode(received.Receipt)
	current, _ := v.Encode(found.Receipt)
	if string(prior) != string(current) {
		t.Fatal("publication changed fixed accepted")
	}
	progress, ok := found.Progress.AsContent()
	if !ok || progress.ContentRef != alphaRef || progress.Publication != "published" {
		t.Fatal("new command reader missed exact durable progress")
	}
	view, err = service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok = view.AsPublished(); !ok {
		t.Fatal("real objects and PG did not survive reopen")
	}
}

func TestContentCurrentSavePolicyStopsObjectWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	request := contentPut(t, alphaRef, "save-before-revoke", "YWxwaGEK")
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	out, err := service.Put(ctx, raw, &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := out.AsReceived()
	if !ok {
		t.Fatal("normal prepared content absent")
	}
	if _, ok = received.Receipt.AsAccepted(); !ok {
		t.Fatal("normal save policy did not accept")
	}
	until := time.Now().UTC().Add(time.Hour)
	if err = w.Store().InstallFixturePolicy(ctx, content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: until, RetainUntil: until, Read: true, Process: true, Save: false, Disclose: true}, 1); err != nil {
		t.Fatal(err)
	}
	processed, err := service.Step(ctx)
	if err != nil || !processed {
		t.Fatalf("original responsibility not closed: %v %v", processed, err)
	}
	result, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	failed, ok := result.AsFailed()
	if !ok || failed.Reason != "forbidden" {
		t.Fatal("current denied save reported usable publication")
	}
	entries, err := os.ReadDir(w.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("current save denial still wrote real object bytes")
	}
	original, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := original.AsFound()
	if !ok {
		t.Fatal("denial removed accepted receipt")
	}
	prior, _ := v.Encode(received.Receipt)
	fixed, _ := v.Encode(found.Receipt)
	if string(prior) != string(fixed) {
		t.Fatal("current policy rewrote fixed accepted")
	}
}
