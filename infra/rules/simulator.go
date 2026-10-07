package rules

import (
	"encoding/json"
	"net"
	"net/url"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/ledger"
)

// Simulator 解释原固定模拟写入与独立查询，不持有目标或历史存储能力。
type Simulator struct{}

var _ ledger.EvidenceRules = Simulator{}

// CheckSupported 纯核验原规则资格；完整声明与原描述绑定由编译器独立核验。
func (Simulator) CheckSupported(op *v1.Operation) error {
	cap := op.GetCapabilitySnapshot()
	if cap.GetAdapterRef().GetRevision() != 1 {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	switch cap.GetAdapterRef().GetName().GetLocalId() {
	case "simulator-idempotent", "simulator-idempotent-expiring", "simulator-idempotent-evicting", "simulator-idempotent-queryable", "simulator-queryable", "simulator-opaque":
	default:
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	protocol, basis, effect := "lerna-simulator-v1", "reference-target-v1", "ATOMIC_WRITE"
	switch cap.GetAction() {
	case "CREATE":
	case "QUERY":
		if cap.GetAdapterRef().GetName().GetLocalId() != "simulator-queryable" {
			return command.Fail("PREPARATION_UNRECOVERABLE")
		}
		protocol, basis, effect = "lerna-simulator-query-v1", "reference-query-v1", "READ"
	default:
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if op.Execution != nil {
		declaration := op.Execution.GetAttempt().GetCapabilities()
		if declaration.GetProtocolVersion() != protocol || declaration.GetVerificationBasis() != basis || declaration.GetDeclarationVersion() != "1" || declaration.GetEffect() != effect {
			return command.Fail("PREPARATION_UNRECOVERABLE")
		}
	}
	return nil
}

func (Simulator) Interpret(op *v1.Operation, raw *v1.RawObservation, body []byte) (*ledger.EvidenceFacts, error) {
	facts := &ledger.EvidenceFacts{}
	if op.QuerySubject == nil {
		facts.Interpretation = interpretSimulator(raw, body, op.Execution.Attempt)
		return facts, nil
	}
	response, conflict := parseSimulatorQueryResponse(raw, body, op)
	facts.Interpretation = &v1.EffectInterpretation{ObservationRef: raw.Ref, Rule: "reference-query-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "QUERY_RESULT_UNKNOWN"}
	if conflict {
		facts.Interpretation.Reason = "EVIDENCE_CONFLICT"
	}
	if response != nil && *response.ReadTerminal {
		facts.Interpretation.Outcome, facts.Interpretation.LateEffect, facts.Interpretation.Reason = "APPLIED", "RULED_OUT", "TERMINAL_READ_RECEIPT"
	}
	facts.Query = queryEvidence(response, conflict, "reference-query-subject-v1")
	return facts, nil
}

func interpretSimulator(raw *v1.RawObservation, body []byte, attempt *v1.ExecutionAttempt) *v1.EffectInterpretation {
	r := &v1.EffectInterpretation{ObservationRef: raw.Ref, Rule: "reference-target-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "INSUFFICIENT_EVIDENCE"}
	cap := attempt.GetCapabilities()
	if cap == nil || cap.ProtocolVersion != "lerna-simulator-v1" || cap.VerificationBasis != "reference-target-v1" || raw.Source != "TRUSTED_IO" || raw.Protocol != "HTTP" || raw.TransportError != "" || raw.StatusCode != 200 {
		return r
	}
	endpoint, e := url.Parse(raw.Target)
	if e != nil {
		return r
	}
	address, port, e := net.SplitHostPort(raw.ActualAddress)
	expectedPort := endpoint.Port()
	if expectedPort == "" {
		expectedPort = "80"
	}
	if e != nil || port != expectedPort || !net.ParseIP(address).Equal(net.ParseIP(endpoint.Hostname())) {
		return r
	}
	values, conflict := command.StrictJSONObject(body, "protocol", "external_key", "attempt_id", "applied", "terminal", "applied_at_unix_nano", "billing")
	if values == nil {
		if conflict {
			r.Reason = "EVIDENCE_CONFLICT"
		}
		return r
	}
	var response struct {
		Protocol    string `json:"protocol"`
		ExternalKey string `json:"external_key"`
		AttemptID   string `json:"attempt_id"`
		Applied     *bool  `json:"applied"`
		Terminal    *bool  `json:"terminal"`
	}
	if json.Unmarshal(body, &response) != nil || response.Protocol != cap.ProtocolVersion || response.ExternalKey != attempt.ExternalKey || response.ExternalKey != raw.ExternalKey || response.AttemptID != attempt.Ref.Name.LocalId || response.Applied == nil || response.Terminal == nil {
		return r
	}
	if !*response.Terminal {
		if *response.Applied {
			r.Outcome = "APPLIED"
		}
		r.Reason = "LATE_EFFECT_POSSIBLE"
		return r
	}
	r.Outcome = "NOT_APPLIED"
	if *response.Applied {
		r.Outcome = "APPLIED"
	}
	r.LateEffect = "RULED_OUT"
	r.Reason = "TERMINAL_PROTOCOL_EVIDENCE"
	return r
}
