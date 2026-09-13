package credentialcheck

import (
	"context"
	"testing"
)

func TestLifecycleRenewalUsesIndependentHTTPProvider(t *testing.T) {
	for _, mode := range []string{"lost-response", "unknown", "not-occurred", "unsafe", "target-mismatch", "policy-revoked", "during-rotation", "provider-change"} {
		t.Run(mode, func(t *testing.T) {
			if e := RenewalCheck(context.Background(), mode); e != nil {
				t.Fatal(e)
			}
		})
	}
}
