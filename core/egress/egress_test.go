package egress

import (
	"context"
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 规则：G4、G5、开始-1
func TestInvalidInvocationCannotReachDependencies(t *testing.T) {
	ports := &inertDependencies{}
	s, err := New(ports, ports, ports, ports, ports)
	if err != nil {
		t.Fatal(err)
	}
	if _, e := s.Invoke(context.Background(), &v1.Caller{UserId: "u", IssuerId: "egress"}, &v1.StartExecutionCommand{}); e == nil {
		t.Fatal("unsupported command accepted")
	}
}
