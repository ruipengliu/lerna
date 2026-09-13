package extraction_test

import (
	"lerna/extraction"
	"lerna/memory"
	"reflect"
	"testing"
)

func TestDerivedRestrictionsIntersectEverySource(t *testing.T) {
	a := extraction.Restrictions{Storage: []string{"device-a", "cloud"}, Processing: []string{"device-a", "cloud"}, Purposes: []string{"assist", "train"}, Recipients: []string{"device-a", "cloud"}, RetainUntil: 200}
	b := extraction.Restrictions{Storage: []string{"device-a"}, Processing: []string{"device-a"}, Purposes: []string{"assist"}, Recipients: []string{"device-a"}, RetainUntil: 150}
	got, err := extraction.IntersectRestrictions(100, []extraction.Restrictions{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, b) {
		t.Fatalf("intersection: %+v", got)
	}
	got.Storage[0] = "changed"
	if a.Storage[0] != "device-a" || b.Storage[0] != "device-a" {
		t.Fatal("intersection aliases trusted source config")
	}
	b.Processing = []string{"other-device"}
	if _, err = extraction.IntersectRestrictions(100, []extraction.Restrictions{a, b}); err != memory.Denied {
		t.Fatalf("empty processing intersection: %v", err)
	}
}

func TestDerivedRestrictionsCannotIgnoreAnUnavailableDimension(t *testing.T) {
	for _, dimension := range []string{"storage", "processing", "purpose", "recipient", "retention"} {
		t.Run(dimension, func(t *testing.T) {
			base := extraction.Restrictions{Storage: []string{"device-a"}, Processing: []string{"device-a"}, Purposes: []string{"assist"}, Recipients: []string{"device-a"}, RetainUntil: 200}
			restricted := base
			switch dimension {
			case "storage":
				restricted.Storage = []string{"cloud"}
			case "processing":
				restricted.Processing = []string{"cloud"}
			case "purpose":
				restricted.Purposes = []string{"train"}
			case "recipient":
				restricted.Recipients = []string{"cloud"}
			case "retention":
				restricted.RetainUntil = 100
			}
			if _, err := extraction.IntersectRestrictions(100, []extraction.Restrictions{base, restricted}); err != memory.Denied {
				t.Fatalf("incompatible %s: %v", dimension, err)
			}
		})
	}
}
