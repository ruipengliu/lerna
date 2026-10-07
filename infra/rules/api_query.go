package rules

import (
	"encoding/json"
	"net/url"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/ledger"
	"google.golang.org/protobuf/proto"
)

type queryResponse struct {
	Account            string `json:"account"`
	Origin             string `json:"origin"`
	QueryStatus        string `json:"query_status"`
	Protocol           string `json:"protocol"`
	QueryExternalKey   string `json:"query_external_key"`
	QueryAttemptID     string `json:"query_attempt_id"`
	QueryOperationID   string `json:"query_operation_id"`
	ReadTerminal       *bool  `json:"read_terminal"`
	SubjectExternalKey string `json:"subject_external_key"`
	SubjectAttemptID   string `json:"subject_attempt_id"`
	SubjectOperationID string `json:"subject_operation_id"`
	SubjectScope       string `json:"subject_scope"`
	Applied            *bool  `json:"applied"`
	Terminal           *bool  `json:"terminal"`
	NegativeProof      bool   `json:"negative_proof"`
	RetryAfterMs       int64  `json:"retry_after_ms"`
}

func parseQueryResponse(raw *v1.RawObservation, body []byte, op *v1.Operation) (*queryResponse, bool) {
	if raw == nil || op == nil || op.Execution == nil || op.QuerySubject == nil || op.Execution.Attempt.Capabilities == nil {
		return nil, false
	}
	cap := op.Execution.Attempt.Capabilities
	if cap.ProtocolVersion != "lerna-reference-api-query-v1" || cap.VerificationBasis != "reference-api-v1" || cap.Effect != "READ" || raw.Source != "TRUSTED_IO" || raw.Protocol != "HTTP" || raw.TransportError != "" || raw.Redacted || raw.StatusCode != 200 || !proto.Equal(raw.QuerySubject, op.QuerySubject) {
		return nil, false
	}
	if _, e := url.Parse(raw.Target); e != nil || raw.Target != op.QuerySubject.TargetScope || !command.APIObservationMatches(raw, op.Execution.CallDescriptor, op.Execution.Attempt) {
		return nil, false
	}
	values, duplicate := command.StrictJSONObject(body, "billing", "query_status", "protocol", "query_external_key", "query_attempt_id", "query_operation_id", "read_terminal", "subject_external_key", "subject_attempt_id", "subject_operation_id", "subject_scope", "applied", "terminal", "negative_proof", "retry_after_ms", "account", "origin")
	if values == nil {
		return nil, duplicate
	}
	var response queryResponse
	if json.Unmarshal(body, &response) != nil || response.Protocol != cap.ProtocolVersion || response.QueryExternalKey != op.Execution.Attempt.ExternalKey || response.QueryExternalKey != raw.ExternalKey || response.QueryAttemptID != op.Execution.Attempt.Ref.Name.LocalId || response.QueryOperationID != op.Ref.Name.LocalId || response.ReadTerminal == nil || response.Applied == nil || response.Terminal == nil || response.RetryAfterMs < 0 || response.RetryAfterMs > 86400000 {
		return nil, false
	}
	if response.QueryStatus != "" && response.QueryStatus != "AVAILABLE" && response.QueryStatus != "TEMPORARILY_UNAVAILABLE" && response.QueryStatus != "RETENTION_EXPIRED" {
		return nil, false
	}
	if response.Account != op.Execution.CallDescriptor.ApiDescriptor.Binding.Account || response.Origin != op.Execution.CallDescriptor.ApiDescriptor.Binding.Origin {
		return nil, false
	}
	subject := op.QuerySubject
	if response.SubjectExternalKey != subject.ExternalKey || response.SubjectAttemptID != subject.AttemptId.LocalId || response.SubjectOperationID != subject.OperationId.LocalId || response.SubjectScope != subject.TargetScope {
		return nil, false
	}
	return &response, false
}

func queryEvidence(response *queryResponse, conflict bool) *ledger.QueryEvidence {
	fact := &ledger.QueryEvidence{Rule: "reference-api-query-subject-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "QUERY_RESULT_UNKNOWN", Conflict: conflict}
	if response != nil {
		fact.RetryAfterMs = &response.RetryAfterMs
	}
	if response != nil && *response.ReadTerminal && (response.QueryStatus == "" || response.QueryStatus == "AVAILABLE") {
		fact.Reason = "SUBJECT_NOT_TERMINAL"
		if *response.Applied {
			fact.Outcome = "APPLIED"
		}
		if *response.Terminal && (*response.Applied || response.NegativeProof) {
			fact.LateEffect, fact.Reason = "RULED_OUT", "TERMINAL_SUBJECT_PROOF"
			if !*response.Applied {
				fact.Outcome = "NOT_APPLIED"
			}
		}
	}
	if response != nil && *response.ReadTerminal && response.QueryStatus == "RETENTION_EXPIRED" {
		fact.Reason = "QUERY_RETENTION_EXPIRED"
	}
	if response != nil && *response.ReadTerminal && response.QueryStatus == "TEMPORARILY_UNAVAILABLE" {
		fact.Reason = "QUERY_TEMPORARILY_UNAVAILABLE"
	}
	if conflict {
		fact.Reason = "EVIDENCE_CONFLICT"
	}
	return fact
}
