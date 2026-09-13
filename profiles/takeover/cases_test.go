package takeover

import (
	"context"
	"testing"
)

func TestResourceContracts(t *testing.T) {
	for _, name := range caseNames {
		t.Run(name, func(t *testing.T) {
			if e := check(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestResourceBoundaries(t *testing.T) {
	for _, name := range boundaryNames {
		t.Run(name, func(t *testing.T) {
			if e := boundary(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestResourceConcurrency(t *testing.T) {
	for _, name := range concurrencyNames {
		t.Run(name, func(t *testing.T) {
			if e := concurrency(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}
