package rules

import (
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/ledger"
	"google.golang.org/protobuf/proto"
)

// File 解释原受管理单文件规则；发布历史和资源责任仍由原核心持有。
type File struct{}

var _ ledger.EvidenceRules = File{}

// CheckSupported 只检查原固定规则声明，不读取文件或重编原描述。
func (File) CheckSupported(op *v1.Operation) error {
	cap := op.GetCapabilitySnapshot()
	if cap.GetAdapterRef().GetName().GetLocalId() != "managed-file" || cap.AdapterRef.Revision != 1 {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	effect, queryable := "ATOMIC_WRITE", true
	switch cap.Action {
	case "CREATE", "REPLACE":
	case "READ", "QUERY":
		effect, queryable = "READ", false
	case "CLEANUP":
		effect, queryable = "PARTIAL_WRITE", false
	default:
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if op.Execution != nil && !proto.Equal(op.Execution.GetAttempt().GetCapabilities(), &v1.ExecutionCapabilities{Effect: effect, Queryable: queryable, ProtocolVersion: "lerna-managed-file-v1", DeclarationVersion: "1", VerificationBasis: "managed-file-v1"}) {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	return nil
}

func (File) Interpret(op *v1.Operation, raw *v1.RawObservation, _ []byte) (*ledger.EvidenceFacts, error) {
	facts := &ledger.EvidenceFacts{Interpretation: interpretFile(raw, op)}
	if op.QuerySubject != nil {
		read, subject := fileQueryEvidence(raw, op)
		facts.Interpretation = &v1.EffectInterpretation{ObservationRef: raw.Ref, Rule: "reference-query-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "QUERY_RESULT_UNKNOWN"}
		if read {
			facts.Interpretation.Outcome, facts.Interpretation.LateEffect, facts.Interpretation.Reason = "APPLIED", "RULED_OUT", "TERMINAL_READ_RECEIPT"
		}
		facts.Query = subject
	}
	return facts, nil
}

func interpretFile(raw *v1.RawObservation, op *v1.Operation) *v1.EffectInterpretation {
	r := &v1.EffectInterpretation{ObservationRef: raw.Ref, Rule: "managed-file-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "INSUFFICIENT_EVIDENCE"}
	if !command.ManagedFileObservationMatches(raw, op) {
		return r
	}
	e := raw.FileEvidence
	if !e.Terminal {
		return r
	}
	r.LateEffect = "RULED_OUT"
	if e.NegativeProof && !e.Published {
		r.Outcome = "NOT_APPLIED"
		r.Reason = e.ErrorCode
		return r
	}
	if op.Execution.CallDescriptor.Method == "READ" && e.ReadTerminal && e.ReadbackVerified && e.ErrorCode == "" {
		r.Outcome = "APPLIED"
		r.Reason = "FILE_READ_VERIFIED"
		return r
	}
	if op.Execution.CallDescriptor.Method == "CLEANUP" && e.Stage == "CLEANED" && e.DurabilityConfirmed && e.ReadbackVerified && e.ErrorCode == "" {
		r.Outcome = "APPLIED"
		r.Reason = "FILE_CLEANUP_VERIFIED"
		return r
	}
	if e.Published && e.DurabilityConfirmed && e.ReadbackVerified && e.ErrorCode == "" && command.ManagedFileCommitMatches(e.Commit, op) && proto.Equal(e.Commit.SendRef.Name, raw.SendRef.Name) && e.Commit.RootIdentity == raw.ActualAddress {
		r.Outcome = "APPLIED"
		r.Reason = "FILE_DURABLE_READBACK"
	}
	return r
}

func fileQueryEvidence(raw *v1.RawObservation, op *v1.Operation) (bool, *ledger.QueryEvidence) {
	facts := &ledger.QueryEvidence{Rule: "reference-query-subject-v1", Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "QUERY_RESULT_UNKNOWN"}
	if !command.ManagedFileObservationMatches(raw, op) || !proto.Equal(raw.QuerySubject, op.QuerySubject) || op.Execution.CallDescriptor.Method != "QUERY" {
		return false, facts
	}
	facts.RetryAfterMs = new(int64)
	e, subject := raw.FileEvidence, op.QuerySubject
	if !e.ReadTerminal {
		return false, facts
	}
	facts.Reason = "SUBJECT_NOT_TERMINAL"
	if e.Published && e.ReadbackVerified && e.ErrorCode == "" && e.Commit != nil && proto.Equal(e.Commit.OperationId, subject.OperationId) && proto.Equal(e.Commit.AttemptId, subject.AttemptId) && e.Commit.ExternalKey == subject.ExternalKey && e.Commit.Target == subject.TargetScope {
		facts.Outcome = "APPLIED"
		if e.Terminal {
			facts.LateEffect, facts.Reason = "RULED_OUT", "TERMINAL_SUBJECT_PROOF"
		}
	}
	return true, facts
}
