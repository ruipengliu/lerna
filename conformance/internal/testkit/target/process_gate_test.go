package target

import "context"

// ConfigureProcessGate is compiled only into this target's test binary. It
// gives its concrete child a private bounded gate, never a provider input.
func ConfigureProcessGate(t *Target, pause func(context.Context, string, string) error) {
	t.checkpoint = func(ctx context.Context, stage string, event Event) error { return pause(ctx, stage, event.ID) }
}
