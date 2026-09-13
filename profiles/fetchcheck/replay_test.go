package fetchcheck

import (
	"context"
	"testing"
)

func TestFixedReplayUsesSameTaskPathWithoutClaimingHTTPRequests(t *testing.T) {
	if err := CheckFixedReplay(context.Background()); err != nil {
		t.Fatal(err)
	}
}
