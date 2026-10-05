package ledger

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/url"
	"unicode/utf8"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type queryResponse struct {
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
	if cap.ProtocolVersion != "lerna-simulator-query-v1" || cap.VerificationBasis != "reference-query-v1" || cap.Effect != "READ" || raw.Source != "TRUSTED_IO" || raw.Protocol != "HTTP" || raw.TransportError != "" || raw.StatusCode != 200 || !proto.Equal(raw.QuerySubject, op.QuerySubject) {
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
	valid, conflict := validQueryObject(body)
	if !valid {
		return nil, conflict
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
func interpretQuery(raw *v1.RawObservation, body []byte, op *v1.Operation) *v1.EffectInterpretation {
	finding := &v1.EffectInterpretation{ObservationRef: raw.Ref, Rule: "reference-query-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "QUERY_RESULT_UNKNOWN"}
	response, conflict := parseQueryResponse(raw, body, op)
	if conflict {
		finding.Reason = "EVIDENCE_CONFLICT"
	}
	if response != nil && *response.ReadTerminal {
		finding.Outcome = "APPLIED"
		finding.LateEffect = "RULED_OUT"
		finding.Reason = "TERMINAL_READ_RECEIPT"
	}
	return finding
}
func validQueryObject(body []byte) (bool, bool) {
	if !utf8.Valid(body) {
		return false, false
	}
	d := json.NewDecoder(bytes.NewReader(body))
	start, e := d.Token()
	if e != nil || start != json.Delim('{') {
		return false, false
	}
	seen := map[string]bool{}
	for d.More() {
		token, e := d.Token()
		if e != nil {
			return false, false
		}
		key, ok := token.(string)
		if !ok {
			return false, false
		}
		switch key {
		case "billing", "query_status", "protocol", "query_external_key", "query_attempt_id", "query_operation_id", "read_terminal", "subject_external_key", "subject_attempt_id", "subject_operation_id", "subject_scope", "applied", "terminal", "negative_proof", "retry_after_ms":
		default:
			return false, false
		}
		if seen[key] {
			return false, true
		}
		seen[key] = true
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			return false, false
		}
	}
	end, e := d.Token()
	if e != nil || end != json.Delim('}') {
		return false, false
	}
	var extra any
	return d.Decode(&extra) == io.EOF, false
}

// applyReconciliationObservation 只追加原责任的证据，不改变其尝试、发送、端点或任务。
func (s *Service) applyReconciliationObservation(ctx context.Context, caller *v1.Caller, query *v1.Operation, raw *v1.RawObservation, body []byte) error {
	q, e := s.store.(reconciliationStore).LoadClosureQuery(ctx, query.ClosureWorkRef)
	if e != nil {
		return e
	}
	if q == nil || q.QueryOperationRef == nil || !proto.Equal(q.QueryOperationRef.Name, query.Ref.Name) || !proto.Equal(q.Work.QuerySubject, query.QuerySubject) {
		return command.Fail("INVALID_QUERY_RELATION")
	}
	p, e := s.QueryReconciliation(ctx, caller, q.Work.QuerySubject.OperationId)
	if e != nil {
		return e
	}
	if p == nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	original, e := s.QueryOperation(ctx, caller, p.OperationId)
	if e != nil {
		return e
	}
	subject := q.Work.QuerySubject
	if original == nil || original.Execution == nil || !proto.Equal(subject.AttemptId, original.Execution.Attempt.Ref.Name) || subject.ExternalKey != original.Execution.Attempt.ExternalKey || subject.TargetScope != original.CapabilitySnapshot.Resource || subject.ExecutorEndpointId != original.ExecutorEndpointId || !proto.Equal(subject.CapabilityRef, original.CapabilitySnapshot.Ref) {
		return command.Fail("INVALID_QUERY_RELATION")
	}
	response, conflict := parseQueryResponse(raw, body, query)
	finding := &v1.ReconciliationFinding{Ref: command.NewRef(s.user, s.domain, "reconciliation-finding", "lerna.v1.ReconciliationFinding"), QueryRef: proto.Clone(q.Ref).(*v1.Ref), OperationId: original.Ref.Name, ObservationRef: raw.Ref, Rule: "reference-query-subject-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "QUERY_RESULT_UNKNOWN"}
	if response != nil && *response.ReadTerminal && (response.QueryStatus == "" || response.QueryStatus == "AVAILABLE") {
		finding.Reason = "SUBJECT_NOT_TERMINAL"
		if *response.Applied {
			finding.Outcome = "APPLIED"
		}
		if *response.Terminal && (*response.Applied || response.NegativeProof) {
			finding.LateEffect = "RULED_OUT"
			finding.Reason = "TERMINAL_SUBJECT_PROOF"
			if !*response.Applied {
				finding.Outcome = "NOT_APPLIED"
			}
		}
	}
	if response != nil && *response.ReadTerminal && response.QueryStatus == "RETENTION_EXPIRED" {
		finding.Reason = "QUERY_RETENTION_EXPIRED"
	}
	if response != nil && *response.ReadTerminal && response.QueryStatus == "TEMPORARILY_UNAVAILABLE" {
		finding.Reason = "QUERY_TEMPORARILY_UNAVAILABLE"
	}
	if conflict {
		finding.Reason = "EVIDENCE_CONFLICT"
	}
	if e = s.store.(reconciliationStore).SaveReconciliationFinding(ctx, finding); e != nil {
		return e
	}
	q.Ref.Revision++
	q.ObservationRef = raw.Ref
	q.InterpretationRef = finding.Ref
	q.State = "OBSERVED"
	q.Reason = finding.Reason
	if response != nil {
		q.RetryAfterMs = response.RetryAfterMs
	}
	p.LastObservationRef = raw.Ref
	previousOutcome, previousLate := original.Effect.Outcome, original.Effect.LateEffect
	manualPause := p.State == "PAUSED" && p.PauseReason == "USER_PAUSED"
	original.Ref.Revision++
	original.Effect.Ref.Revision++
	original.Effect.EvidenceRefs = append(original.Effect.EvidenceRefs, raw.Ref)
	if finding.Outcome != "UNKNOWN" {
		if original.Effect.Outcome != "UNKNOWN" && original.Effect.Outcome != finding.Outcome {
			original.Effect.EvidenceConflict = true
		}
		if !original.Effect.EvidenceConflict {
			original.Effect.Outcome = finding.Outcome
			original.Effect.LateEffect = finding.LateEffect
		}
	}
	if conflict {
		original.Effect.EvidenceConflict = true
	}
	if original.Effect.EvidenceConflict {
		original.Effect.Outcome = "UNKNOWN"
		original.Effect.LateEffect = "MAY_OCCUR"
		original.Lifecycle = "ACTIVE"
	}
	if finding.LateEffect == "RULED_OUT" && !original.Effect.EvidenceConflict {
		original.Dispatch = "SEALED"
		original.Lifecycle = "SETTLED"
		original.ClosureEvidenceRefs = append(original.ClosureEvidenceRefs, finding.Ref)
	}
	original.EffectRef = original.Effect.Ref
	if e = s.store.SaveOperation(ctx, original); e != nil {
		return e
	}
	if query.Lifecycle != "SETTLED" {
		p.State = "PAUSED"
		p.PauseReason = "QUERY_RESULT_UNKNOWN"
	} else if original.Lifecycle == "SETTLED" {
		p.State = "COMPLETED"
		p.ActiveQueryRef = nil
	} else {
		_, now, e := s.store.LedgerPosition(ctx)
		if e != nil {
			return e
		}
		p.State = "WAITING"
		if original.Effect.Outcome != previousOutcome || original.Effect.LateEffect != previousLate {
			p.NoProgressCount = 0
		} else {
			p.NoProgressCount++
		}
		p.ActiveQueryRef = nil
		delay, e := reconciliationDelay(p.Limits, p.NoProgressCount, q.RetryAfterMs)
		if e != nil {
			return e
		}
		p.NextReconcileAtUnixMs = now + delay
		cap, e := s.reconciliationTasks.QueryCapability(ctx, caller, p.QueryCapabilityRef)
		if e != nil {
			return e
		}
		if reason := reconciliationLimitReason(p, now, cap); reason != "" {
			p.State = "PAUSED"
			p.PauseReason = reason
		}
	}
	if finding.Reason == "QUERY_RETENTION_EXPIRED" && p.State != "COMPLETED" {
		p.State = "PAUSED"
		p.PauseReason = finding.Reason
	}
	if manualPause && p.State == "WAITING" {
		p.State = "PAUSED"
		p.PauseReason = "USER_PAUSED"
	}
	if p.State == "COMPLETED" {
		p.PauseReason = ""
	}
	jobs, e := s.store.LedgerJobs(ctx, original.Ref.Name)
	if e != nil {
		return e
	}
	for _, j := range jobs {
		switch j.JobType {
		case "EXECUTE_OPERATION":
			if original.Lifecycle != "SETTLED" {
				continue
			}
			j.State = "COMPLETED"
		case "RECONCILE_OPERATION":
			j.ReadyAtUnixMs = p.NextReconcileAtUnixMs
			switch p.State {
			case "COMPLETED":
				j.State = "COMPLETED"
			case "PAUSED":
				j.State = "BLOCKED"
			default:
				j.State = "WAITING"
			}
		default:
			continue
		}
		j.Ref.Revision++
		if j.JobType == "RECONCILE_OPERATION" {
			p.JobRef = j.Ref
		}
		j.ProcessInstance = ""
		j.LeaseUntilUnixMs = 0
		j.WaitingReason = p.PauseReason
		if e = s.store.SaveLedgerJob(ctx, j); e != nil {
			return e
		}
	}
	return s.saveQueryAndPlan(ctx, p, q)
}

// reconciliationDelay 保存抽样结果；重复查询与重启只读取既有到期时间。
func reconciliationDelay(l *v1.ReconciliationLimits, n uint32, retryAfter int64) (int64, error) {
	upper := l.InitialDelayMs
	for i := uint32(0); i < n && upper < l.MaxDelayMs; i++ {
		upper = min(l.MaxDelayMs, upper*2)
	}
	draw, e := rand.Int(rand.Reader, big.NewInt(upper+1))
	if e != nil {
		return 0, e
	}
	return max(draw.Int64(), retryAfter), nil
}

// applyOriginalReconciliationFact 迟到的原始响应独立于调度领取和暂停状态，仍然更新既有责任。
func (s *Service) applyOriginalReconciliationFact(ctx context.Context, c *v1.Caller, op *v1.Operation) error {
	p, e := s.QueryReconciliation(ctx, c, op.Ref.Name)
	if e != nil || p == nil {
		return e
	}
	if op.Lifecycle == "SETTLED" && p.ActiveQueryRef == nil {
		p.State = "COMPLETED"
		p.PauseReason = ""
		if e = s.saveReconciliationJob(ctx, p); e != nil {
			return e
		}
	}
	p.Ref.Revision++
	return s.saveReconciliation(ctx, p)
}
