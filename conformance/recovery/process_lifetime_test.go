//go:build integration

package recovery_test

import (
	"context"
	"errors"
	"testing"
)

func TestOwnedFixtureRetainsEarlierGenerationCleanupUnknown(t *testing.T) {
	// Mechanical physical-lifetime control: no child, FD, SQL connection or
	// filesystem scope is created. The empty path grants no real deletion power.
	// Each substitute describes cleanup knowledge, not a native FD fault.
	unknown := errors.New("mechanical prior generation FD cleanup unknown")
	f := &ownedFixture{backend: "sqlite", owns: true}
	prior := &hostProcess{waited: true, cancel: func() {}, stopPhysical: func(context.Context) (bool, error) { return false, unknown }}
	successor := &hostProcess{waited: true, cancel: func() {}, stopPhysical: func(context.Context) (bool, error) { return true, nil }}
	f.registerChild(prior)
	f.registerChild(successor)
	for range 2 {
		if err := f.Cleanup(); !errors.Is(err, unknown) {
			t.Fatalf("prior generation cleanup cause lost: %v", err)
		}
		if !f.owns {
			t.Fatal("new generation allowed cleanup to discard prior unknown ownership")
		}
	}
}
