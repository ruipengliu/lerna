package budget

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type UsageSource interface {
	QueryReports(context.Context, *v1.Caller, *v1.Ref) (*v1.ObservationReports, error)
}
type usageStore interface {
	SaveUsage(context.Context, *v1.UsageReport) error
	LoadUsage(context.Context, *v1.Ref) (*v1.UsageReport, error)
}

func (s *Service) WithUsageSource(source UsageSource) *Service { s.usageSource = source; return s }

// AcceptUsage 保存独立用量回执；费用结算仍须使用原计量与计价依据。
func (s *Service) AcceptUsage(ctx context.Context, caller *v1.Caller, c *v1.AcceptUsageCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "ledger-report" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("accept-usage", c.Usage), "budget.usage", func(tx context.Context) (*v1.Ref, error) {
		u := c.Usage
		if u == nil || u.Ref == nil || u.MeasurementRef == nil || u.BillingSource == nil || u.PriceRuleRef == nil || u.Ref.Name == nil || u.Ref.Name.AuthorityDomainId != s.domain || u.Ref.Name.UserId != s.user || u.Settlement != "PENDING" {
			return nil, command.Fail("INVALID_USAGE")
		}
		source, e := s.usageSource.QueryReports(tx, caller, u.MeasurementRef)
		if e != nil {
			return nil, e
		}
		if source == nil || !proto.Equal(source.Usage.GetUsage(), c.Usage) {
			return nil, command.Fail("INVALID_USAGE_SOURCE")
		}
		old, e := s.store.(usageStore).LoadUsage(tx, u.Ref)
		if e != nil {
			return nil, e
		}
		if old != nil {
			if !proto.Equal(old, u) {
				return nil, command.Fail("USAGE_CONFLICT")
			}
			return old.Ref, nil
		}
		if e = s.settleReport(tx, caller, u); e != nil {
			return nil, e
		}
		return u.Ref, s.store.(usageStore).SaveUsage(tx, u)
	})
}
func (s *Service) QueryUsage(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.UsageReport, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.UsageReport" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(caller, r.Name, s.user, s.domain, "usage"); e != nil {
		return nil, e
	}
	v, e := s.store.(usageStore).LoadUsage(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
