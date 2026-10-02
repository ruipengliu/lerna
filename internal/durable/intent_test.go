package durable_test

import (
	"encoding/json"
	"github.com/ruipengliu/lerna/internal/durable"
	"testing"
	"time"
)

func TestCanonicalIntentBeforeLossyDecode(t *testing.T) {
	for _, raw := range []string{`{"x":1,"x":2}`, `{"x":"\ud800"}`, `{"x":9007199254740993}`, `{"x":9007199254740993.0}`, `{"x":9.007199254740993e15}`, `{"x":1e999999999}`, "{\"x\":\"\xff\"}", `{} {}`} {
		if _, err := durable.CanonicalJSON([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid raw JSON %q", raw)
		}
	}
	a, err := durable.CanonicalJSON([]byte(`{"z":1.0,"a":[2,1],"s":" x "}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := durable.CanonicalJSON([]byte(`{"s":" x ","a":[2,1],"z":1}`))
	if err != nil || string(a) != string(b) {
		t.Fatalf("structure canonicalization: %s / %s / %v", a, b, err)
	}
	// JCS uses UTF-16 ordering, which differs from UTF-8 for this pair.
	c, err := durable.CanonicalJSON([]byte(`{"דּ":1,"😀":2}`))
	if err != nil || string(c) != `{"😀":2,"דּ":1}` {
		t.Fatalf("UTF-16 ordering: %s %v", c, err)
	}
}

func TestFixedIntentKeepsRawMethodInput(t *testing.T) {
	raw := json.RawMessage(`{"integer":9007199254740990.1,"tiny":1e-99999999}`)
	i, err := durable.FixIntent(durable.Intent{CommandID: durable.NewID("cmd"), Method: "task.submit", TargetID: durable.NewID("orc"), ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	if string(i.Value().Payload) != string(raw) {
		t.Fatal("method validator lost original numeric representation")
	}
	raw[2] = 'x'
	copy := i.Value()
	copy.Payload[2] = 'y'
	if i.Value().Payload[2] != 'i' {
		t.Fatal("original intent mutated")
	}
}
