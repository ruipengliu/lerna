package memorycheck

import (
	"context"
	"os"
	"testing"
)

func TestProcessProbe(t *testing.T) {
	if path := os.Getenv("HARNESS_MEMORY_PROBE"); path != "" {
		if e := RunProbe(context.Background(), path, os.Getenv("HARNESS_MEMORY_MODE")); e != nil {
			t.Fatal(e)
		}
	}
}
func TestMemoryProcessRecovery(t *testing.T) {
	for _, mode := range []string{"before-commit", "after-commit", "after-reserve", "before-binding", "after-binding"} {
		t.Run(mode, func(t *testing.T) {
			if e := ProcessCheck(context.Background(), mode); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestMemoryProfileCases(t *testing.T) {
	for _, name := range []string{"fact", "preference", "inference", "experience", "schema-rejection", "residency", "revocation", "empty-binding", "budget", "exact-history", "concurrent-read", "invalid-operation"} {
		t.Run(name, func(t *testing.T) {
			if e := Check(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}
