package admission_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/egress"
	"github.com/ruipengliu/lerna/infra/egressio"
)

// recordingIO 只观察真实受信边界，响应仍由生产 HTTP performer 返回。
type recordingIO struct {
	next    egress.IO
	t       *testing.T
	samples *[]protobuf.Sample
}

func (r recordingIO) Perform(ctx context.Context, request *v1.PhysicalIORequest) (*v1.PhysicalIOResult, error) {
	captureObject(r.t, r.samples, "physical-io-request", request, nil)
	result, err := r.next.Perform(ctx, request)
	if err == nil {
		captureObject(r.t, r.samples, "physical-io-result", result, nil)
	}
	return result, err
}

func capturePhysicalIO(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	target := simulator.New("idempotent")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, start := prepareStart(t, f)
	critical, err := egressio.NewFileLock(f.path)
	if err != nil {
		t.Fatal(err)
	}
	// 与 assembly.Open 相同的真实模块、锁和 performer；仅增加边界观察。
	gateway := egress.New(f.h.Tasks, f.h.Ledger, f.h.Content, recordingIO{next: egressio.HTTP{}, t: t, samples: samples}, critical)
	receipt, err := gateway.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, receipt, err)
	operation, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || operation.Effect.Outcome != "APPLIED" {
		t.Fatalf("requests=%d effects=%d operation=%v", len(requests), len(effects), operation)
	}
}
