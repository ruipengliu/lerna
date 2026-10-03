package component_test

import (
	"context"
	"fmt"
	"github.com/ruipengliu/lerna/contract"
	"reflect"
	"testing"
	"time"
)

const readWire = `{"contract_version":"1.0.0","profile":"command","method":"command.get","command_id":"read-new","target":{"tenant_id":"t","owner_id":"o","kind":"command","id":"c"},"payload":{"command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"c"}},"accept_before":"2026-10-03T01:00:00.000000Z"}`

type factSource struct {
	wire  string
	fault bool
	state []string
}

func (f *factSource) ReadCommand(ctx context.Context, ref contract.CommandRef) (contract.CommandGetResponse, error) {
	if err := ctx.Err(); err != nil {
		return contract.CommandGetResponse{}, err
	}
	if f.fault {
		return contract.CommandGetResponse{}, fmt.Errorf("SECRET backend details")
	}
	return contract.Decode[contract.CommandGetResponse]([]byte(f.wire))
}
func TestReadOnlyFactsDoNotReinterpretFixedReceipt(t *testing.T) {
	fixed := `{"state":"accepted","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"c"},"object_ref":{"tenant_id":"t","owner_id":"tasks","kind":"task","id":"x","revision":"5"}}`
	now := func() time.Time { return time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC) }
	for _, status := range []string{"active", "succeeded", "failed"} {
		source := &factSource{wire: `{"status":"found","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"c"},"receipt":` + fixed + `,"progress":{"kind":"task","object_ref":{"tenant_id":"t","owner_id":"tasks","kind":"task","id":"x","revision":"6"},"revision":"6","status":"` + status + `"}}`, state: []string{"original write expired yesterday", "jobs unchanged", "no new effects"}}
		before := append([]string(nil), source.state...)
		result, err := contract.ReadCommandFacts(context.Background(), []byte(readWire), source, now)
		if err != nil {
			t.Fatal(err)
		}
		found, ok := result.AsFound()
		if !ok {
			t.Fatal("not found")
		}
		receipt, err := contract.Encode(found.Receipt)
		if err != nil {
			t.Fatal(err)
		}
		expected, _ := contract.ParseJSON([]byte(fixed))
		actual, _ := contract.ParseJSON(receipt)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatal("changed fixed receipt")
		}
		if !reflect.DeepEqual(before, source.state) {
			t.Fatal("read changed facts")
		}
	}
}
func TestReadOnlyFactsSeparateAbsenceAndUnavailability(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC) }
	for _, status := range []string{"not_found", "gone", "unavailable"} {
		source := &factSource{wire: `{"status":"` + status + `","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"c"}`}
		if status == "unavailable" {
			source.wire += `,"reason":"dependency_unavailable"`
		}
		source.wire += `}`
		result, err := contract.ReadCommandFacts(context.Background(), []byte(readWire), source, now)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := contract.Encode(result)
		raw, _ := contract.ParseJSON(data)
		if raw.(map[string]any)["status"] != status {
			t.Fatal(string(data))
		}
	}
	for _, source := range []*factSource{{fault: true}, {wire: `{"status":"not_found","command_ref":{"owner":{"tenant_id":"t","owner_id":"other"},"command_id":"c"}}`}, {wire: `{"status":"found","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"c"},"receipt":{"state":"rejected","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"c"},"reason":"revision_changed"},"progress":{"kind":"task","object_ref":{"tenant_id":"t","owner_id":"o","kind":"task","id":"x"},"revision":"1","status":"active"}}`}} {
		result, err := contract.ReadCommandFacts(context.Background(), []byte(readWire), source, now)
		if err != nil {
			t.Fatal(err)
		}
		v, ok := result.AsUnavailable()
		if !ok || v.Reason != "dependency_unavailable" || v.CommandRef.Owner.OwnerID != "o" {
			t.Fatal("leaked untrusted backend result")
		}
	}
	expired := func() time.Time { return time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC) }
	result, err := contract.ReadCommandFacts(context.Background(), []byte(readWire), &factSource{fault: true}, expired)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := result.AsRejected()
	if !ok || v.Reason != "expired" {
		t.Fatal("deadline not enforced")
	}
}
