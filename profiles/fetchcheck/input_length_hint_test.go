package fetchcheck

import (
	"context"
	executioncontent "lerna/adapters/execution/content"
	"lerna/answers"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"testing"
)

func TestExecutionInputLengthHintsCannotReturnPartialData(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx, []string{"https://example.test/start", "https://example.test/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	body := []byte(`{"value":"complete input"}`)
	ref, err := h.put(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	id, err := answers.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		size    uint64
		allowed bool
		calls   int32
	}{
		{"correct", uint64(len(body)), true, 1},
		{"too_short", uint64(len(body) - 1), false, 1},
		{"too_long", uint64(len(body) + 1), false, 1},
		{"zero_is_not_cached", 0, true, 2},
		{"oversize_is_not_cached", 32769, true, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			observed := &observedFailureContent{content: h.content}
			reader := executioncontent.New(observed, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, h.clock)
			// Deliberately supply incomplete/untrusted metadata. Only the actual
			// authorized READ record may establish the data's length and validity.
			reader.RememberInput(&wire.ContentRecord{Ref: id, State: "available", Spec: &wire.ContentSpec{Size: test.size, MediaType: "application/json"}})
			got, err := reader.Read(ctx, h.token, ref, h.cap)
			if test.allowed {
				if err != nil || string(got) != string(body) {
					t.Fatalf("complete input: %v", err)
				}
			} else if err == nil || len(got) != 0 {
				t.Fatal("incorrect length returned partial input")
			}
			if observed.calls.Load() != test.calls {
				t.Fatalf("unexpected observations: %d", observed.calls.Load())
			}
		})
	}
}
