package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// 规则：G1、G3、G4、G10、G11、R3、开始-2、准入-8
func TestExecutionDeclarationPreservesUnknownQueryBoundsOriginalProofAndBilling(t *testing.T) {
	for _, behavior := range []string{"terminal", "applied-not-terminal"} {
		t.Run(behavior, func(t *testing.T) {
			target := simulator.New("idempotent")
			if behavior != "terminal" {
				target.SetBehavior(behavior)
			}
			f := newFixtureWithTarget(t, 100, 80, false, target)
			a, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			declaration := op.Execution.Attempt.Capabilities
			assertUnknownQueryBounds(t, declaration)
			if declaration.ProtocolVersion != "lerna-simulator-v1" || declaration.VerificationBasis != "reference-target-v1" || declaration.DeclarationVersion != "1" || declaration.Effect != "ATOMIC_WRITE" || !declaration.Idempotent || declaration.Queryable {
				t.Fatalf("original fixed declaration changed: %v", declaration)
			}
			admission, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, op.AdmissionRef)
			if e != nil || op.CapabilitySnapshot.FeeCeiling == nil || *op.CapabilitySnapshot.FeeCeiling != 30 || op.CapabilitySnapshot.Nonbillable || admission.BudgetBasis.Ceiling != 30 || !proto.Equal(op.CapabilitySnapshot.RateBasisRef, admission.BudgetBasis.RateBasisRef) {
				t.Fatalf("billing dimension left original capability/budget basis: %v %v", admission, e)
			}
			interpreted, e := f.h.Ledger.InterpretObservation(f.ctx, f.caller, &v1.InterpretObservationCommand{Header: ledgerHeader("declaration-original-proof"), ObservationRef: op.Execution.Send.ObservationRef})
			accepted(t, interpreted, e)
			finding, e := f.h.Ledger.QueryInterpretation(f.ctx, f.caller, interpreted.ResultRef)
			if e != nil || finding.Rule != "reference-target-v1" || finding.Outcome != "APPLIED" || behavior == "terminal" && finding.LateEffect != "RULED_OUT" || behavior != "terminal" && finding.LateEffect != "MAY_OCCUR" {
				t.Fatalf("declaration rule was confused with an outcome guarantee: %v %v", finding, e)
			}
			if e = f.h.Close(); e != nil {
				t.Fatal(e)
			}
			f.h, e = assembly.Open(f.path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			restored, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			requests, effects := target.Snapshot()
			if e != nil || !proto.Equal(op.Execution, restored.Execution) || !proto.Equal(op.CapabilitySnapshot, restored.CapabilitySnapshot) || len(requests) != 1 || len(effects) != 1 || f.calls.Load() != 1 {
				t.Fatalf("recovery upgraded declaration, billing or original request: %v %v", restored, e)
			}
		})
	}
}

func assertUnknownQueryBounds(t *testing.T, declaration *v1.ExecutionCapabilities) {
	t.Helper()
	if declaration == nil {
		t.Fatal("original execution has no declaration")
	}
	m := declaration.ProtoReflect()
	for _, name := range []protoreflect.Name{"query_visibility_delay_ms", "query_record_retention_ms"} {
		field := m.Descriptor().Fields().ByName(name)
		if field == nil || !field.HasPresence() {
			t.Fatalf("public original declaration lacks optional unknown query bound %s", name)
		}
		if m.Has(field) {
			t.Fatalf("fixed M1 producer invented a verified query bound %s", name)
		}
		// 仅绑定编解码案例明确置零；不把这个合成变体采进实际声明语料或授予保证。
		zero := proto.Clone(declaration)
		zero.ProtoReflect().Set(field, protoreflect.ValueOfInt64(0))
		encoded, e := proto.Marshal(zero)
		if e != nil {
			t.Fatal(e)
		}
		decoded := new(v1.ExecutionCapabilities)
		if e = proto.Unmarshal(encoded, decoded); e != nil || !decoded.ProtoReflect().Has(field) || decoded.ProtoReflect().Get(field).Int() != 0 || proto.Equal(declaration, decoded) {
			t.Fatalf("binding collapsed absent UNKNOWN and an explicit zero bound %s: %v", name, e)
		}
	}
}

func assertFixedExecutionDeclaration(t *testing.T, declaration *v1.ExecutionCapabilities, protocol, basis string) {
	t.Helper()
	assertUnknownQueryBounds(t, declaration)
	if declaration.ProtocolVersion != protocol || declaration.VerificationBasis != basis || declaration.DeclarationVersion != "1" {
		t.Fatalf("original public protocol/proof dictionary binding changed: %v", declaration)
	}
}

func assertOriginalProofRule(t *testing.T, f *fixture, operation *v1.Operation, rule string) {
	t.Helper()
	receipt, err := f.h.Ledger.InterpretObservation(f.ctx, f.caller, &v1.InterpretObservationCommand{Header: ledgerHeader("declaration-rule-" + operation.Ref.Name.LocalId), ObservationRef: operation.Execution.Send.ObservationRef})
	accepted(t, receipt, err)
	finding, err := f.h.Ledger.QueryInterpretation(f.ctx, f.caller, receipt.ResultRef)
	if err != nil || finding.Rule != rule {
		t.Fatalf("original public interpretation used another proof rule: %v %v", finding, err)
	}
}
