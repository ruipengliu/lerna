package v1_2_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"os"
	"strings"
	"testing"
)

func TestContentCanonicalBodyBound(t *testing.T) {
	normal, err := os.ReadFile("../../conformance/fixtures/1.2.0/put-alpha.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.DecodePut(normal); err != nil {
		t.Fatalf("normal finite baseline: %v", err)
	}
	var request map[string]any
	if err = json.Unmarshal(normal, &request); err != nil {
		t.Fatal(err)
	}
	payload := request["payload"].(map[string]any)
	payload["bytes_base64"] = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 262145)))
	payload["content_ref"].(map[string]any)["byte_length"] = "262145"
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.DecodePut(raw)
	var refused *v.ContractError
	if !errors.As(err, &refused) || refused.Code != "input_over_limit" {
		t.Fatalf("canonical oversized body should input_over_limit, got %v", err)
	}
}
