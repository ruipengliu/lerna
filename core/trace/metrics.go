package trace

import (
	"context"
	"sort"
	"strings"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// MetricFacts 保留来源、接纳和索引各自的事实，不将任一水位解释为业务完成。
type MetricFacts struct {
	Heads              []*v1.TraceProgress
	Sources            []*v1.TraceSourceRecord
	Events             []*v1.TraceEvent
	Indexed            map[string]bool
	AcceptancePosition uint64
	CapturedAtUnixMs   int64
}
type metricFacts interface {
	TraceMetricFacts(context.Context) (*MetricFacts, error)
}

// QueryMetrics 只读原来源及接收方水位；可选诊断未启用时没有丢失或陈旧度样本。
func (s *Service) QueryMetrics(ctx context.Context, caller *v1.Caller) (*v1.TraceMetrics, error) {
	if e := command.CheckCaller(caller, s.user); e != nil {
		return nil, e
	}
	facts, e := s.store.TraceMetricFacts(ctx)
	if e != nil {
		return nil, e
	}
	m := &v1.TraceMetrics{Source: &v1.MetricSource{OwnerDomainId: s.domain, DefinitionVersion: "lerna.m1.trace.v1", CapturedAtUnixMs: facts.CapturedAtUnixMs, Availability: "AVAILABLE", Scope: "ORIGINAL_SOURCE_CUTOFFS_ACCEPTANCE_INDEX_ACK"}, CoverageComplete: len(facts.Heads) > 0, ReceiverAcceptancePosition: facts.AcceptancePosition, Diagnostics: &v1.DiagnosticMetrics{Source: &v1.MetricSource{OwnerDomainId: s.domain, DefinitionVersion: "lerna.m1.diagnostics.v1", CapturedAtUnixMs: facts.CapturedAtUnixMs, Availability: "DISABLED", Scope: "OPTIONAL_SHORT_TRACES_LOGS"}}}
	for _, head := range facts.Heads {
		p := proto.Clone(head).(*v1.TraceProgress)
		p.Cutoff = p.SourceHighWater
		owner := strings.TrimSuffix(s.domain, "/trace")
		if strings.HasSuffix(p.SourceStreamId, "/ledger") {
			owner += "/ledger"
		} else if strings.HasSuffix(p.SourceStreamId, "/content") {
			owner += "/content"
		}
		stream := &v1.TraceStreamMetrics{Source: &v1.MetricSource{OwnerDomainId: owner, DefinitionVersion: "lerna.m1.trace-source.v1", CapturedAtUnixMs: facts.CapturedAtUnixMs, Availability: "AVAILABLE", Scope: "SOURCE_HEAD_AND_ACK:" + p.SourceStreamId}, Progress: p}
		produced, accepted, indexed := map[uint64]bool{}, map[uint64]bool{}, map[uint64]bool{}
		for _, record := range facts.Sources {
			event := record.GetCommand().GetEvent()
			if event.GetSourceStreamId() != p.SourceStreamId || event.GetSourceSeq() == 0 || event.SourceSeq > p.Cutoff {
				continue
			}
			if event.SourceSchemaVersion != 1 || event.ProducerVersion != "lerna-m1-source-v1" || event.MappingVersion != "lerna.m1.local.v1" {
				p.SourceKnown = false
			}
			produced[event.SourceSeq] = true
			if record.Receipt == nil {
				p.PendingReceipts++
			} else {
				stream.Acknowledged++
			}
		}
		for _, event := range facts.Events {
			if event.GetSourceStreamId() != p.SourceStreamId || event.GetSourceSeq() == 0 || event.SourceSeq > p.Cutoff {
				continue
			}
			accepted[event.SourceSeq] = true
			if facts.Indexed[event.GetRef().GetName().GetLocalId()] {
				indexed[event.SourceSeq] = true
			}
		}
		p.AcceptedContiguous, p.Gaps = sequenceCoverage(p.Cutoff, accepted)
		p.IndexedContiguous, _ = sequenceCoverage(p.Cutoff, indexed)
		stream.Accepted = uint64(len(accepted))
		stream.Indexed = uint64(len(indexed))
		p.Backlog = p.Cutoff - stream.Accepted
		stream.IndexBacklog = stream.Accepted - stream.Indexed
		stream.SourceRecordsMissing = p.Cutoff - uint64(len(produced))
		if !p.SourceKnown || stream.SourceRecordsMissing > 0 {
			stream.Source.Availability = "PARTIAL"
			m.Source.Availability = "PARTIAL"
		}
		if !p.SourceKnown || stream.SourceRecordsMissing > 0 || p.AcceptedContiguous < p.Cutoff || p.IndexedContiguous < p.Cutoff {
			m.CoverageComplete = false
		}
		m.Produced += p.SourceHighWater
		m.Accepted += stream.Accepted
		m.Indexed += stream.Indexed
		m.PendingAcknowledgements += p.PendingReceipts
		m.ReceiverBacklog += p.Backlog
		m.IndexBacklog += stream.IndexBacklog
		m.Streams = append(m.Streams, stream)
	}
	sort.Slice(m.Streams, func(i, j int) bool {
		return m.Streams[i].Progress.SourceStreamId < m.Streams[j].Progress.SourceStreamId
	})
	return m, nil
}

// sequenceCoverage 按实际序号形成缺口区间，不按大缺口逐个枚举或分配内存。
func sequenceCoverage(cutoff uint64, seen map[uint64]bool) (contiguous uint64, gaps []*v1.TraceGap) {
	seqs := make([]uint64, 0, len(seen))
	for seq := range seen {
		seqs = append(seqs, seq)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	next := uint64(1)
	for _, seq := range seqs {
		if seq > next {
			gaps = append(gaps, &v1.TraceGap{First: next, Last: seq - 1})
		}
		if seq == contiguous+1 && len(gaps) == 0 {
			contiguous = seq
		}
		if seq == cutoff {
			return contiguous, gaps
		}
		next = seq + 1
	}
	if next <= cutoff {
		gaps = append(gaps, &v1.TraceGap{First: next, Last: cutoff})
	}
	return contiguous, gaps
}
