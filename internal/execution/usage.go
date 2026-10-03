package execution

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

func (s *Service) usageGet(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, q api.Query, p OperationIDInput) (api.UsageSnapshot, error) {
	var r operationRecord
	if p.OperationID != q.TargetID {
		return api.UsageSnapshot{}, api.E("invalid_request", "operation_target_mismatch")
	}
	_, err := st.Read(ctx, sc, Namespace+".operations", p.OperationID, 0, &r)
	if err != nil {
		return api.UsageSnapshot{}, err
	}
	if err = disclose(a, r); err != nil {
		return api.UsageSnapshot{}, err
	}
	snapshot := api.UsageSnapshot{SourceRef: sc.Ref(p.OperationID, r.Revision), UsageRevision: r.Revision, Cumulative: r.Operation.Usage, SpendingClosed: r.NewAttemptsClosed, UsageFinal: r.Operation.UsageFinal, ProofRefs: r.Operation.EvidenceRefs}
	snapshot.UsageDigest, err = api.Digest(snapshot)
	return snapshot, err
}
