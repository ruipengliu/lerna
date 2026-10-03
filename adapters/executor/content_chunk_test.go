package executor

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
)

// 受信 Execution 内容 port 发布真实落盘账务字节，读取沿公开 Dispatcher；
// 不预置 Content 行、查询结果或来源证明，也不冒充云端 Task 链路。
func TestPublicDeviceContentChunksUseExactOutputBound(t *testing.T) {
	f := newDeviceFixture(t)
	want := bytes.Repeat([]byte("账务\n"), (2*ChunkBytes+19)/len("账务\n"))
	ref, err := (deviceContent{f.h}).Publish(f.ctx, f.h.Scope, f.b.Principal.Auth(), execution.Publication{
		ContentID: api.NewID("content"), MediaType: "application/json", Purpose: "execution_usage_proof", Location: "device",
		ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{},
	}, want)
	if err != nil {
		t.Fatal(err)
	}
	got := []byte{}
	var full ContentChunk
	for index := uint64(0); index < chunkCount(ref.ByteLength); index++ {
		chunk := deviceQuery[ContentChunk](t, f, "executor.content.get", ref.ContentID, ContentGet{ContentRef: ref, ChunkIndex: index})
		part, e := base64.StdEncoding.Strict().DecodeString(chunk.DataBase64)
		if e != nil || len(part) > ChunkBytes || chunk.ChunkCount != chunkCount(ref.ByteLength) {
			t.Fatalf("bounded original chunk %d: %v", index, e)
		}
		got = append(got, part...)
		if index == 0 {
			full = chunk
		}
	}
	if !bytes.Equal(got, want) || api.Hash(got) != ref.Hash || uint64(len(got)) != ref.ByteLength {
		t.Fatal("original multi-chunk bytes changed")
	}
	m, ok := f.h.Registry.Method("executor.content.get")
	if !ok {
		t.Fatal("public content method missing")
	}
	v, err := api.NewValidator(m.Contract.OutputSchema)
	if err != nil {
		t.Fatal(err)
	}
	// The published full chunk reaches the bound; one extra ASCII byte is refused
	// by the same closed contract used by Dispatcher and SDK response decoders.
	if len(full.DataBase64) != 4*((ChunkBytes+2)/3) {
		t.Fatal("test did not reach the actual maximum chunk")
	}
	full.DataBase64 = strings.Repeat("A", len(full.DataBase64)+1)
	if err = v.Validate(api.Raw(full)); !api.IsCode(err, "invalid_request") {
		t.Fatalf("oversized public output accepted: %v", err)
	}
}
