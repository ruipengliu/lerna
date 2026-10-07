package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// MetricFacts 是本负责方的一次只读观察，不能据此领取工作或裁决效果。
type MetricFacts struct {
	Operations       []*v1.Operation
	Reconciliations  []*v1.Reconciliation
	Revision         uint64
	CapturedAtUnixMs int64
}
type metricFacts interface {
	LedgerMetricFacts(context.Context) (*MetricFacts, error)
}

var unknownAgeBounds = [...]uint64{60_000, 300_000, 3_600_000, 86_400_000}

// QueryMetrics 包含关闭任务的原动作和全部核对计划，不从运行记录推导业务结论。
func (s *Service) QueryMetrics(ctx context.Context, caller *v1.Caller) (*v1.LedgerMetrics, error) {
	if e := command.CheckCaller(caller, s.user); e != nil {
		return nil, e
	}
	facts, e := s.store.LedgerMetricFacts(ctx)
	if e != nil {
		return nil, e
	}
	m := &v1.LedgerMetrics{Source: &v1.MetricSource{OwnerDomainId: s.domain, DefinitionVersion: "lerna.m1.ledger.v1", CapturedAtUnixMs: facts.CapturedAtUnixMs, SourceRevision: proto.Uint64(facts.Revision), Availability: "AVAILABLE", Scope: "ALL_OPERATIONS_INCLUDING_CLOSED_TASKS"}, Unknown: &v1.UnknownMetrics{AgeBasis: "FIRST_POSSIBLE_SEND_APPROXIMATE_WALL"}, Reconciliation: &v1.ReconciliationMetrics{DurationAvailability: "MISSING_ORIGINAL_UNKNOWN_AND_COMPLETION_ENDPOINTS"}}
	u, r := m.Unknown, m.Reconciliation
	unknown := map[string]bool{}
	var buckets [5]uint64
	for _, op := range facts.Operations {
		if op.Ref == nil || op.Ref.Name == nil || op.Effect == nil || (op.Effect.Outcome != "UNKNOWN" && op.Effect.Outcome != "APPLIED" && op.Effect.Outcome != "NOT_APPLIED") {
			m.UninterpretableOperations++
			continue
		}
		if op.Effect.Outcome != "UNKNOWN" {
			if op.Effect.LateEffect == "MAY_OCCUR" {
				m.KnownEffectLateMayOccur++
			}
			continue
		}
		u.Total++
		if op.QuerySubject != nil || op.GetCapabilitySnapshot().GetAction() == "QUERY" {
			m.UnknownQueryOperations++
		} else {
			m.UnknownBusinessOperations++
		}
		unknown[op.Ref.Name.LocalId] = true
		attempt := op.GetExecution().GetAttempt()
		if attempt.GetCapabilities() == nil {
			u.QueryabilityMissing++
		} else if attempt.GetCapabilities().GetQueryable() {
			u.Queryable++
		} else {
			u.Unqueryable++
		}
		switch op.Effect.LateEffect {
		case "MAY_OCCUR":
			u.LateMayOccur++
		case "RULED_OUT":
			u.LateRuledOut++
		default:
			u.LateUnknown++
		}
		started := attempt.GetFirstPossibleSendAtUnixMs()
		if started <= 0 {
			u.AgeMissing++
			continue
		}
		if facts.CapturedAtUnixMs <= 0 || started > facts.CapturedAtUnixMs {
			u.AgeClockInvalid++
			continue
		}
		age := uint64(facts.CapturedAtUnixMs - started)
		u.AgeSamples++
		if u.OldestAgeMs == nil || age > *u.OldestAgeMs {
			u.OldestAgeMs = proto.Uint64(age)
		}
		index := len(unknownAgeBounds)
		for i, bound := range unknownAgeBounds {
			if age <= bound {
				index = i
				break
			}
		}
		buckets[index]++
	}
	if u.AgeSamples > 0 {
		for i, count := range buckets {
			b := &v1.AgeBucket{Count: count}
			if i < len(unknownAgeBounds) {
				b.UpperBoundMs = proto.Uint64(unknownAgeBounds[i])
			}
			u.AgeBuckets = append(u.AgeBuckets, b)
		}
	}
	for _, plan := range facts.Reconciliations {
		r.Total++
		switch plan.State {
		case "READY":
			r.Ready++
		case "IN_PROGRESS":
			r.InProgress++
		case "WAITING":
			r.Waiting++
		case "PAUSED":
			r.Paused++
		case "COMPLETED":
			r.Completed++
		default:
			r.Other++
		}
		if plan.OperationId != nil && unknown[plan.OperationId.LocalId] {
			r.UnknownCovered++
			delete(unknown, plan.OperationId.LocalId)
		}
	}
	r.Active = r.Ready + r.InProgress
	r.Unfinished = r.Total - r.Completed
	r.UnknownUncovered = uint64(len(unknown))
	if m.UninterpretableOperations > 0 || r.Other > 0 || u.AgeMissing > 0 || u.AgeClockInvalid > 0 || u.QueryabilityMissing > 0 {
		m.Source.Availability = "PARTIAL"
	}
	return m, nil
}
