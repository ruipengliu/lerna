package brain_test

import (
	"lerna/brain"
	"strings"
	"testing"
)

func TestActionSchemaRejectsCaseAliasesAndNullDependencyArray(t *testing.T) {
	for _, field := range []string{`"Capability"`, `"capability"`} {
		deps := `[]`
		if field == `"capability"` {
			deps = `null`
		}
		raw := `{"kind":"actions","reason":"","actions":[{"key":"a",` + field + `:"` + strings.Repeat("a", 64) + `","depends_on":` + deps + `,"arguments":"{}","resource_version":1}]}`
		if _, e := brain.ParseActions([]byte(raw)); e == nil {
			t.Fatalf("accepted noncanonical contract: %s", field)
		}
	}
}
