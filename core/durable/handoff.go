package durable

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable/fault"
)

const jobKindDeliver = "durable.deliver"

// HandoffState 是跨域交接的投递状态（持久工作 2.3）。
type HandoffState string

const (
	HandoffPending  HandoffState = "PENDING"
	HandoffAccepted HandoffState = "ACCEPTED"
	HandoffRejected HandoffState = "REJECTED"
	HandoffBlocked  HandoffState = "BLOCKED"
)

// Handoff 是源方在自己的事务里记下的交接意图（R7 第一步）。
type Handoff struct {
	// ID 由源事务生成并固定，也是对方的命令标识。
	ID   string
	User string
	// Target 是固定的接收事务域；路由重试不得改派。
	Target string
	Kind   string
	// Payload 是不可变的命令正文。
	Payload proto.Message
	// IntentRef 追溯到原责任，例如动作标识。
	IntentRef string
}

// HandoffRecord 是已保存的交接记录。
type HandoffRecord struct {
	ID        string
	User      string
	Source    string
	Target    string
	Kind      string
	IntentRef string
	State     HandoffState
	Envelope  *lernav1.CommandEnvelope
	Receipt   *lernav1.Receipt
}

// ReceiptHook 在源方保存对方回执的同一事务中执行（R7 第三步），由源方模块登记。
type ReceiptHook func(ctx context.Context, tx *Tx, h *HandoffRecord, r *lernav1.Receipt) error

// OnHandoffReceipt 登记某类交接的回执处理。
func (d *Domain) OnHandoffReceipt(kind string, h ReceiptHook) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hooks[kind] = h
}

// EnqueueHandoff 在源方事务中记下交接和投递工作（R7 第一步）。同一交接标识只记一次。
func (t *Tx) EnqueueHandoff(h Handoff) error {
	id := &lernav1.CommandIdentity{
		UserId: h.User,
		// 源域以自己的固定服务命名空间创建目标命令。
		IssuerId:       t.d.id,
		TargetDomainId: h.Target,
		CommandId:      h.ID,
	}
	env, err := NewEnvelope(id, h.Kind, h.Payload)
	if err != nil {
		return err
	}
	blob, err := proto.MarshalOptions{Deterministic: true}.Marshal(env)
	if err != nil {
		return err
	}
	res, err := t.Exec(`INSERT INTO handoffs (handoff_id, user_id, source_domain_id, target_domain_id, command_kind,
		intent_ref, envelope, fingerprint, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (handoff_id) DO NOTHING`,
		h.ID, h.User, t.d.id, h.Target, h.Kind, h.IntentRef, blob, env.GetFingerprint(), string(HandoffPending), t.NowMs(), t.NowMs())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 已有同一交接：内容必须一致，不得新建。
		var fp []byte
		if err := t.QueryRow(`SELECT fingerprint FROM handoffs WHERE handoff_id = ?`, h.ID).Scan(&fp); err != nil {
			return err
		}
		if string(fp) != string(env.GetFingerprint()) {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, "handoff %s already recorded with different content", h.ID)
		}
		return nil
	}
	_, err = t.EnqueueJob(JobSpec{Kind: jobKindDeliver, User: h.User, Subject: h.ID, PurposeKey: "deliver:" + h.ID})
	return err
}

// LoadHandoff 读取交接记录；不存在时返回 nil。
func (t *Tx) LoadHandoff(id string) (*HandoffRecord, error) {
	var h HandoffRecord
	var state string
	var envBlob, recBlob []byte
	err := t.QueryRow(`SELECT handoff_id, user_id, source_domain_id, target_domain_id, command_kind, intent_ref,
		state, envelope, receipt FROM handoffs WHERE handoff_id = ?`, id).
		Scan(&h.ID, &h.User, &h.Source, &h.Target, &h.Kind, &h.IntentRef, &state, &envBlob, &recBlob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	h.State = HandoffState(state)
	h.Envelope = &lernav1.CommandEnvelope{}
	if err := proto.Unmarshal(envBlob, h.Envelope); err != nil {
		return nil, err
	}
	if len(recBlob) > 0 {
		h.Receipt = &lernav1.Receipt{}
		if err := proto.Unmarshal(recBlob, h.Receipt); err != nil {
			return nil, err
		}
	}
	return &h, nil
}

// Endpoint 是一个事务域对外提供的命令入口。核心模块之间的交接是可信基础设施 I/O：
// 只使用固定身份和固定目标，不经过出口闸门。
type Endpoint interface {
	Execute(ctx context.Context, env *lernav1.CommandEnvelope) (*lernav1.Receipt, error)
	QueryReceipt(ctx context.Context, id *lernav1.CommandIdentity) (*lernav1.ReceiptQueryResponse, error)
}

// Router 把命令送到固定的接收事务域。同进程部署也经过它，并可注入域间回执丢失。
type Router struct {
	endpoints map[string]Endpoint
}

// NewRouter 创建路由。
func NewRouter() *Router { return &Router{endpoints: map[string]Endpoint{}} }

// Register 登记事务域的入口。
func (r *Router) Register(domainID string, ep Endpoint) { r.endpoints[domainID] = ep }

// Deliver 把命令交给接收域。接收域已经提交、但回执在途中丢失时返回 TRANSPORT_LOST：
// 网络结果不代替接纳事实，源方必须查询原交接。
func (r *Router) Deliver(ctx context.Context, env *lernav1.CommandEnvelope) (*lernav1.Receipt, error) {
	ep, ok := r.endpoints[env.GetIdentity().GetTargetDomainId()]
	if !ok {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_DEPENDENCY_UNAVAILABLE, "no route to %s", env.GetIdentity().GetTargetDomainId())
	}
	rec, err := ep.Execute(ctx, env)
	if err != nil {
		return nil, err
	}
	if ferr := fault.Hit("deliver:" + env.GetCommandKind() + ":receipt"); ferr != nil {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_TRANSPORT_LOST, "receipt for %s lost in transit", env.GetIdentity().GetCommandId())
	}
	return rec, nil
}

// Query 向接收域查询原交接的回执。
func (r *Router) Query(ctx context.Context, id *lernav1.CommandIdentity) (*lernav1.ReceiptQueryResponse, error) {
	ep, ok := r.endpoints[id.GetTargetDomainId()]
	if !ok {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_DEPENDENCY_UNAVAILABLE, "no route to %s", id.GetTargetDomainId())
	}
	return ep.QueryReceipt(ctx, id)
}

// deliver 是投递工作的处理函数（持久工作 4.3）：先查询原交接，查不到明确结果才用
// 原标识、原内容投递；回执丢失时下次再查询，不新建交接。
func (d *Domain) deliver(ctx context.Context, c *Claim) error {
	var h *HandoffRecord
	if err := d.Read(ctx, func(tx *Tx) error {
		var err error
		h, err = tx.LoadHandoff(c.Subject)
		return err
	}); err != nil {
		return err
	}
	if h == nil {
		return d.Advance(ctx, c, "durable:deliver_missing", func(*Tx) (Transition, error) {
			return Block("handoff record missing"), nil
		})
	}
	if h.State != HandoffPending {
		return d.Advance(ctx, c, "durable:deliver_done", func(*Tx) (Transition, error) { return Done(), nil })
	}
	d.mu.RLock()
	router := d.router
	d.mu.RUnlock()
	retry := func(reason string) error {
		wait := Backoff(c.JobID, c.ClaimCount-1, 50*time.Millisecond, 30*time.Second, time.Millisecond)
		return d.Advance(ctx, c, "durable:deliver_retry", func(tx *Tx) (Transition, error) {
			return WaitUntil(tx.Now().Add(wait), reason), nil
		})
	}
	if router == nil {
		return retry("no router")
	}
	var rec *lernav1.Receipt
	q, err := router.Query(ctx, h.Envelope.GetIdentity())
	if err != nil {
		return retry(err.Error())
	}
	switch q.GetOutcome() {
	case lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_DECIDED:
		rec = q.GetReceipt()
	case lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_NOT_FOUND:
		rec, err = router.Deliver(ctx, h.Envelope)
		if err != nil {
			if errs.IsPermanent(err) {
				// 对方无法接纳原内容（例如内容冲突）：责任保留，停下报告，不改派。
				return d.Advance(ctx, c, "durable:deliver_blocked", func(tx *Tx) (Transition, error) {
					if _, err := tx.Exec(`UPDATE handoffs SET state = ?, updated_at = ? WHERE handoff_id = ?`,
						string(HandoffBlocked), tx.NowMs(), h.ID); err != nil {
						return Transition{}, err
					}
					return Block(err.Error()), nil
				})
			}
			return retry(err.Error())
		}
	default:
		// 已受理待决定、无法查询：保持等待，下次再查。
		return retry("receipt " + q.GetOutcome().String())
	}
	label := "handoff:" + h.Kind + ":record_receipt"
	return d.Advance(ctx, c, label, func(tx *Tx) (Transition, error) {
		state := HandoffAccepted
		if rec.GetDecision() == lernav1.Decision_DECISION_REJECTED {
			state = HandoffRejected
		}
		blob, err := proto.MarshalOptions{Deterministic: true}.Marshal(rec)
		if err != nil {
			return Transition{}, err
		}
		if _, err := tx.Exec(`UPDATE handoffs SET state = ?, receipt = ?, updated_at = ? WHERE handoff_id = ?`,
			string(state), blob, tx.NowMs(), h.ID); err != nil {
			return Transition{}, err
		}
		h.State = state
		h.Receipt = rec
		d.mu.RLock()
		hook := d.hooks[h.Kind]
		d.mu.RUnlock()
		if hook != nil {
			if err := hook(ctx, tx, h, rec); err != nil {
				return Transition{}, err
			}
		}
		return Done(), nil
	})
}
