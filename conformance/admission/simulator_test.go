package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/adapters/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 规则：G1、G5、R3
func TestSimulatorDeclarationsRemainDistinct(t *testing.T) {
	for _, tc := range []struct {
		mode                  string
		idempotent, queryable bool
	}{{"idempotent", true, false}, {"queryable", false, true}, {"opaque", false, false}} {
		t.Run(tc.mode, func(t *testing.T) {
			adapter := simulator.Adapter{}
			op := &v1.Operation{ParametersRef: &v1.Ref{Revision: 1}, CapabilitySnapshot: &v1.Capability{Action: "CREATE", Resource: "http://127.0.0.1:3210/records", Ref: &v1.Ref{Revision: 1}, AdapterRef: &v1.Ref{Name: &v1.GlobalName{LocalId: "simulator-" + tc.mode}, Revision: 1}}}
			d, cap, e := adapter.Compile(op, &v1.ExecutionAttempt{ExternalKey: "original-key"})
			if e != nil || cap.Idempotent != tc.idempotent || cap.Queryable != tc.queryable || cap.Effect != "ATOMIC_WRITE" || cap.ProtocolVersion != "lerna-simulator-v1" || d.ExternalKey != "original-key" || d.Method != "POST" {
				t.Fatalf("declaration %v descriptor %v error %v", cap, d, e)
			}
		})
	}
}
