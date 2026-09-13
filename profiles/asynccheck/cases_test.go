package asynccheck

import (
	"context"
	"testing"
)

func TestAsyncContracts(t *testing.T) {
	for _, name := range caseNames {
		t.Run(name, func(t *testing.T) {
			if e := check(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}
