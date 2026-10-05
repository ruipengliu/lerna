// Package assembly 供命令行宿主与一致性测试共用，只连接固定模块。
package assembly

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/adapters/simulator"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/budget"
	"github.com/ruipengliu/lerna/core/content"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/egress"
	"github.com/ruipengliu/lerna/core/grants"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/core/sessions"
	"github.com/ruipengliu/lerna/core/tasks"
	"github.com/ruipengliu/lerna/core/trace"
	"github.com/ruipengliu/lerna/infra/egressio"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

type Harness struct {
	Bodies     *sqlite.BodyReceipts
	Egress     *egress.Service
	Trace      *trace.Service
	Ledger     *ledger.Service
	LedgerWork *durable.Service
	Grants     *grants.Service
	Budget     *budget.Service
	Sessions   *sessions.Service
	Tasks      *tasks.Service
	Durable    *durable.Service
	Content    *content.Service
	store      *sqlite.Store
}

func Open(path, user, domain string) (*Harness, error) {
	s, err := sqlite.Open(path, user, domain)
	if err != nil {
		return nil, err
	}
	d := durable.New(s, user, domain)
	t := tasks.New(s, user, domain).WithDecisions(d)
	c := content.New(s, user, domain+"/content").WithAssociations(t)
	h := &Harness{Bodies: s.BodyReceipts(), Sessions: sessions.New(s, d, t, c, user, domain), Tasks: t, Durable: d, Content: c, store: s}
	h.Grants = grants.New(s, d, user, domain, "host").WithAdmissions(t)
	h.Grants.WithConfirmations(h.Sessions).WithConfirmationContent(c)
	h.Sessions.WithConfirmations(s, d, t, h.Grants)
	t.WithConfirmationRequests(h.Sessions, c).WithModelContent(c)
	h.Budget = budget.New(s, d, user, domain, "host")
	h.LedgerWork = durable.New(s.LedgerWork(), user, domain+"/ledger")
	h.Ledger = ledger.New(s, user, domain+"/ledger", domain).WithWork(h.LedgerWork).WithCompiler(simulator.Adapter{}).WithStarts(t)
	c.WithObservations(durable.New(s.ContentWork(), user, domain+"/content"), h.Ledger)
	h.Ledger.WithObservations(c)
	h.Budget.WithUsageSource(h.Ledger).WithBillingEvidence(c).WithCompletionAuthority(t)
	h.Trace = trace.New(s, durable.New(s.TraceWork(), user, domain+"/trace"), h.Ledger, user, domain+"/trace")
	h.Ledger.WithReports(h.Budget, d, h.Trace)
	t.WithStart(h.Grants, h.Budget, h.Ledger)
	t.WithCompletion(h.Ledger, h.Budget)
	critical, e := egressio.NewFileLock(path)
	if e != nil {
		s.Close()
		return nil, e
	}
	h.Egress = egress.New(t, h.Ledger, c, egressio.HTTP{}, critical)
	t.WithModelExecution(h.Ledger, h.LedgerWork, h.Grants, h.Egress)
	h.Ledger.WithGrantClosures(h.Grants)
	h.Grants.WithRevocationExits(h.Egress)
	h.Ledger.WithCompletionClosures(t)
	t.WithCompletionClosures(d, h.Egress)
	h.Ledger.WithReconciliation(t, h.Grants, h.Egress)
	t.WithClosureSource(h.Ledger).WithOperationProgress(h.Ledger)
	h.Ledger.WithOperationProgress(t)
	t.WithAdmission(h.Grants, h.Budget, c, h.Sessions, d, h.Ledger).WithHandoffs(d, h.Ledger)
	// 固定受信宿主身份仅驱动已保存的责任，不替换原命令身份。
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	if err := h.Sessions.RecoverPending(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Tasks.RecoverHandoffs(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := c.ProcessRegistrations(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := c.ProcessObservations(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Ledger.ProcessReports(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Ledger.ProcessInterpretations(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Grants.ProcessRevocations(ctx); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Tasks.RecoverCompletions(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Ledger.ProcessOperationProgress(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Ledger.RecoverReconciliations(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Ledger.ProcessOperationProgress(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Budget.ProcessClosures(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return h, nil
}
func (h *Harness) Close() error                     { return h.store.Close() }
func (h *Harness) StorageSettings() sqlite.Settings { return h.store.Settings() }
