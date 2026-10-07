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
	"github.com/ruipengliu/lerna/infra/hosting"
	"github.com/ruipengliu/lerna/infra/keys"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

type Harness struct {
	Recovery    *hosting.Service
	Bodies      *sqlite.BodyReceipts
	Egress      *egress.Service
	Trace       *trace.Service
	Ledger      *ledger.Service
	LedgerWork  *durable.Service
	Grants      *grants.Service
	Budget      *budget.Service
	Sessions    *sessions.Service
	Tasks       *tasks.Service
	Durable     *durable.Service
	Content     *content.Service
	contentWork *durable.Service
	traceWork   *durable.Service
	store       *sqlite.Store
	user        string
	domain      string
	path        string
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
	d, err := durable.New(s, user, domain)
	if err != nil {
		s.Close()
		return nil, err
	}
	t, err := tasks.New(s, user, domain)
	if err != nil {
		s.Close()
		return nil, err
	}
	contentWork, err := durable.New(s.ContentWork(), user, domain+"/content")
	if err != nil {
		s.Close()
		return nil, err
	}
	c, err := content.New(s, contentWork, user, domain+"/content")
	if err != nil {
		s.Close()
		return nil, err
	}
	sessionService, err := sessions.New(s, d, t, c, user, domain)
	if err != nil {
		s.Close()
		return nil, err
	}
	h := &Harness{Bodies: s.BodyReceipts(), Sessions: sessionService, Tasks: t, Durable: d, Content: c, contentWork: contentWork, store: s, user: user, domain: domain, path: path}
	h.Grants, err = grants.New(s, d, user, domain, "host")
	if err != nil {
		s.Close()
		return nil, err
	}
	h.Budget, err = budget.New(s, d, user, domain, "host")
	if err != nil {
		s.Close()
		return nil, err
	}
	h.LedgerWork, err = durable.New(s.LedgerWork(), user, domain+"/ledger")
	if err != nil {
		s.Close()
		return nil, err
	}
	h.Ledger, err = ledger.New(s, user, domain+"/ledger", domain)
	if err != nil {
		s.Close()
		return nil, err
	}
	traceWork, err := durable.New(s.TraceWork(), user, domain+"/trace")
	if err != nil {
		s.Close()
		return nil, err
	}
	h.traceWork = traceWork
	h.Trace, err = trace.New(s, traceWork, h.Ledger, user, domain+"/trace")
	if err != nil {
		s.Close()
		return nil, err
	}
	critical, e := egressio.NewFileLock(path)
	if e != nil {
		s.Close()
		return nil, e
	}
	h.Egress, err = egress.New(t, h.Ledger, c, physicalIO{files: egressio.NewFiles(options.FileRoots, h.Ledger, c), network: egressio.Router{API: apiIO}}, critical)
	if err != nil {
		s.Close()
		return nil, err
	}
	h.connect()
	if err := h.connectRecovery(); err != nil {
		s.Close()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	if err := h.start(ctx); err != nil {
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

// 固定生产装配按消费模块的完整接口编译验证。
var _ ledger.Store = (*sqlite.Store)(nil)
var _ ledger.ExecutionWork = (*durable.Service)(nil)
var _ ledger.Compiler = executionCompiler{}

var _ egress.Starts = (*tasks.Service)(nil)
var _ egress.Ledger = (*ledger.Service)(nil)
var _ egress.Content = (*content.Service)(nil)
var _ egress.IO = physicalIO{}
var _ egress.PreflightIO = physicalIO{}
var _ egress.CheckedIO = physicalIO{}
var _ durable.Store = (*sqlite.LedgerWork)(nil)

// 预算的生产依赖必须满足预算声明的完整消费方接口。
var (
	_ budget.Store                 = (*sqlite.Store)(nil)
	_ budget.Decisions             = (*durable.Service)(nil)
	_ budget.UsageSource           = (*ledger.Service)(nil)
	_ budget.BillingEvidence       = (*content.Service)(nil)
	_ budget.CompletionAuthority   = (*tasks.Service)(nil)
	_ budget.CancellationAuthority = (*tasks.Service)(nil)
	_ budget.TaskClosingAuthority  = (*tasks.Service)(nil)
)

// Tasks 的工作责任只依赖声明端口，保持固定 Job 类型与负责方完成方法。
var (
	_ tasks.Store            = (*sqlite.Store)(nil)
	_ tasks.Decisions        = (*durable.Service)(nil)
	_ tasks.Scheduling       = (*durable.Service)(nil)
	_ tasks.HandoffJobs      = (*durable.Service)(nil)
	_ tasks.ModelWork        = (*durable.Service)(nil)
	_ tasks.CompletionJobs   = (*durable.Service)(nil)
	_ tasks.CancellationJobs = (*durable.Service)(nil)
	_ tasks.TaskClosingJobs  = (*durable.Service)(nil)
	_ durable.Store          = (*sqlite.Store)(nil)
	_ durable.Store          = (*sqlite.ContentWork)(nil)
	_ durable.Store          = (*sqlite.TraceWork)(nil)
)
