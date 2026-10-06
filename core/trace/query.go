package trace

import (
	"context"
	"sort"
	"strings"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// Sources 读取负责方的原交接，并让负责方在独立事务保存接纳回执。
type Sources interface {
	TraceSources(context.Context) ([]*v1.TraceSourceRecord, error)
	LoadTraceSource(context.Context, *v1.CommandIdentity) (*v1.TraceSourceRecord, error)
	AcknowledgeTraceSource(context.Context, *v1.CommandReceipt) error
	TraceSourceHeads(context.Context) ([]*v1.TraceProgress, error)
	AllTraceEvents(context.Context) ([]*v1.TraceEvent, error)
	IndexedTraceEvents(context.Context) (map[string]bool, error)
	IndexTraceEvents(context.Context) error
}

func (s *Service) Recover(ctx context.Context, caller *v1.Caller) error {
	if err := s.Collect(ctx, caller); err != nil {
		return err
	}
	return s.Index(ctx, caller)
}
func (s *Service) Index(ctx context.Context, caller *v1.Caller) error {
	if err := command.CheckCaller(caller, s.user); err != nil {
		return err
	}
	return s.store.(Sources).IndexTraceEvents(ctx)
}
func (s *Service) Collect(ctx context.Context, caller *v1.Caller) error {
	if err := command.CheckCaller(caller, s.user); err != nil {
		return err
	}
	sources := s.store.(Sources)
	all, err := sources.TraceSources(ctx)
	if err != nil {
		return err
	}
	for _, source := range all {
		if err := command.CheckSavedHeaders(source); err != nil {
			return err
		}
		if source.Receipt != nil {
			continue
		}
		c := source.Command
		actor := &v1.Caller{UserId: s.user, IssuerId: c.Header.Identity.IssuerId}
		q, e := s.QueryReceipt(ctx, actor, c.Header.Identity)
		if e != nil {
			return e
		}
		r := q.Receipt
		if q.State == v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
			r, e = s.Accept(ctx, actor, c)
		}
		if e != nil {
			return e
		}
		if r == nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
			return command.Fail("TRACE_NOT_ACCEPTED")
		}
		if e = sources.AcknowledgeTraceSource(ctx, r); e != nil {
			return e
		}
	}
	return nil
}

// Query 的完整性只声明返回的来源集合及截止水位，不推断业务效果。
func (s *Service) Query(ctx context.Context, caller *v1.Caller, q *v1.TraceQuery) (*v1.TraceView, error) {
	if err := command.CheckCaller(caller, s.user); err != nil {
		return nil, err
	}
	if q == nil || q.TaskId == nil && q.OperationId == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if q.ProcessingPurpose != "" && q.ProcessingPurpose != "DIAGNOSTIC" {
		return nil, command.Fail("UNSUPPORTED_PURPOSE")
	}
	if q.TaskId != nil {
		if err := command.CheckName(caller, q.TaskId, s.user, strings.TrimSuffix(s.domain, "/trace"), "task"); err != nil {
			return nil, err
		}
	}
	if q.OperationId != nil {
		if err := command.CheckName(caller, q.OperationId, s.user, strings.TrimSuffix(s.domain, "/trace")+"/ledger", "operation"); err != nil {
			return nil, err
		}
	}
	sources := s.store.(Sources)
	heads, err := sources.TraceSourceHeads(ctx)
	if err != nil {
		return nil, err
	}
	outbox, err := sources.TraceSources(ctx)
	if err != nil {
		return nil, err
	}
	events, err := sources.AllTraceEvents(ctx)
	if err != nil {
		return nil, err
	}
	indexed, err := sources.IndexedTraceEvents(ctx)
	if err != nil {
		return nil, err
	}
	view := &v1.TraceView{Complete: true, MappingVersion: "lerna.m1.local.v1"}
	progress := map[string]*v1.TraceProgress{}
	for _, p := range heads {
		progress[p.SourceStreamId] = p
	}
	for _, p := range progress {
		p.Cutoff = p.SourceHighWater
	}
	seen := map[string]bool{}
	for _, cutoff := range q.Cutoff {
		if cutoff == nil || cutoff.SourceStreamId == "" || seen[cutoff.SourceStreamId] {
			return nil, command.Fail("INVALID_TRACE_CUTOFF")
		}
		seen[cutoff.SourceStreamId] = true
		p := progress[cutoff.SourceStreamId]
		if p != nil && p.SourceKnown && cutoff.SourceSeq > p.SourceHighWater {
			return nil, command.Fail("INVALID_TRACE_CUTOFF")
		}
		if p == nil {
			p = &v1.TraceProgress{SourceStreamId: cutoff.SourceStreamId}
			progress[cutoff.SourceStreamId] = p
		}
		p.Cutoff = cutoff.SourceSeq
	}
	for _, record := range outbox {
		e := record.Command.Event
		if p := progress[e.SourceStreamId]; p != nil && e.SourceSeq <= p.Cutoff && record.Receipt == nil {
			p.PendingReceipts++
			view.PendingReceipts++
		}
	}
	accepted := map[string]map[uint64]bool{}
	indexedSeq := map[string]map[uint64]bool{}
	for _, e := range events {
		p := progress[e.SourceStreamId]
		if p == nil {
			continue
		}
		if accepted[e.SourceStreamId] == nil {
			accepted[e.SourceStreamId] = map[uint64]bool{}
		}
		accepted[e.SourceStreamId][e.SourceSeq] = true
		if indexedSeq[e.SourceStreamId] == nil {
			indexedSeq[e.SourceStreamId] = map[uint64]bool{}
		}
		if indexed[e.Ref.Name.LocalId] {
			indexedSeq[e.SourceStreamId][e.SourceSeq] = true
		} else if e.SourceSeq <= p.Cutoff {
			view.IndexBacklog++
		}
		if indexed[e.Ref.Name.LocalId] && e.SourceSeq <= p.Cutoff && (q.TaskId == nil || proto.Equal(q.TaskId, e.TaskId)) && (q.OperationId == nil || proto.Equal(q.OperationId, e.OperationId)) {
			view.Events = append(view.Events, e)
		}
	}
	for _, p := range progress {
		a := accepted[p.SourceStreamId]
		if !p.SourceKnown {
			view.Complete = false
			view.Sources = append(view.Sources, p)
			continue
		}
		for seq := uint64(1); seq <= p.Cutoff; seq++ {
			if a[seq] && p.AcceptedContiguous == seq-1 {
				p.AcceptedContiguous = seq
			}
			if !a[seq] {
				p.Backlog++
				n := len(p.Gaps)
				if n > 0 && p.Gaps[n-1].Last == seq-1 {
					p.Gaps[n-1].Last = seq
				} else {
					p.Gaps = append(p.Gaps, &v1.TraceGap{First: seq, Last: seq})
				}
			}
		}
		for indexedSeq[p.SourceStreamId][p.IndexedContiguous+1] && p.IndexedContiguous < p.Cutoff {
			p.IndexedContiguous++
		}
		if !p.SourceKnown || p.AcceptedContiguous < p.Cutoff || p.IndexedContiguous < p.Cutoff || p.Cutoff > p.SourceHighWater {
			view.Complete = false
		}
		view.Backlog += p.Backlog
		view.Sources = append(view.Sources, p)
	}
	if len(progress) == 0 {
		view.Complete = false
	}
	sort.Slice(view.Sources, func(i, j int) bool { return view.Sources[i].SourceStreamId < view.Sources[j].SourceStreamId })
	sort.Slice(view.Events, func(i, j int) bool {
		a, b := view.Events[i], view.Events[j]
		if a.SourceStreamId != b.SourceStreamId {
			return a.SourceStreamId < b.SourceStreamId
		}
		return a.SourceSeq < b.SourceSeq
	})
	return view, nil
}

// QuerySources 显示原来源待交接及其回执，不将其视为运行记录的接纳。
func (s *Service) QuerySources(ctx context.Context, caller *v1.Caller) ([]*v1.TraceSourceRecord, error) {
	if err := command.CheckCaller(caller, s.user); err != nil {
		return nil, err
	}
	return s.store.(Sources).TraceSources(ctx)
}
