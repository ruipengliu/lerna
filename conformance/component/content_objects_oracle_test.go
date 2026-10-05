//go:build integration

package component_test

import (
	"os"
	"path/filepath"
	"testing"

	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
)

// Observe the original exact body keys independently. These existing scenarios
// create no seals; only their known keys' empty permanent flock files are valid
// additional entries. Arbitrary metadata names and every temp/body are checked.
func assertExactContentObjects(t *testing.T, directory string, present map[v.ContentRef]string, absent ...v.ContentRef) {
	t.Helper()
	keys := make(map[string]bool, len(present)+len(absent))
	bodies := make(map[string]string, len(present))
	for ref, expected := range present {
		_, key, err := content.VersionIdentity(ref)
		if err != nil {
			t.Fatal(err)
		}
		keys[key], bodies[key] = true, expected
		actual, err := os.ReadFile(filepath.Join(directory, key))
		if err != nil || string(actual) != expected {
			t.Fatal("exact original independent body differs", ref, err)
		}
	}
	for _, ref := range absent {
		_, key, err := content.VersionIdentity(ref)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := bodies[key]; exists {
			t.Fatal("same exact object required both present and absent", ref)
		}
		keys[key] = true
		if _, err = os.Lstat(filepath.Join(directory, key)); !os.IsNotExist(err) {
			t.Fatal("refused original installed a body", ref, err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal("unexpected object entry type", entry.Name(), err)
		}
		if _, ok := bodies[entry.Name()]; ok {
			continue
		}
		name := entry.Name()
		if len(name) == 69 && name[64:] == ".lock" && keys[name[:64]] && info.Size() == 0 {
			continue
		}
		t.Fatal("unexpected body, attempt or metadata in original scope", name)
	}
}
