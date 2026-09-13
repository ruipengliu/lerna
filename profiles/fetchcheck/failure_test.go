package fetchcheck_test

import (
	"context"
	"lerna/profiles/fetchcheck"
	"strconv"
	"testing"
	"time"
)

func TestCorePreservesFailedAcquisitionFacts(t *testing.T) {
	for _, status := range []int{403, 404, 410} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := fetchcheck.CheckFailure(ctx, status); err != nil {
				t.Fatal(err)
			}
		})
	}
}
