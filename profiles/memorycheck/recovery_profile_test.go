package memorycheck

import (
	"context"
	"testing"
)

func TestRecoveryIsRequiredByDeletionProfile(t *testing.T) {
	for _, c := range DeletionProfile().Cases {
		if c.ID != "sdk-backup-recovery-governance" {
			continue
		}
		if !c.Required {
			t.Fatal("backup recovery is optional")
		}
		got, err := c.Check(context.Background())
		if err != nil || got != c.Expected {
			t.Fatalf("backup recovery profile: %q %v", got, err)
		}
		return
	}
	t.Fatal("deletion profile omits backup recovery")
}
