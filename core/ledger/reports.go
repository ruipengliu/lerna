package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type UsageReceiver interface {
	AcceptUsage(context.Context, *v1.Caller, *v1.AcceptUsageCommand) (*v1.CommandReceipt, error)
}
type ReceiptReader interface {
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
}
type TraceReceiver interface {
	Accept(context.Context, *v1.Caller, *v1.AcceptTraceCommand) (*v1.CommandReceipt, error)
	ReceiptReader
}
type reportStore interface {
	SaveReports(context.Context, *v1.ObservationReports) error
	LoadReports(context.Context, *v1.Ref) (*v1.ObservationReports, error)
	AllReports(context.Context) ([]*v1.ObservationReports, error)
}

func (s *Service) WithReports(b UsageReceiver, receipts ReceiptReader, t TraceReceiver) *Service {
	s.usage = b
	s.usageReceipts = receipts
	s.trace = t
	return s
}
func reportHeader(user, issuer, domain, id string) *v1.CommandHeader {
	return &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: user, IssuerId: issuer, TargetDomainId: domain, CommandId: id}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
}
func (s *Service) createReports(ctx context.Context, o *v1.RawObservation, op *v1.Operation) error {
	admission, e := s.starts.QueryAdmission(ctx, &v1.Caller{UserId: s.user, IssuerId: "ledger-report"}, op.AdmissionRef)
	if e != nil {
		return e
	}
	usage := &v1.UsageReport{Ref: command.NewRef(s.user, s.sourceDomain, "usage", "lerna.v1.UsageReport"), BillingSource: o.SendRef, SourceRevision: 1, MeasurementRef: o.Ref, PriceRuleRef: admission.CapabilitySnapshot.RateBasisRef, OperationId: o.OperationId, AttemptId: o.AttemptId, SendRef: o.SendRef, TaskId: o.TaskId, Settlement: "PENDING", Unit: "USD_MICRO", PhysicalSends: 1}
	event := &v1.TraceEvent{Ref: command.NewRef(s.user, s.sourceDomain+"/trace", "trace-event", "lerna.v1.TraceEvent"), EventType: "PHYSICAL_OBSERVATION", TaskId: o.TaskId, OperationId: o.OperationId, AttemptId: o.AttemptId, SendRef: o.SendRef, ObservationRef: o.Ref, BodyRef: o.BodyRef}
	return s.store.(reportStore).SaveReports(ctx, &v1.ObservationReports{ObservationRef: o.Ref, Usage: &v1.AcceptUsageCommand{Header: reportHeader(s.user, "ledger-report", s.sourceDomain, "usage:"+o.Ref.Name.LocalId), Usage: usage}, Trace: &v1.AcceptTraceCommand{Header: reportHeader(s.user, "ledger-report", s.sourceDomain+"/trace", "trace:"+o.Ref.Name.LocalId), Event: event}})
}
func (s *Service) QueryReports(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.ObservationReports, error) {
	if e := s.checkHistory(caller, r, "observation", "lerna.v1.RawObservation"); e != nil {
		return nil, e
	}
	v, e := s.store.(reportStore).LoadReports(ctx, r)
	if v != nil && !proto.Equal(v.ObservationRef, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}

// ProcessReports 分别查询原接收回执并保存源端确认；任何一个目的域失败都保留待交接责任。
func (s *Service) ProcessReports(ctx context.Context, caller *v1.Caller) error {
	if caller.GetUserId() != s.user {
		return command.Fail("PERMISSION_DENIED")
	}
	all, e := s.store.(reportStore).AllReports(ctx)
	if e != nil {
		return e
	}
	actor := &v1.Caller{UserId: s.user, IssuerId: "ledger-report"}
	for _, reports := range all {
		for _, kind := range []string{"usage", "trace"} {
			if kind == "usage" && reports.UsageReceipt != nil || kind == "trace" && reports.TraceReceipt != nil {
				continue
			}
			id := reports.Usage.Header.Identity
			reader := s.usageReceipts
			if kind == "trace" {
				id = reports.Trace.Header.Identity
				reader = s.trace
			}
			q, e := reader.QueryReceipt(ctx, actor, id)
			if e != nil {
				return e
			}
			var r *v1.CommandReceipt
			switch q.State {
			case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED:
				r = q.Receipt
			case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND:
				if kind == "usage" {
					r, e = s.usage.AcceptUsage(ctx, actor, reports.Usage)
				} else {
					r, e = s.trace.Accept(ctx, actor, reports.Trace)
				}
			default:
				return command.Fail("DEPENDENCY_UNAVAILABLE")
			}
			if e != nil {
				return e
			}
			if r.Decision != v1.Decision_DECISION_ACCEPTED {
				return command.Fail("REPORT_REJECTED")
			}
			h := reportHeader(s.user, "ledger-report", s.domain, "ack:"+kind+":"+reports.ObservationRef.Name.LocalId)
			saveAck := func(tx context.Context) (*v1.Ref, error) {
				current, e := s.store.(reportStore).LoadReports(tx, reports.ObservationRef)
				if e != nil {
					return nil, e
				}
				expected := current.Usage.Header.Identity
				if kind == "trace" {
					expected = current.Trace.Header.Identity
				}
				if !proto.Equal(expected, r.Identity) {
					return nil, command.Fail("INVALID_RECEIPT")
				}
				if kind == "usage" {
					current.UsageReceipt = r
				} else {
					current.TraceReceipt = r
				}
				return reports.ObservationRef, s.store.(reportStore).SaveReports(tx, current)
			}
			fingerprint := command.SemanticFingerprint("report-ack", kind, reports.ObservationRef, r)
			if kind == "usage" {
				_, e = s.work.Execute(ctx, actor, h, fingerprint, "ledger.usage_ack", saveAck)
			} else {
				_, e = s.work.Execute(ctx, actor, h, fingerprint, "ledger.trace_ack", saveAck)
			}
			if e != nil {
				return e
			}
		}
	}
	return nil
}
