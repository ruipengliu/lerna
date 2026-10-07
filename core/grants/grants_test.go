package grants_test

import (
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/core/grants"
)

// 只用于构造边界的 typed nil，不提供业务实现。
type missingStore struct{ grants.Store }

// 规则：R6、G1
func TestConstructorRejectsMissingStoreBeforeUse(t *testing.T) {
	var typedNil *missingStore
	for _, store := range []grants.Store{nil, typedNil} {
		service, err := grants.New(store, nil, "alice", "local", "host")
		if service != nil || err == nil || !strings.Contains(err.Error(), "grants.store") {
			t.Fatalf("constructor: %v %v", service, err)
		}
	}
}
