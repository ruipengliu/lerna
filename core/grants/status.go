package grants

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// QueryGrantStatus 计算诊断展示值，不产生出口许可，也不改写授权历史。
func (s *Service) QueryGrantStatus(ctx context.Context, caller *v1.Caller, n *v1.GlobalName) (*v1.GrantStatus, error) {
	g, e := s.QueryCurrentGrant(ctx, caller, n)
	if e != nil || g == nil {
		return nil, e
	}
	count, e := s.store.GrantUseCount(ctx, g.UsePoolId)
	if e != nil {
		return nil, e
	}
	now, e := s.store.ReadAuthorityTime(ctx)
	if e != nil {
		return nil, e
	}
	status := &v1.GrantStatus{Grant: g, OccupiedAdmissions: count, Expired: now < g.ValidFromUnixMs || now >= g.ValidUntilUnixMs, ObservedAtUnixMs: now, DisplayStatus: g.Status}
	if g.MaxAdmissions > 0 {
		remaining := uint64(0)
		if count < g.MaxAdmissions {
			remaining = g.MaxAdmissions - count
		}
		status.RemainingAdmissions = &remaining
	}
	switch {
	case g.Status == "REVOKED" && g.RevocationCompletion == "PENDING":
		status.DisplayStatus = "REVOKING"
	case g.Status == "REVOKED":
		status.DisplayStatus = "REVOKED"
	case status.Expired:
		status.DisplayStatus = "EXPIRED"
	case status.RemainingAdmissions != nil && *status.RemainingAdmissions == 0:
		status.DisplayStatus = "EXHAUSTED"
	}
	return status, nil
}
