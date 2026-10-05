// Package harness 是 M1 的主测试切面：用真实核心和 SQLite 装配进程内宿主，
// 测试只通过公共契约命令驱动，断言持久记录和模拟目标实际收到的调用次数。
//
// harness 与生产宿主共用 cmd/host 的装配代码。故障构建（-tags fault）下，
// 注入的崩溃被识别为"进程崩溃"：宿主被丢弃，Restart 从同一组数据库文件以新的进程实例重启。
package harness

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
)

// User 是 M1 唯一的测试用户（合成数据）。
const User = "u-test"

// Issuer 是命令行交互适配器的调用者命名空间。
const Issuer = "cli"

// ErrCrashed 表示宿主在注入点崩溃，需要 Restart。
var ErrCrashed = errors.New("harness: host crashed at injected fault point")

// Harness 是一个可崩溃、可重启的进程内宿主。
type Harness struct {
	T     testing.TB
	Dir   string
	Clock *Clock
	Host  *host.Host
	// CrashedAt 是最近一次注入崩溃的点；为空表示宿主在运行。
	CrashedAt string
	gen       int
	options   []Option
}

// Option 调整装配配置，例如替换推理和出口目标。
type Option func(*host.Config)

// New 在临时目录中装配宿主。
func New(t testing.TB, opts ...Option) *Harness {
	t.Helper()
	h := &Harness{T: t, Dir: t.TempDir(), Clock: NewClock(), options: opts}
	h.start()
	t.Cleanup(func() {
		if h.Host != nil {
			_ = h.Host.Close()
		}
	})
	return h
}

func (h *Harness) start() {
	h.T.Helper()
	h.gen++
	cfg := host.Config{
		Dir:      h.Dir,
		UserID:   User,
		Clock:    h.Clock,
		Instance: fmt.Sprintf("proc-%d", h.gen),
	}
	for _, o := range h.options {
		o(&cfg)
	}
	hst, err := host.Open(cfg)
	if err != nil {
		h.T.Fatalf("open host: %v", err)
	}
	h.Host = hst
	h.CrashedAt = ""
}

// Restart 丢弃当前宿主（包括已崩溃的），从同一组数据库文件以新的进程实例重启。
// 进程重启后，领取的租约要过期才能被接替，所以时钟前进一个租约时长。
func (h *Harness) Restart() {
	h.T.Helper()
	if h.Host != nil {
		_ = h.Host.Close()
		h.Host = nil
	}
	h.Clock.Advance(time.Minute)
	h.start()
}

// guard 执行 fn；注入的崩溃被转换为 ErrCrashed，其他 panic 原样抛出。
func (h *Harness) guard(fn func() error) (err error) {
	if h.Host == nil {
		return ErrCrashed
	}
	defer func() {
		if v := recover(); v != nil {
			point, ok := crashPoint(v)
			if !ok {
				panic(v)
			}
			h.CrashedAt = point
			_ = h.Host.Close()
			h.Host = nil
			err = ErrCrashed
		}
	}()
	return fn()
}

// Client 返回命令行主体的公共契约入口。
func (h *Harness) Client() ports.Core { return h.Host.Client(Issuer) }

// Identity 构造命令行发给裁决域的命令身份。
func Identity(commandID string) *lernav1.CommandIdentity {
	return &lernav1.CommandIdentity{
		UserId:         User,
		IssuerId:       Issuer,
		TargetDomainId: host.DomainAdjudication,
		CommandId:      commandID,
	}
}

// Submit 以命令行主体提交命令。
func (h *Harness) Submit(commandID, kind string, payload proto.Message) (*lernav1.Receipt, error) {
	h.T.Helper()
	env, err := durable.NewEnvelope(Identity(commandID), kind, payload)
	if err != nil {
		h.T.Fatalf("envelope: %v", err)
	}
	var rec *lernav1.Receipt
	err = h.guard(func() error {
		var err error
		rec, err = h.Client().Submit(context.Background(), env)
		return err
	})
	return rec, err
}

// MustAccept 提交命令并要求它被接受，返回解码后的结果。
func (h *Harness) MustAccept(commandID, kind string, payload, result proto.Message) *lernav1.Receipt {
	h.T.Helper()
	rec, err := h.Submit(commandID, kind, payload)
	if err != nil {
		h.T.Fatalf("submit %s: %v", kind, err)
	}
	if rec.GetDecision() != lernav1.Decision_DECISION_ACCEPTED {
		h.T.Fatalf("submit %s: decision %s: %v", kind, rec.GetDecision(), errs.FromProto(rec.GetRejection()))
	}
	if result != nil {
		if err := durable.ResultOf(rec, result); err != nil {
			h.T.Fatalf("decode result: %v", err)
		}
	}
	return rec
}

// SubmitGoal 以模板提交一个新目标，返回结果。
func (h *Harness) SubmitGoal(commandID, text string, draft *lernav1.RequirementSetDraft) *lernav1.SubmitInputResult {
	h.T.Helper()
	res := &lernav1.SubmitInputResult{}
	h.MustAccept(commandID, ports.CommandSubmitInput, &lernav1.SubmitInputCommand{
		InputKind:    lernav1.InputKind_INPUT_KIND_NEW_GOAL,
		Text:         text,
		Requirements: draft,
	}, res)
	return res
}

// Query 用原命令身份查询回执。
func (h *Harness) Query(commandID string) *lernav1.ReceiptQueryResponse {
	h.T.Helper()
	q, err := h.Client().QueryCommand(context.Background(), Identity(commandID))
	if err != nil {
		h.T.Fatalf("query: %v", err)
	}
	return q
}

// Run 推进全部工作直到空闲。宿主崩溃时返回 ErrCrashed。
// 只有等待中的定时工作时，把时钟拨到最早的到期时间继续推进（最多推进 horizon）。
func (h *Harness) Run() error {
	return h.RunFor(10 * time.Minute)
}

// RunFor 与 Run 相同，但最多让时钟前进 horizon。
func (h *Harness) RunFor(horizon time.Duration) error {
	deadline := h.Clock.Now().Add(horizon)
	return h.guard(func() error {
		ctx := context.Background()
		for round := 0; round < 10000; round++ {
			did, err := h.Host.RunOnce(ctx)
			if err != nil {
				return err
			}
			if did {
				continue
			}
			next := time.Time{}
			for _, d := range h.Host.Domains() {
				due, err := d.NextDue(ctx)
				if err != nil {
					return err
				}
				if !due.IsZero() && (next.IsZero() || due.Before(next)) {
					next = due
				}
			}
			if next.IsZero() || next.After(deadline) {
				return nil
			}
			h.Clock.Set(next)
		}
		return fmt.Errorf("harness: run did not settle")
	})
}

// MustRun 推进工作并要求没有错误。
func (h *Harness) MustRun() {
	h.T.Helper()
	if err := h.Run(); err != nil {
		h.T.Fatalf("run: %v", err)
	}
}

// Task 返回任务快照。
func (h *Harness) Task(taskID string) *lernav1.TaskView {
	h.T.Helper()
	v, err := h.Client().Task(context.Background(), User, taskID)
	if err != nil {
		h.T.Fatalf("task %s: %v", taskID, err)
	}
	return v
}

// Session 返回会话快照。
func (h *Harness) Session(sessionID string) *lernav1.SessionView {
	h.T.Helper()
	v, err := h.Client().Session(context.Background(), User, sessionID)
	if err != nil {
		h.T.Fatalf("session %s: %v", sessionID, err)
	}
	return v
}

// Count 在指定事务域中执行计数查询，用于断言持久记录。
func (h *Harness) Count(domainID, query string, args ...any) int {
	h.T.Helper()
	var n int
	for _, d := range h.Host.Domains() {
		if d.ID() != domainID {
			continue
		}
		err := d.Read(context.Background(), func(tx *durable.Tx) error {
			return tx.QueryRow(query, args...).Scan(&n)
		})
		if err != nil {
			h.T.Fatalf("count %q: %v", query, err)
		}
		return n
	}
	h.T.Fatalf("no domain %s", domainID)
	return 0
}

// NewID 返回一个新的命令标识。
func NewID() string { return ids.New() }
