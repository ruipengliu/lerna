package tasks

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type admissionMetricKey struct{}
type admissionMetricEntry struct {
	started          time.Time
	validated        bool
	originalDecision bool
}
type admissionMetricState struct {
	mu                                                                         sync.Mutex
	instance                                                                   string
	revision                                                                   uint64
	entries, samples, accepted, rejected, replays, invalid, missing, undecided uint64
	durationSum, durationMax                                                   uint64
	buckets                                                                    [8]uint64
	lastSample                                                                 int64
}

var admissionLatencyBounds = [...]uint64{1_000_000, 5_000_000, 10_000_000, 50_000_000, 100_000_000, 500_000_000, 1_000_000_000}

func (s *Service) beginAdmissionMetric(ctx context.Context) (context.Context, *admissionMetricEntry) {
	entry := &admissionMetricEntry{started: time.Now()}
	s.admissionMetrics.mu.Lock()
	s.admissionMetrics.entries++
	s.admissionMetrics.revision++
	s.admissionMetrics.mu.Unlock()
	return context.WithValue(ctx, admissionMetricKey{}, entry), entry
}
func (s *Service) finishAdmissionMetric(entry *admissionMetricEntry, r *v1.CommandReceipt, e error) {
	duration := uint64(time.Since(entry.started))
	m := &s.admissionMetrics
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revision++
	if !entry.validated {
		m.invalid++
		return
	}
	if e != nil || r == nil {
		var failure *command.Failure
		if entry.originalDecision && errors.As(e, &failure) && failure.Detail.CommandAcceptance == v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN {
			m.missing++
		} else {
			m.undecided++
		}
		return
	}
	if !entry.originalDecision {
		m.replays++
		return
	}
	m.samples++
	if r.Decision == v1.Decision_DECISION_ACCEPTED {
		m.accepted++
	} else {
		m.rejected++
	}
	m.durationSum += duration
	m.durationMax = max(m.durationMax, duration)
	m.lastSample = time.Now().UnixMilli()
	index := len(admissionLatencyBounds)
	for i, bound := range admissionLatencyBounds {
		if duration <= bound {
			index = i
			break
		}
	}
	m.buckets[index]++
}

// QueryAdmissionMetrics 只返回当前进程的实际测量；重启和回查原回执不会恢复旧单调时钟。
func (s *Service) QueryAdmissionMetrics(_ context.Context, c *v1.Caller) (*v1.AdmissionMetrics, error) {
	if e := command.CheckCaller(c, s.user); e != nil {
		return nil, e
	}
	m := &s.admissionMetrics
	m.mu.Lock()
	defer m.mu.Unlock()
	availability := "AVAILABLE"
	if m.samples == 0 {
		availability = "NO_SAMPLES"
	}
	if m.missing > 0 {
		availability = "PARTIAL"
		if m.samples == 0 {
			availability = "MISSING"
		}
	}
	result := &v1.AdmissionMetrics{
		Source:          &v1.MetricSource{OwnerDomainId: s.domain, DefinitionVersion: "lerna.m1.admission.v1", CapturedAtUnixMs: time.Now().UnixMilli(), SourceRevision: proto.Uint64(m.revision), Availability: availability, Scope: "PROCESS_INSTANCE:TARGET_MODEL_CLOSURE"},
		ProcessInstance: m.instance, Entries: m.entries, Samples: m.samples, Accepted: m.accepted, Rejected: m.rejected, Replays: m.replays, InvalidHeaders: m.invalid, DecisionsWithoutEndpoint: m.missing, CallsWithoutDecision: m.undecided,
	}
	if m.samples > 0 {
		result.DurationSumNs = proto.Uint64(m.durationSum)
		result.DurationMaxNs = proto.Uint64(m.durationMax)
		result.LastSampleAtUnixMs = proto.Int64(m.lastSample)
		for i, count := range m.buckets {
			bucket := &v1.LatencyBucket{Count: count}
			if i < len(admissionLatencyBounds) {
				bucket.UpperBoundNs = proto.Uint64(admissionLatencyBounds[i])
			}
			result.Buckets = append(result.Buckets, bucket)
		}
	}
	return result, nil
}
