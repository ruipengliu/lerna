package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/url"
	"unicode/utf8"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type interpretationStore interface {
	SaveInterpretation(context.Context, *v1.EffectInterpretation) error
	LoadInterpretation(context.Context, *v1.Ref) (*v1.EffectInterpretation, error)
}

func interpretationRef(o *v1.Ref) *v1.Ref {
	return &v1.Ref{Name: &v1.GlobalName{UserId: o.Name.UserId, AuthorityDomainId: o.Name.AuthorityDomainId, ObjectKind: "interpretation", LocalId: o.Name.LocalId}, Revision: 1, SchemaId: "lerna.v1.EffectInterpretation"}
}

// InterpretObservation 只解释已经持久保存的原证据；适配器声明和 HTTP 成功码都不能单独证明效果。
func (s *Service) InterpretObservation(ctx context.Context, caller *v1.Caller, c *v1.InterpretObservationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "ledger-interpretation" && caller.GetIssuerId() != "host" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("interpret-observation", c.ObservationRef), "ledger.interpret", func(tx context.Context) (*v1.Ref, error) {
		raw, e := s.QueryObservation(tx, caller, c.ObservationRef)
		if e != nil {
			return nil, e
		}
		if raw == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		ref := interpretationRef(raw.Ref)
		old, e := s.store.LoadInterpretation(tx, ref)
		if e != nil {
			return nil, e
		}
		if old != nil {
			return old.Ref, nil
		}
		op, e := s.QueryOperation(tx, caller, raw.OperationId)
		if e != nil {
			return nil, e
		}
		body, e := s.observations.Read(tx, caller, raw.BodyRef)
		if e != nil {
			return nil, e
		}
		if body == nil || body.Status != "AVAILABLE" {
			return nil, command.Fail("CONTENT_UNUSABLE")
		}
		facts, e := s.interpretEvidence(op, raw, body.RawBody)
		if e != nil {
			return nil, e
		}
		finding := facts.Interpretation
		finding.Ref = ref
		var waitSend *v1.PhysicalSend
		if wait := facts.Wait; wait != nil {
			send := executionSend(op.Execution, raw.SendRef)
			if send != nil {
				send.Ref.Revision++
				send.ApiWait = wait
				waitSend = send
				if proto.Equal(send.Ref.Name, op.Execution.Send.Ref.Name) {
					op.ApiWait = wait
				}
			}
		}
		if e = s.store.SaveInterpretation(tx, finding); e != nil {
			return nil, e
		}
		admission, e := s.starts.QueryAdmission(tx, caller, op.AdmissionRef)
		if e != nil {
			return nil, e
		}
		if e = s.store.SaveTraceSource(tx, "ledger", &v1.TraceEvent{EventType: "EFFECT_INTERPRETED", SourceRecordRef: finding.Ref, TaskId: admission.TaskId, OperationId: op.Ref.Name, AttemptId: raw.AttemptId, SendRef: raw.SendRef, ObservationRef: raw.Ref, RelatedRefs: []*v1.Ref{op.EffectRef, op.AdmissionRef}, EffectOutcome: finding.Outcome, LateEffect: finding.LateEffect, ReasonCode: finding.Reason}); e != nil {
			return nil, e
		}
		op.Ref.Revision++
		op.Effect.Ref.Revision++
		if e = s.projectPhysicalEvidence(tx, op, finding); e != nil {
			return nil, e
		}
		if op.Effect.LateEffect == "RULED_OUT" && !op.Effect.EvidenceConflict {
			op.Dispatch = "SEALED"
			op.Lifecycle = "SETTLED"
		} else {
			op.Lifecycle = "ACTIVE"
		}
		op.EffectRef = op.Effect.Ref
		jobs, e := s.store.LedgerJobs(tx, op.Ref.Name)
		if e != nil {
			return nil, e
		}
		for _, j := range jobs {
			if j.JobType != "EXECUTE_OPERATION" || (op.Lifecycle != "SETTLED" && !proto.Equal(raw.SendRef.Name, op.Execution.Send.Ref.Name)) {
				continue
			}
			j.Ref.Revision++
			if op.Lifecycle == "SETTLED" {
				j.State = "COMPLETED"
			} else {
				j.State = "WAITING"
				if op.ApiWait != nil {
					j.ReadyAtUnixMs = op.ApiWait.ReadyAtUnixMs
					j.WaitingReason = "API_429_" + op.ApiWait.Category
				}
			}
			j.ProcessInstance = ""
			j.LeaseUntilUnixMs = 0
			if e = s.store.SaveLedgerJob(tx, j); e != nil {
				return nil, e
			}
		}
		if e = s.saveOperation(tx, op); e != nil {
			return nil, e
		}
		if waitSend != nil {
			if e = s.store.SaveTraceSource(tx, "ledger", &v1.TraceEvent{EventType: "API_WAIT_RECORDED", SourceRecordRef: waitSend.Ref, OriginCommand: c.Header.Identity, TaskId: admission.TaskId, OperationId: op.Ref.Name, AttemptId: raw.AttemptId, SendRef: raw.SendRef, ObservationRef: raw.Ref, ReasonCode: "API_429_" + waitSend.ApiWait.Category, RelatedRefs: []*v1.Ref{op.Ref, op.AdmissionRef, op.Execution.Attempt.Ref, finding.Ref}}); e != nil {
				return nil, e
			}
		}
		if op.QuerySubject != nil {
			if e = s.applyReconciliationObservation(tx, caller, op, raw, facts.Query); e != nil {
				return nil, e
			}
		} else if e = s.applyOriginalReconciliationFact(tx, caller, op); e != nil {
			return nil, e
		}
		return ref, nil
	})
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
	valid, conflict := validSimulatorObject(body)
	if !valid {
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
func (s *Service) QueryInterpretation(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.EffectInterpretation, error) {
	if e := s.checkHistory(caller, r, "interpretation", "lerna.v1.EffectInterpretation"); e != nil {
		return nil, e
	}
	v, e := s.store.LoadInterpretation(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
func (s *Service) ProcessInterpretations(ctx context.Context, caller *v1.Caller) error {
	if caller.GetUserId() != s.user {
		return command.Fail("PERMISSION_DENIED")
	}
	all, e := s.store.AllReports(ctx)
	if e != nil {
		return e
	}
	actor := &v1.Caller{UserId: s.user, IssuerId: "ledger-interpretation"}
	for _, reports := range all {
		h := reportHeader(s.user, actor.IssuerId, s.domain, "interpret:"+reports.ObservationRef.Name.LocalId)
		r, e := s.InterpretObservation(ctx, actor, &v1.InterpretObservationCommand{Header: h, ObservationRef: reports.ObservationRef})
		if e != nil {
			return e
		}
		if r.Decision != v1.Decision_DECISION_ACCEPTED {
			return command.Fail("INTERPRETATION_REJECTED")
		}
	}
	return nil
}

// validSimulatorObject 拒绝重复字段，避免 JSON 的后值覆盖前值隐藏相反证据。
func validSimulatorObject(body []byte) (bool, bool) {
	if !utf8.Valid(body) {
		return false, false
	}
	d := json.NewDecoder(bytes.NewReader(body))
	opening, e := d.Token()
	if e != nil || opening != json.Delim('{') {
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
		case "protocol", "external_key", "attempt_id", "applied", "terminal", "applied_at_unix_nano", "billing":
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
	closing, e := d.Token()
	if e != nil || closing != json.Delim('}') {
		return false, false
	}
	var extra any
	return d.Decode(&extra) == io.EOF, false
}
