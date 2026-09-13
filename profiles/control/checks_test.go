package control

import (
	"context"
	"testing"
)

func TestControlContractChecks(t *testing.T) {
	for _, name := range checks {
		t.Run(name, func(t *testing.T) {
			if err := check(context.Background(), name); err != nil {
				t.Fatal(err)
			}
		})
	}
}
