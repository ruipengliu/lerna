//go:build fault

package sqlite

import (
	"context"
	"testing"
)

// 规则：G3、V4
func TestFaultOccurrenceCountsOnlySelectedPointAndPhase(t *testing.T) {
	ctx, err := WithFaultOnOccurrence(context.Background(), "tasks.planning", LoseReceipt, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []struct {
		point     string
		committed bool
	}{
		{"tasks.planning", false}, {"tasks.planning", true},
		{"tasks.planning", false}, {"tasks.admit", true},
	} {
		if err := persistenceBoundary(ctx, boundary.point, boundary.committed); err != nil || FaultTriggered(ctx) {
			t.Fatalf("fault fired before selected occurrence: %v", err)
		}
	}
	if err := persistenceBoundary(ctx, "tasks.planning", true); err == nil || !FaultTriggered(ctx) {
		t.Fatalf("selected occurrence missed: %v", err)
	}
	if err := persistenceBoundary(ctx, "tasks.planning", true); err != nil {
		t.Fatalf("one-shot fault repeated: %v", err)
	}
}

// 规则：G3、V4
func TestFaultOccurrencePreservesDefaultAndRejectsZero(t *testing.T) {
	if _, err := WithFaultOnOccurrence(context.Background(), "tasks.planning", LoseReceipt, 0); err == nil {
		t.Fatal("zero occurrence accepted")
	}
	ctx, err := WithFault(context.Background(), "tasks.planning", LoseReceipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := persistenceBoundary(ctx, "tasks.planning", false); err != nil || FaultTriggered(ctx) {
		t.Fatalf("wrong phase: %v", err)
	}
	if err := persistenceBoundary(ctx, "tasks.planning", true); err == nil || !FaultTriggered(ctx) {
		t.Fatalf("default first occurrence missed: %v", err)
	}
}
