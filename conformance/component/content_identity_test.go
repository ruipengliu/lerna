//go:build integration

package component_test

import (
	"context"
	"encoding/base64"
	"errors"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"testing"
	"time"
)

func putContentRequest(t *testing.T, ctx context.Context, service interface {
	Put(context.Context, []byte, *v.SubjectBinding) (v.TransportOutcome, error)
}, request v.ContentPutRequest) v.CommandReceipt {
	t.Helper()
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
		t.Fatal("normal admission not durably received")
	}
	return received.Receipt
}
func assertContentBody(t *testing.T, ctx context.Context, service interface {
	Get(context.Context, []byte, *v.SubjectBinding) (v.ContentGetResponse, error)
}, ref v.ContentRef, part *v.ContentRange, want string) {
	t.Helper()
	result, err := service.Get(ctx, contentGetWire(t, ref, part), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	published, ok := result.AsPublished()
	if !ok {
		t.Fatal("permitted exact content not published")
	}
	bytes, err := base64.StdEncoding.DecodeString(published.BytesBase64)
	if err != nil || string(bytes) != want || published.ContentRef != ref {
		t.Fatal("public bytes differ from independent original")
	}
	wire, err := v.EncodeContentResponse(result, v.ContentGetPayload{ContentRef: ref, Purpose: "verification", Range: part})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.DecodeContentResponse(wire, v.ContentGetPayload{ContentRef: ref, Purpose: "verification", Range: part}); err != nil {
		t.Fatal(err)
	}
}
func TestContentCommandReplayAndExactVersionIdentities(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	request := contentPut(t, alphaRef, "original-alpha", "YWxwaGEK")
	first := putContentRequest(t, ctx, service, request)
	if _, ok := first.AsAccepted(); !ok {
		t.Fatal("normal alpha not accepted")
	}
	replay := request
	replay.TraceContext = &v.TraceContext{TraceID: "new-connection-trace"}
	second := putContentRequest(t, ctx, service, replay)
	a, _ := v.Encode(first)
	b, _ := v.Encode(second)
	if string(a) != string(b) {
		t.Fatal("trace-only original replay changed fixed receipt")
	}
	altered := request
	altered.Payload.Purpose = "other-purpose"
	raw, err := v.Encode(altered)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Put(ctx, raw, &contentPrincipal)
	var conflict *v.ContractError
	if !errors.As(err, &conflict) || conflict.Code != "idempotency_conflict" {
		t.Fatal("original Command accepted altered business meaning", err)
	}
	same := request
	same.CommandID = "another-command"
	another := putContentRequest(t, ctx, service, same)
	accepted, ok := another.AsAccepted()
	if !ok || accepted.CommandRef.CommandID != same.CommandID {
		t.Fatal("new command did not fix its own reference to original version")
	}
	declaration := request
	declaration.CommandID = "changed-retention"
	declaration.Payload.RetainUntil = v.Time(time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))
	rejection := putContentRequest(t, ctx, service, declaration)
	rejected, ok := rejection.AsRejected()
	if !ok || rejected.Reason != "version_conflict" {
		t.Fatal("new Command changed original version declaration")
	}
	beta := alphaRef
	beta.Version = "2"
	beta.ByteLength = "5"
	beta.Hash = "sha256:f2c82decdd7181cf98945929a62598db7e6b477e11f6e0eb0ae97020eff151ad"
	installContentPolicy(t, ctx, w, beta)
	version2 := contentPut(t, beta, "put-version2", "YmV0YQo=")
	if _, ok := putContentRequest(t, ctx, service, version2).AsAccepted(); !ok {
		t.Fatal("distinct exact version not independently accepted")
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	for i := 0; i < 2; i++ {
		processed, err := service.Step(ctx)
		if err != nil || !processed {
			t.Fatal("separate original versions lost durable publication responsibility", err)
		}
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	assertContentBody(t, ctx, service, beta, nil, "beta\n")
	assertExactContentObjects(t, w.Directory, map[v.ContentRef]string{alphaRef: "alpha\n", beta: "beta\n"})
	replayed := putContentRequest(t, ctx, service, request)
	c, _ := v.Encode(replayed)
	if string(c) != string(a) {
		t.Fatal("published replay rewrote original accepted")
	}
}
func TestContentEmptyBytesBinaryAndFiniteRanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	empty := v.ContentRef{Owner: contentOwner, ContentID: "empty", Version: "9223372036854775807", Hash: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", MediaType: "application/octet-stream", ByteLength: "0"}
	installContentPolicy(t, ctx, w, empty)
	binary := v.ContentRef{Owner: contentOwner, ContentID: "binary", Version: "7", Hash: "sha256:26a66b061e8f48f39927c312f25293959729eee95978e2892d49d3512a5cc092", MediaType: "application/octet-stream", ByteLength: "3"}
	installContentPolicy(t, ctx, w, binary)
	installContentPolicy(t, ctx, w, alphaRef)
	for _, input := range []struct {
		ref  v.ContentRef
		body string
	}{{empty, ""}, {binary, "AAH/"}, {alphaRef, "YWxwaGEK"}} {
		request := contentPut(t, input.ref, string(input.ref.ContentID), input.body)
		if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
			t.Fatal("valid finite/empty/binary input refused")
		}
		if _, err := service.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	assertContentBody(t, ctx, service, empty, nil, "")
	assertContentBody(t, ctx, service, empty, &v.ContentRange{Offset: "0", Length: "0"}, "")
	assertContentBody(t, ctx, service, binary, nil, string([]byte{0, 1, 255}))
	assertContentBody(t, ctx, service, alphaRef, &v.ContentRange{Offset: "1", Length: "3"}, "lph")
	assertContentBody(t, ctx, service, alphaRef, &v.ContentRange{Offset: "6", Length: "0"}, "")
	for _, part := range []*v.ContentRange{{Offset: "7", Length: "0"}, {Offset: "6", Length: "1"}, {Offset: "9223372036854775807", Length: "9223372036854775807"}} {
		view, err := service.Get(ctx, contentGetWire(t, alphaRef, part), &contentPrincipal)
		if err != nil {
			t.Fatal(err)
		}
		rejected, ok := view.AsRejected()
		if !ok || rejected.Reason != "range_invalid" {
			t.Fatal("out-of-bounds range silently truncated or overflowed")
		}
	}
}
