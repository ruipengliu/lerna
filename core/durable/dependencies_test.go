package durable

import (
	"strings"
	"testing"
)

// 规则：R6、G1
func TestRequireDependenciesRejectsFirstMissingIncludingTypedNil(t *testing.T) {
	var pointer *struct{}
	var callback func()
	var mapping map[string]string
	var slice []string
	var channel chan string
	for _, value := range []any{nil, pointer, callback, mapping, slice, channel} {
		err := RequireDependencies("ledger", Dependency{Name: "store", Value: struct{}{}}, Dependency{Name: "work", Value: value}, Dependency{Name: "starts", Value: nil})
		if err == nil || !strings.Contains(err.Error(), "ledger.work") || strings.Contains(err.Error(), "starts") {
			t.Fatalf("missing dependency error = %v", err)
		}
	}
	if err := RequireDependencies("egress", Dependency{Name: "io", Value: struct{}{}}); err != nil {
		t.Fatalf("complete dependency rejected: %v", err)
	}
}
