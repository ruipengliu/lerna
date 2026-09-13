package catalogcheck

import (
	"context"
	"os"
	"testing"
)

func TestFullDirectoryExecution(t *testing.T) {
	f, e := newFixture(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer f.close()
	for _, index := range []int{0, 1, 2, 3, 4, 5, 6, 998, 1007} {
		if e = f.call(context.Background(), index); e != nil {
			t.Fatalf("API %d: %v", index, e)
		}
	}
}
func TestCatalogBoundaries(t *testing.T) {
	for _, name := range boundaryNames {
		t.Run(name, func(t *testing.T) {
			if e := boundary(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestCatalogRecovery(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for _, point := range crashNames {
		t.Run(point, func(t *testing.T) {
			if e := recovery(context.Background(), exe, point); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestCatalogChild(t *testing.T) {
	root := os.Getenv("CATALOG_PROBE_ROOT")
	if root == "" {
		return
	}
	if e := RunProbe(context.Background(), root, os.Getenv("CATALOG_PROBE_POINT")); e != nil {
		t.Fatal(e)
	}
}
func TestCatalogDataPolicy(t *testing.T) {
	for _, name := range dataPolicyNames {
		t.Run(name, func(t *testing.T) {
			if e := dataPolicyCheck(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}
