// Package assembly 供命令行宿主与一致性测试共用，只连接固定模块。
package assembly

import (
	"context"
	"crypto/x509"
	"path/filepath"
	"time"

	"github.com/ruipengliu/lerna/adapters/api"
	fileadapter "github.com/ruipengliu/lerna/adapters/file"
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
	"github.com/ruipengliu/lerna/infra/keys"
	"github.com/ruipengliu/lerna/infra/rules"
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
	user       string
	domain     string
	path       string
}

func Open(path, user, domain string) (*Harness, error) {
	return OpenWithOptions(path, user, domain, Options{})
}

// Options 固定受信宿主的文件根、平台凭据及证书配置。
type Options struct {
	FileRoots       map[string]string
	APIKeychainPath string
	APIRoots        *x509.CertPool
}

// OpenWithFiles 固定宿主配置的受管理根；配置本身不访问外部文件。
func OpenWithFiles(path, user, domain string, roots map[string]string) (*Harness, error) {
	return OpenWithOptions(path, user, domain, Options{FileRoots: roots})
}

func OpenWithOptions(path, user, domain string, options Options) (*Harness, error) {
	apiIO := egressio.APIHTTP{Roots: options.APIRoots}
	if options.APIKeychainPath != "" {
		secure, e := keys.OpenFileBased(options.APIKeychainPath)
		if e != nil {
			return nil, e
		}
		apiIO.Credentials = secure
	}
	s, err := sqlite.Open(path, user, domain)
	if err != nil {
		return nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		s.Close()
		return nil, err
	}
	d := durable.New(s, user, domain)
	t := tasks.New(s, user, domain).WithDecisions(d).WithCancellationJobs(d)
	c := content.New(s, user, domain+"/content").WithAssociations(t)
	h := &Harness{Bodies: s.BodyReceipts(), Sessions: sessions.New(s, d, t, c, user, domain), Tasks: t, Durable: d, Content: c, store: s, user: user, domain: domain, path: path}
	h.Grants = grants.New(s, d, user, domain, "host").WithAdmissions(t)
	h.Grants.WithConfirmations(h.Sessions).WithConfirmationContent(c)
	h.Sessions.WithConfirmations(s, d, t, h.Grants)
	t.WithConfirmationRequests(h.Sessions, c).WithModelContent(c).WithReasonerQuestions(h.Sessions).WithConditionConfirmations(h.Sessions)
	h.Budget = budget.New(s, d, user, domain, "host")
	h.LedgerWork = durable.New(s.LedgerWork(), user, domain+"/ledger")
	h.Ledger = ledger.New(s, user, domain+"/ledger", domain).WithWork(h.LedgerWork).WithCompiler(executionCompiler{api: api.Adapter{Content: c}}).WithStarts(t).WithEvidenceRules(rules.API{})
	c.WithObservations(durable.New(s.ContentWork(), user, domain+"/content"), h.Ledger)
	h.Ledger.WithObservations(c)
	h.Budget.WithUsageSource(h.Ledger).WithBillingEvidence(c).WithCompletionAuthority(t).WithCancellationAuthority(t).WithTaskClosingAuthority(t)
	h.Trace = trace.New(s, durable.New(s.TraceWork(), user, domain+"/trace"), h.Ledger, user, domain+"/trace")
	h.Ledger.WithReports(h.Budget, d, h.Trace)
	t.WithStart(h.Grants, h.Budget, h.Ledger)
	t.WithCompletion(h.Ledger, h.Budget)
	critical, e := egressio.NewFileLock(path)
	if e != nil {
		s.Close()
		return nil, e
	}
	h.Egress = egress.New(t, h.Ledger, c, physicalIO{files: egressio.NewFiles(options.FileRoots, h.Ledger, c), network: egressio.Router{API: apiIO}}, critical)
	t.WithModelExecution(h.Ledger, h.LedgerWork, h.Grants, h.Egress)
	t.WithReasonerDriver(h.Durable, defaultReasoner)
	h.Ledger.WithGrantClosures(h.Grants)
	h.Grants.WithRevocationExits(h.Egress)
	h.Ledger.WithCompletionClosures(t)
	t.WithCompletionClosures(d, h.Egress)
	h.Ledger.WithCancellationClosures(t)
	t.WithCancellationClosures(d, h.Egress)
	h.Ledger.WithTaskClosures(t)
	t.WithTaskClosures(d, h.Egress).WithTaskClosingFacts(h.Ledger, h.Budget)
	h.Ledger.WithReconciliation(t, h.Grants, h.Egress)
	t.WithClosureSource(h.Ledger).WithOperationProgress(h.Ledger)
	h.Ledger.WithOperationProgress(t)
	t.WithAdmission(h.Grants, h.Budget, c, h.Sessions, d, h.Ledger).WithHandoffs(d, h.Ledger)
	// 固定受信宿主身份仅驱动已保存的责任，不替换原命令身份。
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	if err := t.CheckStartupCompatibility(ctx); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Ledger.CheckStartupCompatibility(ctx); err != nil {
		s.Close()
		return nil, err
	}
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
	if err := h.Tasks.RecoverCancellations(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
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
	if err := h.Tasks.RecoverTaskClosures(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Tasks.ProcessTaskClosings(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Budget.ProcessClosures(ctx); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Ledger.ProcessExecutionFollowups(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Budget.ProcessSettlementFollowups(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Tasks.RecoverReasonerDrivers(ctx, &v1.Caller{UserId: user, IssuerId: "host"}); err != nil {
		s.Close()
		return nil, err
	}
	if err := h.Trace.Recover(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	return h, nil
}
func (h *Harness) Close() error                     { return h.store.Close() }
func (h *Harness) StorageSettings() sqlite.Settings { return h.store.Settings() }

// executionCompiler 只分派已经固定的能力版本。
type executionCompiler struct{ api api.Adapter }

// CheckRecoverySupported 依原适配器分派；不调用 Compile 或替换原能力。
func (executionCompiler) CheckRecoverySupported(op *v1.Operation) error {
	if op.GetCapabilitySnapshot().GetAdapterRef().GetName().GetLocalId() == "api-reference-v1" {
		return (api.Adapter{}).CheckRecoverySupported(op)
	}
	if op.GetCapabilitySnapshot().GetAdapterRef().GetName().GetLocalId() == "managed-file" {
		return (fileadapter.Adapter{}).CheckRecoverySupported(op)
	}
	return (simulator.Adapter{}).CheckRecoverySupported(op)
}

func (c executionCompiler) Compile(o *v1.Operation, a *v1.ExecutionAttempt) (*v1.CallDescriptor, *v1.ExecutionCapabilities, error) {
	return c.CompileContext(context.Background(), o, a)
}
func (c executionCompiler) CompileContext(ctx context.Context, o *v1.Operation, a *v1.ExecutionAttempt) (*v1.CallDescriptor, *v1.ExecutionCapabilities, error) {
	if o.GetCapabilitySnapshot().GetAdapterRef().GetName().GetLocalId() == "api-reference-v1" {
		return c.api.CompileContext(ctx, o, a)
	}
	if o.GetCapabilitySnapshot().GetAdapterRef().GetName().GetLocalId() == "managed-file" {
		return (fileadapter.Adapter{}).Compile(o, a)
	}
	return (simulator.Adapter{}).Compile(o, a)
}

type physicalIO struct {
	files   *egressio.Files
	network egressio.Router
}

func (p physicalIO) Preflight(ctx context.Context, d *v1.CallDescriptor) error {
	return p.network.Preflight(ctx, d)
}
func (p physicalIO) Perform(ctx context.Context, r *v1.PhysicalIORequest) (*v1.PhysicalIOResult, error) {
	return p.network.Perform(ctx, r)
}
func (p physicalIO) PerformChecked(ctx context.Context, r *v1.PhysicalIORequest, check func(context.Context) error) (*v1.PhysicalIOResult, error) {
	return p.files.PerformChecked(ctx, r, check)
}
