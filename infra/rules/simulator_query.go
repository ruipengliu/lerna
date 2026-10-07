package rules

import (
	"encoding/json"
	"net"
	"net/url"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func parseSimulatorQueryResponse(raw *v1.RawObservation, body []byte, op *v1.Operation) (*queryResponse, bool) {
	if raw == nil || op == nil || op.Execution == nil || op.QuerySubject == nil || op.Execution.Attempt.Capabilities == nil {
		return nil, false
	}
	cap := op.Execution.Attempt.Capabilities
	protocol, basis := "lerna-simulator-query-v1", "reference-query-v1"
	if cap.ProtocolVersion != protocol || cap.VerificationBasis != basis || cap.Effect != "READ" || raw.Source != "TRUSTED_IO" || raw.Protocol != "HTTP" || raw.TransportError != "" || raw.Redacted || raw.StatusCode != 200 || !proto.Equal(raw.QuerySubject, op.QuerySubject) {
		return nil, false
	}
	endpoint, e := url.Parse(raw.Target)
	if e != nil || raw.Target != op.QuerySubject.TargetScope {
		return nil, false
	}
	address, port, e := net.SplitHostPort(raw.ActualAddress)
	expected := endpoint.Port()
	if expected == "" {
		expected = "80"
	}
	if e != nil || port != expected || !net.ParseIP(address).Equal(net.ParseIP(endpoint.Hostname())) {
		return nil, false
	}
	values, duplicate := command.StrictJSONObject(body, "billing", "query_status", "protocol", "query_external_key", "query_attempt_id", "query_operation_id", "read_terminal", "subject_external_key", "subject_attempt_id", "subject_operation_id", "subject_scope", "applied", "terminal", "negative_proof", "retry_after_ms")
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
	subject := op.QuerySubject
	if response.SubjectExternalKey != subject.ExternalKey || response.SubjectAttemptID != subject.AttemptId.LocalId || response.SubjectOperationID != subject.OperationId.LocalId || response.SubjectScope != subject.TargetScope {
		return nil, false
	}
	return &response, false
}
