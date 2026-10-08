package rules_test

import (
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/rules"
)

const target = "http://127.0.0.1:8080/target"

var (
	simulatorWrite = &v1.ExecutionCapabilities{Effect: "ATOMIC_WRITE", ProtocolVersion: "lerna-simulator-v1", DeclarationVersion: "1", VerificationBasis: "reference-target-v1"}
	simulatorQuery = &v1.ExecutionCapabilities{Effect: "READ", ProtocolVersion: "lerna-simulator-query-v1", DeclarationVersion: "1", VerificationBasis: "reference-query-v1"}
	modelInference = &v1.ExecutionCapabilities{Effect: "MODEL_INFERENCE", ProtocolVersion: "lerna-model-v1", DeclarationVersion: "1", VerificationBasis: "reference-model-v1"}
)

func fileDeclaration(effect string, queryable bool) *v1.ExecutionCapabilities {
	return &v1.ExecutionCapabilities{Effect: effect, Queryable: queryable, ProtocolVersion: "lerna-managed-file-v1", DeclarationVersion: "1", VerificationBasis: "managed-file-v1"}
}

// operation 只构造规则读取的原能力、尝试与发送绑定。
func operation(adapter string, revision uint64, action string, declaration *v1.ExecutionCapabilities) *v1.Operation {
	op := &v1.Operation{
		Ref:                &v1.Ref{Name: &v1.GlobalName{UserId: "u", LocalId: "op-1"}},
		CapabilitySnapshot: &v1.Capability{AdapterRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", LocalId: adapter}, Revision: revision}, Action: action, Resource: target},
	}
	if declaration != nil {
		op.Execution = &v1.Execution{
			Attempt: &v1.ExecutionAttempt{Ref: &v1.Ref{Name: &v1.GlobalName{UserId: "u", LocalId: "attempt-1"}}, ExternalKey: "key-1", Capabilities: declaration},
			Send:    &v1.PhysicalSend{Ref: &v1.Ref{Name: &v1.GlobalName{UserId: "u", LocalId: "send-1"}}},
		}
	}
	return op
}

func httpObservation() *v1.RawObservation {
	return &v1.RawObservation{Ref: &v1.Ref{Name: &v1.GlobalName{UserId: "u", LocalId: "raw-1"}}, Source: "TRUSTED_IO", Protocol: "HTTP", StatusCode: 200, Target: target, ActualAddress: "127.0.0.1:8080", ExternalKey: "key-1", SendRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", LocalId: "send-1"}}}
}

// 规则：G1
func TestFixedAcceptsOnlyOriginalDeclarations(t *testing.T) {
	supported := []struct {
		name string
		op   *v1.Operation
	}{
		{"simulator-idempotent", operation("simulator-idempotent", 1, "CREATE", simulatorWrite)},
		{"simulator-idempotent-expiring", operation("simulator-idempotent-expiring", 1, "CREATE", simulatorWrite)},
		{"simulator-idempotent-evicting", operation("simulator-idempotent-evicting", 1, "CREATE", simulatorWrite)},
		{"simulator-idempotent-queryable", operation("simulator-idempotent-queryable", 1, "CREATE", simulatorWrite)},
		{"simulator-queryable", operation("simulator-queryable", 1, "CREATE", simulatorWrite)},
		{"simulator-opaque", operation("simulator-opaque", 1, "CREATE", simulatorWrite)},
		{"simulator-queryable query", operation("simulator-queryable", 1, "QUERY", simulatorQuery)},
		{"simulator before execution", operation("simulator-opaque", 1, "CREATE", nil)},
		{"model", operation("model-reference-v1", 1, "MODEL_INFER", modelInference)},
		{"file create", operation("managed-file", 1, "CREATE", fileDeclaration("ATOMIC_WRITE", true))},
		{"file replace", operation("managed-file", 1, "REPLACE", fileDeclaration("ATOMIC_WRITE", true))},
		{"file read", operation("managed-file", 1, "READ", fileDeclaration("READ", false))},
		{"file query", operation("managed-file", 1, "QUERY", fileDeclaration("READ", false))},
		{"file cleanup", operation("managed-file", 1, "CLEANUP", fileDeclaration("PARTIAL_WRITE", false))},
	}
	for _, c := range supported {
		if err := (rules.Fixed{}).CheckSupported(c.op); err != nil {
			t.Errorf("%s: original declaration rejected: %v", c.name, err)
		}
	}
	changedProtocol := clone(simulatorWrite)
	changedProtocol.ProtocolVersion = "lerna-simulator-v2"
	changedBasis := clone(modelInference)
	changedBasis.VerificationBasis = "reference-model-v2"
	unsupported := []struct {
		name string
		op   *v1.Operation
	}{
		{"unknown adapter", operation("simulator-unknown", 1, "CREATE", simulatorWrite)},
		{"missing adapter", &v1.Operation{CapabilitySnapshot: &v1.Capability{Action: "CREATE"}}},
		{"simulator revision", operation("simulator-opaque", 2, "CREATE", simulatorWrite)},
		{"model revision", operation("model-reference-v1", 2, "MODEL_INFER", modelInference)},
		{"file revision", operation("managed-file", 2, "CREATE", fileDeclaration("ATOMIC_WRITE", true))},
		{"simulator query on non-queryable", operation("simulator-opaque", 1, "QUERY", simulatorQuery)},
		{"simulator unknown action", operation("simulator-opaque", 1, "DELETE", simulatorWrite)},
		{"model wrong action", operation("model-reference-v1", 1, "CREATE", modelInference)},
		{"file unknown action", operation("managed-file", 1, "DELETE", fileDeclaration("ATOMIC_WRITE", true))},
		{"simulator changed protocol", operation("simulator-opaque", 1, "CREATE", changedProtocol)},
		{"model changed basis", operation("model-reference-v1", 1, "MODEL_INFER", changedBasis)},
		{"file read declared as write", operation("managed-file", 1, "READ", fileDeclaration("ATOMIC_WRITE", true))},
		{"api without descriptor", operation("api-reference-v1", 1, "CREATE", nil)},
	}
	for _, c := range unsupported {
		if err := (rules.Fixed{}).CheckSupported(c.op); err == nil || err.Error() != "PREPARATION_UNRECOVERABLE" {
			t.Errorf("%s: CheckSupported = %v", c.name, err)
		}
		facts, err := (rules.Fixed{}).Interpret(c.op, httpObservation(), nil)
		if facts != nil || err == nil || err.Error() != "PREPARATION_UNRECOVERABLE" {
			t.Errorf("%s: Interpret = %v %v", c.name, facts, err)
		}
	}
}

func clone(c *v1.ExecutionCapabilities) *v1.ExecutionCapabilities {
	return &v1.ExecutionCapabilities{Effect: c.Effect, Queryable: c.Queryable, ProtocolVersion: c.ProtocolVersion, DeclarationVersion: c.DeclarationVersion, VerificationBasis: c.VerificationBasis}
}

// 规则：G1
func TestFixedSimulatorWriteRulesOutLateEffectOnlyOnTerminalProof(t *testing.T) {
	op := operation("simulator-opaque", 1, "CREATE", simulatorWrite)
	body := func(applied, terminal string) []byte {
		return []byte(`{"protocol":"lerna-simulator-v1","external_key":"key-1","attempt_id":"attempt-1","applied":` + applied + `,"terminal":` + terminal + `}`)
	}
	untrusted := httpObservation()
	untrusted.Source = "ADAPTER"
	wrongAddress := httpObservation()
	wrongAddress.ActualAddress = "127.0.0.2:8080"
	for _, c := range []struct {
		name                  string
		raw                   *v1.RawObservation
		body                  []byte
		outcome, late, reason string
	}{
		{"terminal applied", httpObservation(), body("true", "true"), "APPLIED", "RULED_OUT", "TERMINAL_PROTOCOL_EVIDENCE"},
		{"terminal not applied", httpObservation(), body("false", "true"), "NOT_APPLIED", "RULED_OUT", "TERMINAL_PROTOCOL_EVIDENCE"},
		{"applied but not terminal", httpObservation(), body("true", "false"), "APPLIED", "MAY_OCCUR", "LATE_EFFECT_POSSIBLE"},
		{"not applied and not terminal", httpObservation(), body("false", "false"), "UNKNOWN", "MAY_OCCUR", "LATE_EFFECT_POSSIBLE"},
		{"untrusted source", untrusted, body("false", "true"), "UNKNOWN", "MAY_OCCUR", "INSUFFICIENT_EVIDENCE"},
		{"other address", wrongAddress, body("false", "true"), "UNKNOWN", "MAY_OCCUR", "INSUFFICIENT_EVIDENCE"},
		{"other attempt", httpObservation(), []byte(`{"protocol":"lerna-simulator-v1","external_key":"key-1","attempt_id":"attempt-2","applied":false,"terminal":true}`), "UNKNOWN", "MAY_OCCUR", "INSUFFICIENT_EVIDENCE"},
		{"duplicate field", httpObservation(), []byte(`{"protocol":"lerna-simulator-v1","external_key":"key-1","attempt_id":"attempt-1","applied":false,"applied":true,"terminal":true}`), "UNKNOWN", "MAY_OCCUR", "EVIDENCE_CONFLICT"},
	} {
		facts, err := (rules.Fixed{}).Interpret(op, c.raw, c.body)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		r := facts.Interpretation
		if r.Rule != "reference-target-v1" || r.Outcome != c.outcome || r.LateEffect != c.late || r.Reason != c.reason || facts.Query != nil || facts.Wait != nil {
			t.Errorf("%s: %v", c.name, facts)
		}
	}
}

// 规则：G1
func TestFixedSimulatorQueryKeepsOwnAndSubjectFinalitySeparate(t *testing.T) {
	op := operation("simulator-queryable", 1, "QUERY", simulatorQuery)
	op.QuerySubject = &v1.QuerySubject{OperationId: &v1.GlobalName{UserId: "u", LocalId: "subject-op"}, AttemptId: &v1.GlobalName{UserId: "u", LocalId: "subject-attempt"}, ExternalKey: "subject-key", TargetScope: target}
	raw := httpObservation()
	raw.QuerySubject = op.QuerySubject
	body := func(readTerminal, applied, terminal, negative string) []byte {
		return []byte(`{"protocol":"lerna-simulator-query-v1","query_external_key":"key-1","query_attempt_id":"attempt-1","query_operation_id":"op-1","read_terminal":` + readTerminal + `,"subject_external_key":"subject-key","subject_attempt_id":"subject-attempt","subject_operation_id":"subject-op","subject_scope":"` + target + `","applied":` + applied + `,"terminal":` + terminal + `,"negative_proof":` + negative + `,"retry_after_ms":0}`)
	}
	for _, c := range []struct {
		name                                                         string
		body                                                         []byte
		ownOutcome, ownLate, subjectOutcome, subjectLate, subjectWhy string
	}{
		{"subject applied", body("true", "true", "true", "false"), "APPLIED", "RULED_OUT", "APPLIED", "RULED_OUT", "TERMINAL_SUBJECT_PROOF"},
		{"subject proven absent", body("true", "false", "true", "true"), "APPLIED", "RULED_OUT", "NOT_APPLIED", "RULED_OUT", "TERMINAL_SUBJECT_PROOF"},
		// 只查到当前不存在不足以证明未执行（G1）。
		{"subject absent without proof", body("true", "false", "true", "false"), "APPLIED", "RULED_OUT", "UNKNOWN", "MAY_OCCUR", "SUBJECT_NOT_TERMINAL"},
		{"subject not terminal", body("true", "true", "false", "false"), "APPLIED", "RULED_OUT", "APPLIED", "MAY_OCCUR", "SUBJECT_NOT_TERMINAL"},
		{"query read not terminal", body("false", "true", "true", "false"), "UNKNOWN", "MAY_OCCUR", "UNKNOWN", "MAY_OCCUR", "QUERY_RESULT_UNKNOWN"},
	} {
		facts, err := (rules.Fixed{}).Interpret(op, raw, c.body)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		own, subject := facts.Interpretation, facts.Query
		if own.Rule != "reference-query-v1" || own.Outcome != c.ownOutcome || own.LateEffect != c.ownLate {
			t.Errorf("%s: query finality %v", c.name, own)
		}
		if subject == nil || subject.Rule != "reference-query-subject-v1" || subject.Outcome != c.subjectOutcome || subject.LateEffect != c.subjectLate || subject.Reason != c.subjectWhy {
			t.Errorf("%s: subject evidence %+v", c.name, subject)
		}
	}
}

// 规则：G1
func TestFixedModelOutputNeedsTerminalProviderProtocol(t *testing.T) {
	op := operation("model-reference-v1", 1, "MODEL_INFER", modelInference)
	untrusted := httpObservation()
	untrusted.Source = "ADAPTER"
	for _, c := range []struct {
		name                  string
		raw                   *v1.RawObservation
		body                  []byte
		outcome, late, reason string
	}{
		{"untrusted source", untrusted, []byte(`{}`), "UNKNOWN", "MAY_OCCUR", "INSUFFICIENT_EVIDENCE"},
		{"invalid output", httpObservation(), []byte(`{"unexpected":true}`), "UNKNOWN", "MAY_OCCUR", "INSUFFICIENT_EVIDENCE"},
	} {
		facts, err := (rules.Fixed{}).Interpret(op, c.raw, c.body)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		r := facts.Interpretation
		if r.Rule != "reference-model-v1" || r.Outcome != c.outcome || r.LateEffect != c.late || r.Reason != c.reason {
			t.Errorf("%s: %v", c.name, r)
		}
	}
}

// 规则：G1
func TestFixedFileRequiresBoundTerminalEvidence(t *testing.T) {
	op := operation("managed-file", 1, "CREATE", fileDeclaration("ATOMIC_WRITE", true))
	op.Execution.CallDescriptor = &v1.CallDescriptor{Method: "CREATE"}
	observation := func(e *v1.FileEvidence) *v1.RawObservation {
		e.Rule = "managed-file-v1"
		return &v1.RawObservation{Ref: &v1.Ref{Name: &v1.GlobalName{UserId: "u", LocalId: "raw-1"}}, Source: "TRUSTED_IO", Protocol: "FILE", Target: target, OperationId: op.Ref.Name, AttemptId: op.Execution.Attempt.Ref.Name, ExternalKey: "key-1", SendRef: op.Execution.Send.Ref, FileEvidence: e}
	}
	otherSend := observation(&v1.FileEvidence{Terminal: true, NegativeProof: true, ErrorCode: "NOT_PUBLISHED"})
	otherSend.SendRef = &v1.Ref{Name: &v1.GlobalName{UserId: "u", LocalId: "send-2"}}
	for _, c := range []struct {
		name                  string
		raw                   *v1.RawObservation
		outcome, late, reason string
	}{
		{"terminal negative proof", observation(&v1.FileEvidence{Terminal: true, NegativeProof: true, ErrorCode: "NOT_PUBLISHED"}), "NOT_APPLIED", "RULED_OUT", "NOT_PUBLISHED"},
		// 已发布但没有耐久与读回证据：迟到已排除，效果仍未知。
		{"published without readback", observation(&v1.FileEvidence{Terminal: true, Published: true}), "UNKNOWN", "RULED_OUT", "INSUFFICIENT_EVIDENCE"},
		{"not terminal", observation(&v1.FileEvidence{NegativeProof: true}), "UNKNOWN", "MAY_OCCUR", "INSUFFICIENT_EVIDENCE"},
		{"unbound send", otherSend, "UNKNOWN", "MAY_OCCUR", "INSUFFICIENT_EVIDENCE"},
	} {
		facts, err := (rules.Fixed{}).Interpret(op, c.raw, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		r := facts.Interpretation
		if r.Rule != "managed-file-v1" || r.Outcome != c.outcome || r.LateEffect != c.late || r.Reason != c.reason || facts.Query != nil {
			t.Errorf("%s: %v", c.name, r)
		}
	}
}
