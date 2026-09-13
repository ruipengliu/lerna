package worker

import (
	"context"
	"testing"
)

func TestWorkerContractChecks(t *testing.T) {
	for _, name := range checks {
		t.Run(name, func(t *testing.T) {
			if err := check(context.Background(), name); err != nil {
				t.Fatal(err)
			}
		})
	}
}
