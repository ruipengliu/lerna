package content_test

import (
	"encoding/json"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"testing"
)

func TestIndependentVersionIdentityGolden(t *testing.T) {
	data, err := os.ReadFile("../../conformance/fixtures/1.2.0/digests.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		VersionIdentity struct {
			Ref v.ContentRef
			Key string
		} `json:"version_identity"`
	}
	if err = json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	id, key, err := content.VersionIdentity(golden.VersionIdentity.Ref)
	if err != nil || key != golden.VersionIdentity.Key || id != "cv-"+key {
		t.Fatal("independent original identity tuple differs", err)
	}
	other := golden.VersionIdentity.Ref
	other.Version = "2"
	_, changed, err := content.VersionIdentity(other)
	if err != nil || changed == key {
		t.Fatal("exact version identities collapsed", err)
	}
}
