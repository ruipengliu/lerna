package extractioncheck

import (
	"context"
	"lerna/contextassembly"
	"testing"
)

// Cancel precisely where the observed overloaded run exhausted its context,
// then use the actual Memory/grant implementation to perform the check.
type cancelRetentionCheck struct {
	contextassembly.Memories
	cancel context.CancelFunc
	fired  bool
}

func (m *cancelRetentionCheck) Validate(ctx context.Context, r contextassembly.Request, ref contextassembly.Reference, retained bool) error {
	if retained {
		m.fired = true
		m.cancel()
	}
	return m.Memories.Validate(ctx, r, ref, retained)
}

func checkCancelledMemoryValidation(t *testing.T, store contextassembly.Store, facts contextassembly.Facts, memories contextassembly.Memories, policy contextassembly.SnapshotPolicy, request contextassembly.Request) {
	t.Helper()
	for _, method := range []string{"assemble", "validate"} {
		ctx, cancel := context.WithCancel(context.Background())
		wrapped := &cancelRetentionCheck{Memories: memories, cancel: cancel}
		assembler, err := contextassembly.NewWithPolicy(store, facts, wrapped, policy)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if method == "assemble" {
			out, e := assembler.Assemble(ctx, request)
			err = e
			if len(out.Input.Blocks) != 0 {
				cancel()
				t.Fatal("cancelled assembly released context")
			}
		} else {
			err = assembler.Validate(ctx, request)
		}
		cancel()
		if !wrapped.fired || err != contextassembly.Unavailable {
			t.Fatalf("%s cancellation classified as permission/state: fired=%v err=%v", method, wrapped.fired, err)
		}
	}
}
