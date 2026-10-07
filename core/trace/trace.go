// Package trace 保存白名单结构化事件及内容引用，不接收任意正文。
package trace

import (
	"context"
	"strings"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
	"google.golang.org/protobuf/proto"
)

// Store 声明事件、原来源交接、索引与指标的全部持久能力。
type Store interface {
	Sources
	metricFacts
	SaveTraceEvent(context.Context, *v1.TraceEvent) error
	LoadTraceEvent(context.Context, *v1.Ref) (*v1.TraceEvent, error)
}
type Work interface {
	Execute(context.Context, *v1.Caller, *v1.CommandHeader, string, string, func(context.Context) (*v1.Ref, error)) (*v1.CommandReceipt, error)
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
}
type Source interface {
	QueryReports(context.Context, *v1.Caller, *v1.Ref) (*v1.ObservationReports, error)
}
type Service struct {
	store        Store
	work         Work
	source       Source
	user, domain string
}

func New(s Store, w Work, source Source, user, domain string) (*Service, error) {
	service := &Service{store: s, work: w, source: source, user: user, domain: domain}
	if err := service.ValidateDependencies(); err != nil {
		return nil, err
	}
	return service, nil
}

// ValidateDependencies 在恢复和命令调用前核对完整依赖，不推进任何责任。
func (s *Service) ValidateDependencies() error {
	return durable.RequireDependencies("trace",
		durable.Dependency{Name: "store", Value: s.store},
		durable.Dependency{Name: "work", Value: s.work},
		durable.Dependency{Name: "source", Value: s.source},
	)
}
func (s *Service) Accept(ctx context.Context, caller *v1.Caller, c *v1.AcceptTraceCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "ledger-report" && !strings.HasPrefix(caller.GetIssuerId(), "trace-source/") {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("accept-trace", c.Event), "trace.accept", func(tx context.Context) (*v1.Ref, error) {
		ev := c.Event
		if ev.GetProducer() != "" {
			if ev == nil || ev.Ref == nil || ev.Ref.Name == nil || ev.Ref.Name.UserId != s.user || ev.Ref.Name.AuthorityDomainId != s.domain || ev.SourceRecordRef == nil || ev.SourceSeq == 0 {
				return nil, command.Fail("INVALID_TRACE_EVENT")
			}
			source, err := s.store.LoadTraceSource(tx, c.Header.Identity)
			if err != nil {
				return nil, err
			}
			if source == nil || !proto.Equal(source.Command, c) {
				return nil, command.Fail("INVALID_TRACE_SOURCE")
			}
			return ev.Ref, s.store.SaveTraceEvent(tx, ev)
		}
		if ev == nil || ev.Ref == nil || ev.Ref.Name == nil || ev.Ref.Name.UserId != s.user || ev.Ref.Name.AuthorityDomainId != s.domain || ev.EventType != "PHYSICAL_OBSERVATION" || ev.ObservationRef == nil || ev.BodyRef == nil {
			return nil, command.Fail("INVALID_TRACE_EVENT")
		}
		source, e := s.source.QueryReports(tx, caller, ev.ObservationRef)
		if e != nil {
			return nil, e
		}
		if source == nil || !proto.Equal(source.Trace, c) {
			return nil, command.Fail("INVALID_TRACE_SOURCE")
		}
		return ev.Ref, s.store.SaveTraceEvent(tx, ev)
	})
}
func (s *Service) QueryEvent(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.TraceEvent, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.TraceEvent" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(caller, r.Name, s.user, s.domain, "trace-event"); e != nil {
		return nil, e
	}
	v, e := s.store.LoadTraceEvent(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
func (s *Service) QueryReceipt(ctx context.Context, caller *v1.Caller, id *v1.CommandIdentity) (*v1.ReceiptQuery, error) {
	return s.work.QueryReceipt(ctx, caller, id)
}
