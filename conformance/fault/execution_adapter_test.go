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
	"github.com/ruipengliu/lerna/infra/egressio"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

func executionAdapterSetup(t *testing.T, adapter string) executor.Fixture {
	t.Helper()
	if adapter == "FILE" {
		n := newNativeScenario(t)
		a, c := n.prepare(t, "adapter-contract", "CREATE", "", []byte("exact fixed file payload"))
		recorder := egressio.NewNativeFileRecorder(nil, nil)
		return executor.Fixture{LoseDispatchReceipt: loseExecutionDispatchReceipt, Harness: n.h, Context: egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: recorder}), Admission: a, Start: c, Actual: func() (int, int) {
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
	return executor.Fixture{LoseDispatchReceipt: loseExecutionDispatchReceipt, Harness: h, Context: context.Background(), ExpectedCalls: 1, Admission: a, Start: c, Actual: func() (int, int) { requests, effects := target.Snapshot(); return len(requests), len(effects) }}
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
