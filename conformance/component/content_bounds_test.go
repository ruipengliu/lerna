//go:build integration

package component_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContentDecodedByteBoundHasRealNormalPublication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	ref := alphaRef
	ref.ContentID = "largest-body"
	ref.ByteLength = "262144"
	ref.Hash = "sha256:dd3dde87623d9a6b354c68c943d189c89c63652d945e7bbdf0986cae91a49521"
	ref.MediaType = "application/octet-stream"
	installContentPolicy(t, ctx, w, ref)
	service := contentService(t, w)
	original := strings.Repeat("a", 262144)
	request := contentPut(t, ref, "largest-normal", base64.StdEncoding.EncodeToString([]byte(original)))
	oversized := request
	oversized.CommandID = "oversized-decoded"
	oversized.Payload.BytesBase64 = base64.StdEncoding.EncodeToString([]byte(original + "a"))
	oversized.Payload.ContentRef.ByteLength = "262145"
	raw, err := json.Marshal(oversized)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Put(ctx, raw, &contentPrincipal)
	var refused *v.ContractError
	if !errors.As(err, &refused) || refused.Code != "input_over_limit" {
		t.Fatal("public oversized decoded body not classified", err)
	}
	command, err := service.GetCommand(ctx, contentCommandGetWire(t, oversized.CommandID), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := command.AsNotFound(); !ok {
		t.Fatal("invalid format body fixed an original receipt")
	}
	if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
		t.Fatal("largest exact normal refused")
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, ref, nil, original)
	assertContentBody(t, ctx, service, ref, &v.ContentRange{Offset: "262144", Length: "0"}, "")
	entries, err := os.ReadDir(w.Directory)
	if err != nil || len(entries) != 1 {
		t.Fatal("bounded publication did not install exactly one object", err)
	}
	bytes, err := os.ReadFile(filepath.Join(w.Directory, entries[0].Name()))
	if err != nil || string(bytes) != original {
		t.Fatal("native bytes truncated at decoded bound", err)
	}
}
