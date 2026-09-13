package fetchcheck_test

import (
	"context"
	"lerna/profiles/fetchcheck"
	"testing"
	"time"
)

func TestCoreAndSDKAcquireActualHTTPEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := fetchcheck.CheckTask(ctx); err != nil {
		t.Fatal(err)
	}
}
