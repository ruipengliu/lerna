package component_test

import (
	"encoding/json"
	"github.com/ruipengliu/lerna/contract"
	"os"
	"strings"
	"testing"
)

func TestRevisionRetainsExactLargeInteger(t *testing.T) {
	value, err := contract.Decode[contract.Revision]([]byte(`"9223372036854775807"`))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := contract.Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != `"9223372036854775807"` {
		t.Fatalf("lost exact integer: %s", wire)
	}
}

func TestContentReferenceRetainsExactVersionAndLength(t *testing.T) {
	wire := []byte(`{"owner":{"tenant_id":"tenant-1","owner_id":"content-1"},"content_id":"c:1","version":"9007199254740993","hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","media_type":"text/plain","byte_length":"9223372036854775807"}`)
	value, err := contract.Decode[contract.ContentRef](wire)
	if err != nil {
		t.Fatal(err)
	}
	if string(value.Version) != "9007199254740993" || string(value.ByteLength) != "9223372036854775807" {
		t.Fatalf("lost exact content identity: %+v", value)
	}
	encoded, err := contract.Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := contract.Decode[contract.ContentRef](encoded)
	if err != nil || decoded != value {
		t.Fatalf("reference did not roundtrip: %+v %v", decoded, err)
	}
}

func TestValueBoundaryRejectsTruncationAndMalformedJSON(t *testing.T) {
	for _, wire := range []string{`"9223372036854775808"`, `"01"`, `"-1"`, `1`, `"0" "1"`, `"\ud800"`, string([]byte{'"', 0xff, '"'})} {
		t.Run(wire, func(t *testing.T) {
			if _, err := contract.Decode[contract.Revision]([]byte(wire)); err == nil {
				t.Fatalf("accepted illegal wire %q", wire)
			}
		})
	}
}

func TestPublicJSONBoundaryPreservesUnicodeAndRejectsAmbiguity(t *testing.T) {
	value, err := contract.ParseJSON([]byte(`{"__proto__":"safe","text":"👩‍💻 汉字"}`))
	if err != nil {
		t.Fatal(err)
	}
	if value.(map[string]any)["text"] != "👩‍💻 汉字" {
		t.Fatalf("lost Unicode: %+v", value)
	}
	for _, wire := range []string{`{"a":true,"\u0061":false}`, `{"text":"\ud800"}`, `{"text":"\udfff"}`, `{"text":"\ud800\u0041"}`, `{"text":"` + string([]byte{0xff}) + `"}`, `{"n":9007199254740993}`, `{} {}`, "\ufeff{}"} {
		if _, err := contract.ParseJSON([]byte(wire)); err == nil {
			t.Fatalf("accepted ambiguous wire %q", wire)
		}
	}
}

func TestSharedValueFixtures(t *testing.T) {
	data, err := os.ReadFile("../fixtures/1.0.0/values.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name   string
		Schema string
		Wire   string
		Valid  bool
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			value, err := contract.ParseJSON([]byte(fixture.Wire))
			if err == nil {
				err = contract.Validate(fixture.Schema, value)
			}
			if (err == nil) != fixture.Valid {
				t.Fatalf("valid=%v expected %v: %v", err == nil, fixture.Valid, err)
			}
		})
	}
}

func TestEncodingCannotReplaceInvalidUnicode(t *testing.T) {
	view, err := contract.Decode[contract.CollectionView]([]byte(`{"items":[],"cursor":"next","exhausted":false,"partial":false,"gaps":[],"read_scope":{"owner":{"tenant_id":"t1","owner_id":"o1"},"object_type":"task"},"watermark":"1"}`))
	if err != nil {
		t.Fatal(err)
	}
	cursor := string([]byte{0xff})
	view.Cursor = &cursor
	if _, err := contract.Encode(view); err == nil {
		t.Fatal("encoding silently replaced invalid UTF-8")
	}
}

func TestRawJSONBodyAndDepthLimits(t *testing.T) {
	if _, err := contract.ParseJSON([]byte(strings.Repeat(" ", 1048572) + "null")); err != nil {
		t.Fatal(err)
	}
	if _, err := contract.ParseJSON([]byte(strings.Repeat(" ", 1048573) + "null")); err == nil {
		t.Fatal("accepted body above 1MiB")
	}
	if _, err := contract.ParseJSON([]byte(strings.Repeat("[", 64) + "null" + strings.Repeat("]", 64))); err != nil {
		t.Fatal(err)
	}
	if _, err := contract.ParseJSON([]byte(strings.Repeat("[", 65) + "null" + strings.Repeat("]", 65))); err == nil {
		t.Fatal("accepted container depth 65")
	}
}
