package trace

import (
	"context"
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 规则：G7、R7
func TestTraceCannotAcceptUnversionedExtensionPayload(t *testing.T) {
	s := New(nil, nil, nil, "u", "d/trace")
	if _, e := s.Accept(context.Background(), &v1.Caller{UserId: "u", IssuerId: "extension"}, &v1.AcceptTraceCommand{}); e == nil {
		t.Fatal("unsupported trace accepted")
	}
}
