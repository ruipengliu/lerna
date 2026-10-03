package component_test

import (
	"encoding/json"
	"errors"
	"github.com/ruipengliu/lerna/contract"
	"os"
	"strings"
	"testing"
)

func TestCommandDigestMatchesIndependentGolden(t *testing.T) {
	data, err := os.ReadFile("../fixtures/1.0.0/digests.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Wire, Digest string
		SubjectWire        string `json:"subject_wire"`
		Valid              bool
		Code               string
		BodyRepeatCount    int `json:"body_repeat_count"`
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			wire := f.Wire
			if f.BodyRepeatCount > 0 {
				wire = strings.ReplaceAll(wire, "@B@", strings.Repeat("x", f.BodyRepeatCount))
			}
			got, err := contract.CommandDigest([]byte(wire), []byte(f.SubjectWire))
			if !f.Valid {
				var refusal *contract.ContractError
				if !errors.As(err, &refusal) || string(refusal.Code) != f.Code {
					t.Fatalf("got %v want refusal %s", err, f.Code)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != f.Digest {
				t.Fatalf("got %s want %s", got, f.Digest)
			}
		})
	}
}

func TestDigestDoesNotGrantExecutionForFixtureMethods(t *testing.T) {
	data, err := os.ReadFile("../fixtures/1.0.0/digests.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct{ Wire string }
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	_, err = contract.DecodeCommand([]byte(fixtures[0].Wire))
	var refusal *contract.ContractError
	if !errors.As(err, &refusal) || refusal.Code != "unsupported" {
		t.Fatalf("digest fixture offered execution: %v", err)
	}
}
