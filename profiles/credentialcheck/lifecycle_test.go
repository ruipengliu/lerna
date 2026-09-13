package credentialcheck

import (
	"context"
	"testing"
)

func TestCompletedRotationRejectsLateOldKeyWrite(t *testing.T) {
	if e := lateRotationWrite(context.Background()); e != nil {
		t.Fatal(e)
	}
}

func TestLifecycleStorageAndRecovery(t *testing.T) {
	for _, mode := range []string{"repeat-operation", "bad-material", "activation-failure", "lost-commit", "missing-old-key", "damaged-ciphertext", "backup-retirement", "archive-clone", "damaged-backup", "rollback-quarantine"} {
		t.Run(mode, func(t *testing.T) {
			if e := LifecycleCheck(context.Background(), mode); e != nil {
				t.Fatal(e)
			}
		})
	}
}
