//go:build fault && darwin

package fault_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/executor"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/egressio"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

type executionDriver struct {
	invoke             func(context.Context, *v1.Caller, *v1.StartExecutionCommand) (*v1.CommandReceipt, error)
	queryOperation     func(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Operation, error)
	queryObservation   func(context.Context, *v1.Caller, *v1.Ref) (*v1.RawObservation, error)
	queryBillingSource func(context.Context, *v1.Caller, *v1.Ref) (*v1.BillingSource, error)
}

func newExecutionDriver(h *assembly.Harness) executor.Driver {
	return executionDriver{
		invoke:             h.Egress.Invoke,
		queryOperation:     h.Ledger.QueryOperation,
		queryObservation:   h.Ledger.QueryObservation,
		queryBillingSource: h.Budget.QueryBillingSource,
	}
}

func (d executionDriver) Invoke(ctx context.Context, caller *v1.Caller, c *v1.StartExecutionCommand) (*v1.CommandReceipt, error) {
	return d.invoke(ctx, caller, c)
}

func (d executionDriver) QueryOperation(ctx context.Context, caller *v1.Caller, id *v1.GlobalName) (*v1.Operation, error) {
	return d.queryOperation(ctx, caller, id)
}

func (d executionDriver) QueryObservation(ctx context.Context, caller *v1.Caller, ref *v1.Ref) (*v1.RawObservation, error) {
	return d.queryObservation(ctx, caller, ref)
}

func (d executionDriver) QueryBillingSource(ctx context.Context, caller *v1.Caller, ref *v1.Ref) (*v1.BillingSource, error) {
	return d.queryBillingSource(ctx, caller, ref)
}

func executionAdapterSetup(t *testing.T, adapter string) executor.Fixture {
	t.Helper()
	if adapter == "FILE" {
		n := newNativeScenario(t)
		a, c := n.prepare(t, "adapter-contract", "CREATE", "", []byte("exact fixed file payload"))
		recorder := egressio.NewNativeFileRecorder(nil, nil)
		return executor.Fixture{LoseDispatchReceipt: loseExecutionDispatchReceipt, Driver: newExecutionDriver(n.h), Context: egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: recorder}), Admission: a, Start: c, Actual: func() (int, int) {
			events := recorder.Events()
			publications := 0
			for _, e := range events {
				if e.Kind == "rename" {
					publications++
				}
			}
			return len(events), publications
		}}
	}
	target := simulator.New("idempotent")
	server := httptest.NewServer(target)
	t.Cleanup(server.Close)
	h, e := assembly.Open(filepath.Join(t.TempDir(), "state.db"), "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = h.Close() })
	a, c := prepareStartFault(t, h, server.URL)
	return executor.Fixture{LoseDispatchReceipt: loseExecutionDispatchReceipt, Driver: newExecutionDriver(h), Context: context.Background(), ExpectedCalls: 1, Admission: a, Start: c, Actual: func() (int, int) { requests, effects := target.Snapshot(); return len(requests), len(effects) }}
}

func loseExecutionDispatchReceipt(ctx context.Context) (context.Context, error) {
	return sqlite.WithFault(ctx, "ledger.dispatch", sqlite.LoseReceipt)
}

// 规则：G1、G2、G3、G4、G5、G10、G11
func TestExecutionAdapterConformance(t *testing.T) {
	for _, adapter := range []string{"API", "FILE"} {
		t.Run(adapter, func(t *testing.T) {
			executor.Run(t, func(t *testing.T) executor.Fixture { return executionAdapterSetup(t, adapter) })
		})
	}
}
