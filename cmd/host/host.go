// Package host 是 M1 单进程宿主的装配代码：生产宿主（lernad、lerna）与测试 harness 共用。
//
// 只装配，不写业务规则：打开各事务域的数据库，登记核心模块，连接路由和工作者，
// 并以本地绑定向交互适配器提供公共契约命令（不实现网关）。
package host

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/budget"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/egress"
	"github.com/ruipengliu/lerna/core/grants"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/core/sessions"
	"github.com/ruipengliu/lerna/core/tasks"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

// 稳定的负责域标识：逻辑标识，不是进程地址。
const (
	DomainAdjudication = "adjudication"
	DomainLedger       = "ledger"
	// EndpointLocal 是本机执行端点。
	EndpointLocal = "local"
)

// Config 是宿主的装配配置。
type Config struct {
	// Dir 存放各事务域的数据库文件。
	Dir string
	// UserID 是 M1 唯一的用户；其他用户身份一律拒绝。
	UserID string
	// Clock 是权威时钟。
	Clock durable.Clock
	// Instance 是本进程实例的身份；进程重启后必须换新。
	Instance string
	// Reasoner 是推理实现；测试 harness 替换为脚本化推理。
	Reasoner ports.Reasoner
	// Executors 是受审查的参考执行适配器；测试 harness 替换出口目标。
	Executors []ports.Executor
}

// Host 是装配好的单进程宿主。
type Host struct {
	cfg     Config
	dbs     []*sql.DB
	router  *durable.Router
	domains []*durable.Domain
	workers []*durable.Worker

	Adjudication *durable.Domain
	Ledger       *durable.Domain
	Sessions     *sessions.Module
	Tasks        *tasks.Module
	Grants       *grants.Module
	Budget       *budget.Module
	LedgerModule *ledger.Module
	Catalog      *Catalog
}

// Open 打开数据目录并装配全部模块。
func Open(cfg Config) (*Host, error) {
	if cfg.UserID == "" || cfg.Instance == "" || cfg.Clock == nil {
		return nil, fmt.Errorf("host: UserID, Instance and Clock are required")
	}
	h := &Host{cfg: cfg, router: durable.NewRouter()}
	adj, err := h.openDomain(DomainAdjudication, "adjudication")
	if err != nil {
		return nil, err
	}
	led, err := h.openDomain(DomainLedger, "ledger")
	if err != nil {
		return nil, err
	}
	h.Adjudication = adj
	h.Ledger = led
	h.Catalog, err = NewCatalog(cfg.Executors, DomainLedger, EndpointLocal)
	if err != nil {
		_ = h.Close()
		return nil, err
	}
	h.Tasks = &tasks.Module{Domain: adj, Reasoner: cfg.Reasoner, Catalog: h.Catalog}
	h.Sessions = &sessions.Module{Domain: adj, Tasks: h.Tasks}
	h.Grants = &grants.Module{Domain: adj, LedgerDomain: DomainLedger}
	h.Budget = &budget.Module{Domain: adj}
	h.LedgerModule = &ledger.Module{
		Domain:             led,
		Endpoint:           EndpointLocal,
		AdjudicationDomain: DomainAdjudication,
		Gate:               &egress.Gate{Router: h.router, Executor: h.Catalog.Executor, Timeout: 20 * time.Second},
	}
	h.Tasks.Register()
	h.Sessions.Register()
	h.Grants.Register()
	h.Budget.Register()
	h.LedgerModule.Register()
	return h, nil
}

func (h *Host) openDomain(id, kind string) (*durable.Domain, error) {
	db, err := sqlite.Open(filepath.Join(h.cfg.Dir, id+".db"), kind, sqlite.LocalProfile)
	if err != nil {
		_ = h.Close()
		return nil, fmt.Errorf("host: open %s: %w", id, err)
	}
	h.dbs = append(h.dbs, db)
	d := durable.NewDomain(id, db, h.cfg.Clock)
	d.SetRouter(h.router)
	h.router.Register(id, d)
	h.domains = append(h.domains, d)
	h.workers = append(h.workers, durable.NewWorker(d, h.cfg.Instance+"@"+id))
	return d, nil
}

// Close 关闭全部数据库。
func (h *Host) Close() error {
	var first error
	for _, db := range h.dbs {
		if err := db.Close(); err != nil && first == nil {
			first = err
		}
	}
	h.dbs = nil
	return first
}

// Domains 返回全部事务域。
func (h *Host) Domains() []*durable.Domain { return h.domains }

// RunOnce 依次让各事务域的工作者推进一项工作；有工作被推进时返回 true。
func (h *Host) RunOnce(ctx context.Context) (bool, error) {
	any := false
	for _, w := range h.workers {
		did, err := w.RunOne(ctx)
		if err != nil {
			return any, err
		}
		any = any || did
	}
	return any, nil
}

// RunUntilIdle 推进工作直到没有可领取的工作，最多 max 轮。
func (h *Host) RunUntilIdle(ctx context.Context, max int) error {
	for i := 0; i < max; i++ {
		did, err := h.RunOnce(ctx)
		if err != nil {
			return err
		}
		if !did {
			return nil
		}
	}
	return fmt.Errorf("host: still busy after %d rounds", max)
}

// Client 返回绑定到已认证主体的公共契约入口。issuer 由认证得出，不采信信封自报的值。
func (h *Host) Client(issuer string) ports.Core {
	return &client{h: h, user: h.cfg.UserID, issuer: issuer}
}

type client struct {
	h      *Host
	user   string
	issuer string
}

func (c *client) authorize(id *lernav1.CommandIdentity) error {
	if id.GetUserId() != c.user {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "user %q is not served by this host", id.GetUserId())
	}
	if id.GetIssuerId() != c.issuer {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "issuer %q does not match the authenticated principal", id.GetIssuerId())
	}
	return nil
}

func (c *client) Submit(ctx context.Context, env *lernav1.CommandEnvelope) (*lernav1.Receipt, error) {
	if err := c.authorize(env.GetIdentity()); err != nil {
		return nil, err
	}
	return c.h.router.Deliver(ctx, env)
}

func (c *client) QueryCommand(ctx context.Context, id *lernav1.CommandIdentity) (*lernav1.ReceiptQueryResponse, error) {
	if err := c.authorize(id); err != nil {
		return nil, err
	}
	return c.h.router.Query(ctx, id)
}

func (c *client) Session(ctx context.Context, user, sessionID string) (*lernav1.SessionView, error) {
	if user != c.user {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "user %q is not served by this host", user)
	}
	return c.h.Sessions.View(ctx, user, sessionID)
}

func (c *client) Task(ctx context.Context, user, taskID string) (*lernav1.TaskView, error) {
	if user != c.user {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "user %q is not served by this host", user)
	}
	return c.h.Tasks.View(ctx, user, taskID)
}

func (c *client) Grant(ctx context.Context, user, grantID string) (*lernav1.GrantView, error) {
	if user != c.user {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "user %q is not served by this host", user)
	}
	return c.h.Grants.View(ctx, user, grantID)
}

func (c *client) Budget(ctx context.Context, user string) (*lernav1.BudgetView, error) {
	if user != c.user {
		return nil, errs.New(lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, "user %q is not served by this host", user)
	}
	return c.h.Budget.View(ctx, user)
}
