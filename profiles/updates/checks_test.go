package updates

import (
	"context"
	"testing"
)

func TestUpdateContracts(t *testing.T) {
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if e := check(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestAdvancedContracts(t *testing.T) {
	for _, name := range advancedNames {
		t.Run(name, func(t *testing.T) {
			if e := advanced(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestV08Upgrade(t *testing.T) {
	if e := upgrade(context.Background()); e != nil {
		t.Fatal(e)
	}
}
