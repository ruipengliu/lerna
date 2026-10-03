package component_test

import (
	"encoding/json"
	"github.com/ruipengliu/lerna/contract"
	"os"
	"testing"
)

func TestSharedResponseFixtures(t *testing.T) {
	data, err := os.ReadFile("../fixtures/1.0.0/responses.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Schema, Wire string
		Valid              bool
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			v, err := contract.ParseJSON([]byte(f.Wire))
			if err == nil {
				err = contract.Validate(f.Schema, v)
			}
			if (err == nil) != f.Valid {
				t.Fatalf("valid=%v expected=%v: %v", err == nil, f.Valid, err)
			}
		})
	}
}

func TestClosedReceiptRoundtrip(t *testing.T) {
	value, err := contract.Decode[contract.CommandReceipt]([]byte(`{"state":"accepted","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"c"},"object_ref":{"tenant_id":"t","owner_id":"tasks","kind":"task","id":"x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	accepted, ok := value.AsAccepted()
	if !ok || accepted.ObjectRef.ID != "x" {
		t.Fatal("lost accepted variant")
	}
	if _, ok = value.AsRejected(); ok {
		t.Fatal("accepted also rejected")
	}
	if _, err = contract.Encode(value); err != nil {
		t.Fatal(err)
	}
	if _, err = contract.Encode(contract.CommandReceipt{}); err == nil {
		t.Fatal("zero union encoded")
	}
}

func TestResponseEncodingValidatesOriginalIdentity(t *testing.T) {
	original := contract.CommandRef{Owner: contract.OwnerRef{TenantID: "t", OwnerID: "o"}, CommandID: "c"}
	wrong := original
	wrong.CommandID = "other"
	result := contract.NewCommandGetResponseGone(contract.CommandGetResponseGone{CommandRef: original})
	if _, err := contract.EncodeCommandResponse(result, original); err != nil {
		t.Fatal(err)
	}
	if _, err := contract.EncodeCommandResponse(result, wrong); err == nil {
		t.Fatal("accepted another original identity")
	}
	data, _ := contract.Encode(result)
	if _, err := contract.DecodeCommandResponse(data, wrong); err == nil {
		t.Fatal("decoded another original identity")
	}
}
