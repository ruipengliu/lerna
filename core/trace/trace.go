// Package trace 保存白名单结构化事件及内容引用，不接收任意正文。
package trace

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type Store interface {
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

func New(s Store, w Work, source Source, user, domain string) *Service {
	return &Service{s, w, source, user, domain}
}
func (s *Service) Accept(ctx context.Context, caller *v1.Caller, c *v1.AcceptTraceCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "ledger-report" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("accept-trace", c.Event), "trace.accept", func(tx context.Context) (*v1.Ref, error) {
		ev := c.Event
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
