package v1_2_test

import (
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"os"
	"testing"
)

func TestContentResponseBindsOriginalFullRefAndRange(t *testing.T) {
	wire, err := os.ReadFile("../../conformance/fixtures/1.2.0/put-alpha.json")
	if err != nil {
		t.Fatal(err)
	}
	input, err := v.DecodePut(wire)
	if err != nil {
		t.Fatal(err)
	}
	request := v.ContentGetPayload{ContentRef: input.Payload.ContentRef, Purpose: "verification", Range: &v.ContentRange{Offset: "1", Length: "3"}}
	response := v.NewContentGetResponsePublished(v.ContentGetResponsePublished{ContentRef: request.ContentRef, BytesBase64: "bHBo", Range: request.Range})
	encoded, err := v.EncodeContentResponse(response, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.DecodeContentResponse(encoded, request); err != nil {
		t.Fatal(err)
	}
	wrong := request
	wrong.ContentRef.Version = "2"
	if _, err = v.DecodeContentResponse(encoded, wrong); err == nil {
		t.Fatal("different exact version response accepted")
	}
	wrong = request
	wrong.ContentRef.MediaType = "application/octet-stream"
	if _, err = v.DecodeContentResponse(encoded, wrong); err == nil {
		t.Fatal("different full metadata response accepted")
	}
	wrong = request
	wrong.Range = &v.ContentRange{Offset: "2", Length: "3"}
	if _, err = v.DecodeContentResponse(encoded, wrong); err == nil {
		t.Fatal("different same-size original range accepted")
	}
	wrong = request
	wrong.Range = nil
	if _, err = v.DecodeContentResponse(encoded, wrong); err == nil {
		t.Fatal("ranged response accepted for whole-byte request")
	}
}
