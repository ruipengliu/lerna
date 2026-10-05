// Package durable 实现持久工作（docs/architecture/core/durable/README.md）：
// 命令回执、事务参与、待办工作（领取、租约、代次、修订号）和跨域交接（R7）。
//
// 每个事务域是一个 Domain，绑定一个数据库、一个稳定的负责域标识和权威时钟。
// 处理函数只在事务内修改核心状态；外部调用必须在事务提交之后由对应的核心路径发起。
package durable

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable/fault"
)

// Clock 是权威时钟（内部接口）：租约和到期都按它判断，工作者的本地计时只用于提前停手。
type Clock interface {
	Now() time.Time
}

// Domain 是一个事务域的持久工作设施。
type Domain struct {
	id    string
	db    *sql.DB
	clock Clock

	mu       sync.RWMutex
	commands map[string]commandEntry
	jobs     map[string]JobHandler
	hooks    map[string]ReceiptHook
	router   *Router

	// LeaseDuration 是领取的租约时长；租约参数不承担正确性。
	LeaseDuration time.Duration
}

// NewDomain 创建事务域。db 必须已按持久档位配置并完成迁移。
func NewDomain(id string, db *sql.DB, clock Clock) *Domain {
	d := &Domain{
		id:            id,
		db:            db,
		clock:         clock,
		commands:      map[string]commandEntry{},
		jobs:          map[string]JobHandler{},
		hooks:         map[string]ReceiptHook{},
		LeaseDuration: 30 * time.Second,
	}
	d.HandleJob(jobKindDeliver, d.deliver)
	return d
}

// ID 返回负责域标识：稳定的逻辑标识，不是进程地址。
func (d *Domain) ID() string { return d.id }

// Now 返回权威时间。
func (d *Domain) Now() time.Time { return d.clock.Now() }

// SetRouter 设置跨域交接使用的路由。
func (d *Domain) SetRouter(r *Router) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.router = r
}

// Tx 是一个事务域内的事务上下文（内部接口）。不得跨域使用。
type Tx struct {
	d   *Domain
	sql *sql.Tx
	now time.Time
}

// Exec 在事务中执行语句。
func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.sql.Exec(query, args...)
}

// Query 在事务中查询。
func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.sql.Query(query, args...)
}

// QueryRow 在事务中查询一行。
func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.sql.QueryRow(query, args...)
}

// Now 返回事务开始时取得的权威时间。
func (t *Tx) Now() time.Time { return t.now }

// NowMs 返回事务时间的毫秒值，用于存储。
func (t *Tx) NowMs() int64 { return t.now.UnixMilli() }

// DomainID 返回事务所属的负责域。
func (t *Tx) DomainID() string { return t.d.id }

// Write 在一个写事务中执行 fn；fn 返回 nil 才提交。
//
// label 命名这个持久化点：故障注入在 "<label>:before_commit" 和
// "<label>:after_commit" 两处生效。提交结果无法确定时返回 INDETERMINATE 错误，
// 调用方必须查询或用原标识重入，不得宣称失败。
func (d *Domain) Write(ctx context.Context, label string, fn func(*Tx) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_DEPENDENCY_UNAVAILABLE, "%s: begin: %v", d.id, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	t := &Tx{d: d, sql: tx, now: d.clock.Now()}
	if err := fn(t); err != nil {
		return err
	}
	if err := fault.Hit(label + ":before_commit"); err != nil {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_TRANSPORT_LOST, "%s: %s: commit request lost", d.id, label)
	}
	if err := tx.Commit(); err != nil {
		committed = true
		return errs.New(lernav1.ErrorCode_ERROR_CODE_TRANSPORT_LOST, "%s: %s: commit result unknown: %v", d.id, label, err)
	}
	committed = true
	if err := fault.Hit(label + ":after_commit"); err != nil {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_TRANSPORT_LOST, "%s: %s: receipt lost after commit", d.id, label)
	}
	return nil
}

// Read 在一个只读事务中执行 fn。
func (d *Domain) Read(ctx context.Context, fn func(*Tx) error) error {
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_DEPENDENCY_UNAVAILABLE, "%s: begin read: %v", d.id, err)
	}
	defer func() { _ = tx.Rollback() }()
	return fn(&Tx{d: d, sql: tx, now: d.clock.Now()})
}
