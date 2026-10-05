package mockapi

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
)

// Version 是参考适配器的实现版本。
const Version = "mockapi.m1"

// Adapter 是模拟 API 的执行适配器：按目标类别如实声明能力，只回报，不写效果；
// 不自行重试，超时或回执丢失回报为无法确定。
type Adapter struct {
	ID         string
	Capability string
	Target     *Target
}

var _ ports.Executor = (*Adapter)(nil)

// New 创建一个模拟 API 适配器，能力标识为 "<id>.put"。
func New(id string, t *Target) *Adapter {
	return &Adapter{ID: id, Capability: id + ".put", Target: t}
}

// AdapterID 返回适配器标识。
func (a *Adapter) AdapterID() string { return a.ID }

// Declarations 按类别分维度声明能力（核心契约 2.4）；未声明的维度取最保守的缺省值。
func (a *Adapter) Declarations() []*lernav1.CapabilityDeclaration {
	d := &lernav1.CapabilityDeclaration{
		AdapterId:             a.ID,
		ImplementationVersion: Version,
		ContractVersion:       ports.ContractVersion,
		CapabilityId:          a.Capability,
		CapabilityVersion:     "1",
		ExternalEffect:        lernav1.ExternalEffect_EXTERNAL_EFFECT_CHANGES_WORLD,
		Idempotency:           &lernav1.Idempotency{},
		Query:                 &lernav1.ResultQuery{},
		LateFinality:          &lernav1.LateFinality{},
		Billing: &lernav1.Billing{
			Billed: true, BillingSource: a.ID, CeilingKnown: true, Ceiling: a.Target.Price, Unit: "micro_usd",
		},
		RequiredArguments: []string{"key", "value"},
		VerificationRefs:  []string{"conformance/executor@m1", "mockapi.target." + a.Target.Class().String()},
		Description:       "把资源 key 设为 value（模拟 API，" + a.Target.Class().String() + "）",
	}
	switch a.Target.Class() {
	case Idempotent:
		d.Idempotency = &lernav1.Idempotency{
			Supported: true, KeyScope: a.ID, ParamsMustMatch: true, KeyValiditySeconds: int64(a.Target.KeyTTL.Seconds()),
		}
		d.LateFinality = &lernav1.LateFinality{Provable: true, Evidence: "200 committed response for the attempt's external key"}
	case Queryable:
		d.Query = &lernav1.ResultQuery{
			Supported: true, Correlation: lernav1.QueryCorrelation_QUERY_CORRELATION_BY_ATTEMPT_ID,
			VisibilityDelayMs: a.Target.QueryLag.Milliseconds(), RetentionSeconds: 7 * 24 * 3600,
		}
		d.LateFinality = &lernav1.LateFinality{Provable: true, Evidence: "query reports the attempt committed or terminally rejected"}
	}
	return []*lernav1.CapabilityDeclaration{d}
}

// Execute 进行一次调用。只在出口闸门放行后被调用；外部键必须由核心显式传入。
func (a *Adapter) Execute(_ context.Context, req *lernav1.ExecuteRequest) (*lernav1.ExecutionReport, error) {
	if req.GetCapabilityId() != a.Capability {
		return nil, fmt.Errorf("mockapi: unknown capability %q", req.GetCapabilityId())
	}
	resp := a.Target.Put(req.GetExternalKey(), req.GetAttemptId(), req.GetArguments()["key"], req.GetArguments()["value"])
	rep := &lernav1.ExecutionReport{AdapterVersion: Version, ObservedAt: timestamppb.New(a.Target.now())}
	if resp.Lost {
		// 回执丢失：效果无法确定，用量也无法确定。
		rep.Status = lernav1.ExecutionStatus_EXECUTION_STATUS_UNKNOWN
		rep.ClaimedEffect = lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN
		rep.Diagnostic = "response lost"
		rep.Usage = []*lernav1.UsageItem{{BillingSource: a.ID, Unit: "micro_usd", Unknown: true}}
		return rep, nil
	}
	rep.TargetReceipt = resp.RequestID
	rep.ProtocolResult = fmt.Sprintf("%d", resp.Status)
	rep.Fields = map[string]string{"key": req.GetArguments()["key"]}
	if resp.Cost > 0 && !resp.Dedup {
		rep.Usage = []*lernav1.UsageItem{{BillingSource: a.ID, NativeId: resp.RequestID, Unit: "micro_usd", Amount: resp.Cost, Final: true}}
	}
	switch {
	case resp.Status == 429:
		rep.Status = lernav1.ExecutionStatus_EXECUTION_STATUS_RATE_LIMITED
		rep.ClaimedEffect = lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED
		rep.ClaimsTerminal = true
		rep.RetryAfterMs = resp.RetryAfter.Milliseconds()
		rep.RateLimit = lernav1.RateLimitKind_RATE_LIMIT_KIND_RATE
	case resp.Status == 422:
		rep.Status = lernav1.ExecutionStatus_EXECUTION_STATUS_NOT_EXECUTED
		rep.ClaimedEffect = lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED
		rep.ClaimsTerminal = true
	case resp.Committed:
		rep.Status = lernav1.ExecutionStatus_EXECUTION_STATUS_FINISHED
		rep.ClaimedEffect = lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED
		rep.ClaimsTerminal = true
		rep.Fields["value"] = req.GetArguments()["value"]
	default:
		// 202 已接受：只证明"已受理"，效果仍未知。
		rep.Status = lernav1.ExecutionStatus_EXECUTION_STATUS_ACCEPTED
		rep.ClaimedEffect = lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN
	}
	return rep, nil
}

// Query 按原尝试查询；不重新执行原动作。"查无记录"保持未知，除非目标给出终局依据。
func (a *Adapter) Query(_ context.Context, req *lernav1.QueryRequest) (*lernav1.ExecutionReport, error) {
	q := a.Target.Query(req.GetAttemptId())
	rep := &lernav1.ExecutionReport{AdapterVersion: Version, ObservedAt: timestamppb.New(a.Target.now()),
		// 查询不收费：零费用有依据（模拟目标的计费约定）。
		Usage: []*lernav1.UsageItem{{BillingSource: a.ID, NativeId: "query:" + req.GetAttemptId() + ":" + fmt.Sprint(req.GetSendSeq()),
			Unit: "micro_usd", Amount: 0, Final: true}}}
	switch {
	case !q.Supported:
		rep.Status = lernav1.ExecutionStatus_EXECUTION_STATUS_UNSUPPORTED
		rep.ClaimedEffect = lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN
	case q.Committed:
		rep.Status = lernav1.ExecutionStatus_EXECUTION_STATUS_FINISHED
		rep.ClaimedEffect = lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED
		rep.ClaimsTerminal = true
		rep.TargetReceipt = q.RequestID
		rep.Fields = map[string]string{"value": q.Value}
	case q.Found && q.Terminal:
		rep.Status = lernav1.ExecutionStatus_EXECUTION_STATUS_FINISHED
		rep.ClaimedEffect = lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED
		rep.ClaimsTerminal = true
		rep.TargetReceipt = q.RequestID
	case q.Found:
		rep.Status = lernav1.ExecutionStatus_EXECUTION_STATUS_IN_PROGRESS
		rep.ClaimedEffect = lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN
		rep.TargetReceipt = q.RequestID
	default:
		// 只查到"当前不存在"不足以证明未执行（G1）。
		rep.Status = lernav1.ExecutionStatus_EXECUTION_STATUS_NOT_FOUND
		rep.ClaimedEffect = lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN
	}
	return rep, nil
}
