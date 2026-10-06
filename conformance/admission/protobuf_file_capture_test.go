package admission_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/content"
	"github.com/ruipengliu/lerna/core/egress"
	"github.com/ruipengliu/lerna/infra/egressio"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// recordingFileContent 委托真实内容治理，仅保留实际接纳的公共命令。
type recordingFileContent struct {
	*content.Service
	t       *testing.T
	samples *[]protobuf.Sample
	action  string
}

func (r *recordingFileContent) BindFileRoot(ctx context.Context, caller *v1.Caller, c *v1.BindFileRootCommand) (*v1.CommandReceipt, error) {
	receipt, err := r.Service.BindFileRoot(ctx, caller, c)
	accepted(r.t, receipt, err)
	captureObject(r.t, r.samples, "file-bind-root-command-"+r.action, c, nil)
	return receipt, err
}
func (r *recordingFileContent) RegisterFileResources(ctx context.Context, caller *v1.Caller, c *v1.RegisterFileResourcesCommand) (*v1.CommandReceipt, error) {
	receipt, err := r.Service.RegisterFileResources(ctx, caller, c)
	accepted(r.t, receipt, err)
	captureObject(r.t, r.samples, "file-register-resources-command-"+r.action, c, nil)
	return receipt, err
}

// recordingFileIO 保留原生执行的最终资格检查，不替换结果或屏障。
type recordingFileIO struct {
	*egressio.Files
	t       *testing.T
	samples *[]protobuf.Sample
	calls   int
}

func (r *recordingFileIO) PerformChecked(ctx context.Context, request *v1.PhysicalIORequest, check func(context.Context) error) (*v1.PhysicalIOResult, error) {
	r.calls++
	action := request.CallDescriptor.Method
	captureObject(r.t, r.samples, "file-io-request-"+action, request, nil)
	result, err := r.Files.PerformChecked(ctx, request, check)
	captureObject(r.t, r.samples, "file-io-result-"+action, result, err)
	return result, err
}

// 规则：G1、G3、G5、G9、G10、G12
func captureManagedFiles(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"objects", "commits", "locks"} {
		if err = os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	roots := map[string]string{"documents": root}
	f := newFixtureWithTargetAndFiles(t, 100, 80, false, nil, roots)
	observer := &recordingFileContent{Service: f.h.Content, t: t, samples: samples}
	io := &recordingFileIO{Files: egressio.NewFiles(roots, f.h.Ledger, observer), t: t, samples: samples}
	lock, err := egressio.NewFileLock(f.path)
	if err != nil {
		t.Fatal(err)
	}
	gateway := egress.New(f.h.Tasks, f.h.Ledger, observer, io, lock)
	var original *v1.RawObservation
	var previous string
	for index, action := range []string{"CREATE", "REPLACE", "READ"} {
		f.suffix = "-file-" + action
		observer.action = action
		body := []byte("file version A")
		if action == "REPLACE" {
			body = []byte("file version B")
		}
		if action == "READ" {
			body = nil
		}
		fileParameters(t, f, previous, body)
		configureFile(t, f, action, "managed://documents/report")
		a, start := prepareStart(t, f)
		captureObject(t, samples, "file-start-"+action, start, nil)
		receipt, err := gateway.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
		accepted(t, receipt, err)
		op, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
		captureObject(t, samples, "file-operation-"+action, op, err)
		if op.Effect.Outcome != "APPLIED" || op.Lifecycle != "SETTLED" {
			t.Fatalf("file %s not settled: %v", action, op)
		}
		observation, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, op.Execution.Send.ObservationRef)
		captureObject(t, samples, "file-observation-"+action, observation, err)
		observedBody, err := f.h.Content.Read(f.ctx, f.caller, observation.BodyRef)
		captureObject(t, samples, "file-body-"+action, observedBody, err)
		want := "file version A"
		if index > 0 {
			want = "file version B"
		}
		if string(observedBody.RawBody) != want {
			t.Fatalf("unexpected readback %q", observedBody.RawBody)
		}
		billing, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, op.Execution.Send.Ref)
		captureObject(t, samples, "file-billing-"+action, billing, err)
		if billing.Status != "SETTLED" {
			t.Fatal(billing)
		}
		pointerBytes, err := os.ReadFile(filepath.Join(root, "commits", "report"))
		if err != nil {
			t.Fatal(err)
		}
		pointer := new(v1.FileCommit)
		if err = protojson.Unmarshal(pointerBytes, pointer); err != nil {
			t.Fatal(err)
		}
		nativeBody, err := os.ReadFile(filepath.Join(root, "objects", pointer.ObjectName))
		if err != nil || string(nativeBody) != want {
			t.Fatalf("native body %q: %v", nativeBody, err)
		}
		if action != "READ" {
			if !proto.Equal(pointer, observation.FileEvidence.Commit) || !observation.FileEvidence.DurabilityConfirmed {
				t.Fatal("native pointer differs from accepted durable evidence")
			}
			resources, err := f.h.Content.QueryFileResourcesForSend(f.ctx, f.caller, op.Execution.Send.Ref)
			captureObject(t, samples, "file-resources-"+action, resources, err)
			if resources.ObjectIdentity == "" || resources.PointerIdentity == "" {
				t.Fatal("missing native resource identities")
			}
			if leaked, err := f.h.Content.QueryFileResources(f.ctx, &v1.Caller{UserId: "other", IssuerId: "host"}, resources.Ref); err == nil || leaked != nil {
				t.Fatal("cross-user file resources exposed")
			}
		}
		if action == "CREATE" {
			original = observation
		}
		previous = pointer.Version
		replay, err := gateway.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
		accepted(t, replay, err)
		after, err := os.ReadFile(filepath.Join(root, "commits", "report"))
		if err != nil || string(after) != string(pointerBytes) || io.calls != index+1 {
			t.Fatal("replay changed native target or invoked performer")
		}
	}
	managed, err := f.h.Content.QueryManagedFileRoot(f.ctx, f.caller, "documents")
	captureObject(t, samples, "file-root", managed, err)
	if leaked, err := f.h.Content.QueryManagedFileRoot(f.ctx, &v1.Caller{UserId: "other", IssuerId: "host"}, "documents"); err == nil || leaked != nil {
		t.Fatal("cross-user managed root exposed")
	}
	historical, err := f.h.Ledger.QueryFilePublication(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress-io"}, original.FileEvidence.Commit)
	captureObject(t, samples, "file-history-original", historical, err)
	if !proto.Equal(historical, original) || historical.FileEvidence.Commit.Version == previous {
		t.Fatal("replacement changed original publication history")
	}
	objects, err := os.ReadDir(filepath.Join(root, "objects"))
	if err != nil || len(objects) != 2 || io.calls != 3 || f.calls.Load() != 0 {
		t.Fatalf("native publications=%d calls=%d err=%v", len(objects), io.calls, err)
	}
}
