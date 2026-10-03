package brain

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// UsageProof只有原账务与物理出口元数据，不含Snapshot/Encoding/回复正文。
// 字段及准确JSON顺序沿用既有usage proof，保留原publication身份/字节。
type UsageProof struct {
	Snapshot         api.UsageSnapshot `json:"snapshot"`
	CallID           string            `json:"call_id"`
	PhysicalRequests uint64            `json:"physical_requests"`
	SendStarted      bool              `json:"send_started"`
	Phase            string            `json:"phase"`
}

type AccountingFacts struct {
	Proof           UsageProof
	OriginalCommand api.ObjectRef
	TaskRef         api.ObjectRef
	ModelProfileRef api.ComponentRef
	UseRefs         []api.ObjectRef
}

// AccountingContent只允许宿主按当前原账本核准最低invoice；不能接纳任意正文。
// 实现必须核原authority/版本/金额/CallID/Use身份，采用仅账务用途的独立Policy。
type AccountingContent interface {
	PublishUsageProof(context.Context, runtime.Scope, runtime.Auth, Publication, AccountingFacts) (api.ContentRef, error)
}

func accountingFacts(scope runtime.Scope, d decision) AccountingFacts {
	u := api.UsageSnapshot{SourceRef: scope.Ref(d.Record.DecisionID, d.Record.Revision), UsageRevision: d.Record.Revision, Cumulative: append([]api.Amount{}, d.Record.Usage...), SpendingClosed: d.Record.Status == "completed" || d.Record.Status == "cancelled" || d.Record.Status == "failed", UsageFinal: d.Record.UsageFinal, ProofRefs: []api.ContentRef{}}
	if len(u.Cumulative) == 0 {
		for _, unit := range d.Input.Limits {
			u.Cumulative = append(u.Cumulative, api.Amount{Unit: unit.Unit, Value: "0"})
		}
	}
	return AccountingFacts{Proof: UsageProof{u, d.CallID, d.Record.PhysicalRequestCount, d.Record.SendStarted, d.Phase}, OriginalCommand: scope.Ref(d.CommandID, 1), TaskRef: d.Input.TaskRef, ModelProfileRef: d.Input.ModelProfileRef, UseRefs: append([]api.ObjectRef{}, d.Input.UseRefs...)}
}

// AccountingFacts读取原主账元数据，不hydrate私有正文或读取Content。
// 此为受信宿主的小用例，不新增公开线方法/扩大ordinary数据用途。
func (s *Service) AccountingFacts(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, ref api.ObjectRef) (AccountingFacts, error) {
	if ref.TenantID != scope.TenantID || ref.OwnerID != scope.OwnerID || auth.TenantID != scope.TenantID || auth.SubjectID != scope.OwnerID || !auth.HasRole("usage_reporter") {
		return AccountingFacts{}, api.E("forbidden", "model_accounting_authority_required")
	}
	var d decision
	if _, err := store.Read(ctx, scope, records, ref.ObjectID, 0, &d); err != nil {
		return AccountingFacts{}, err
	}
	if d.Record.DecisionID != ref.ObjectID || d.Input.DecisionID != ref.ObjectID || d.Record.OwnerID != scope.OwnerID || d.Record.Revision == 0 || d.Record.PhysicalRequestCount > 1 || (!d.Record.SendStarted && d.Record.PhysicalRequestCount != 0) || !api.ValidID(d.CommandID) || !api.ValidID(d.CallID) {
		return AccountingFacts{}, api.E("dependency_unavailable", "original_model_accounting_incomplete")
	}
	return accountingFacts(scope, d), nil
}
