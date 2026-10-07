package egress

import (
	"strings"
	"testing"
)

// inertDependencies 只用于构造检查；调用其业务方法会因没有实现而失败。
type inertDependencies struct {
	Starts
	Ledger
	Content
	IO
	CriticalSection
}

// 规则：R6、G4、G5
func TestEgressConstructionRejectsEveryMissingDependencyIncludingTypedNil(t *testing.T) {
	present := &inertDependencies{}
	var absent *inertDependencies
	for _, typedNil := range []bool{false, true} {
		for _, name := range []string{"starts", "ledger", "content", "io", "critical"} {
			var starts Starts = present
			var ledger Ledger = present
			var content Content = present
			var io IO = present
			var critical CriticalSection = present
			switch name {
			case "starts":
				starts = nil
				if typedNil {
					starts = absent
				}
			case "ledger":
				ledger = nil
				if typedNil {
					ledger = absent
				}
			case "content":
				content = nil
				if typedNil {
					content = absent
				}
			case "io":
				io = nil
				if typedNil {
					io = absent
				}
			case "critical":
				critical = nil
				if typedNil {
					critical = absent
				}
			}
			service, err := New(starts, ledger, content, io, critical)
			if err == nil || service != nil || !strings.Contains(err.Error(), "egress."+name) {
				t.Fatalf("missing %s (typed nil %v): %v %v", name, typedNil, service, err)
			}
		}
	}
	service, err := New(present, present, present, present, present)
	if err != nil || service.ValidateDependencies() != nil {
		t.Fatalf("complete construction rejected: %v", err)
	}
}
