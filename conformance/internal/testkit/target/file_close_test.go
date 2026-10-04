package target

import (
	"errors"
	"testing"
)

// This is a mechanical FD-close substitution; no os.File/native failure or
// physical scope is created. The actual writer/parent call sites use this gate.
func TestFileReleasePreservesFirstUnknownOutcome(t *testing.T) {
	unknown := errors.New("mechanical first FD close unknown")
	first := true
	release := firstFileClose(func() error {
		if first {
			first = false
			return unknown
		}
		return nil
	})
	if err := release(); !errors.Is(err, unknown) {
		t.Fatal("first physical close cause lost", err)
	}
	if err := release(); !errors.Is(err, unknown) {
		t.Fatal("repeated FD close erased original unknown", err)
	}
}
