package credentialcheck

import (
	"context"
	"os"
	"testing"
)

func TestLifecycleProbeProcess(t *testing.T) {
	path := os.Getenv("HARNESS_LIFECYCLE_PROBE")
	if path == "" {
		return
	}
	if e := RunLifecycleProbe(context.Background(), path, os.Getenv("HARNESS_LIFECYCLE_MODE")); e != nil {
		t.Fatal(e)
	}
}
func TestLifecycleRealProcessInterruption(t *testing.T) {
	for _, mode := range []string{"after-prepare", "before-activate", "after-activate", "before-row", "after-row"} {
		t.Run(mode, func(t *testing.T) {
			if e := LifecycleProcessCheck(context.Background(), mode); e != nil {
				t.Fatal(e)
			}
		})
	}
}
