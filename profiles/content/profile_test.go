package content

import (
	"context"
	"testing"
)

func TestControlledContentCases(t *testing.T) {
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if err := check(context.Background(), name); err != nil {
				t.Fatal(err)
			}
		})
	}
}
