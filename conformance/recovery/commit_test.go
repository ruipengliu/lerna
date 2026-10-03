//go:build integration

package recovery_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/conformance/internal/testkit"
	"github.com/ruipengliu/lerna/contract"
)

func TestPGCommitConfirmationLossPreservesUnknownAndOriginalIdentity(t *testing.T) {
	observer := database(t)
	ctx := contextFor(t)
	configuration, _ := configurations.Load(observer)
	cfg := configuration.(postgres.Config)
	proxy, err := testkit.NewCommitProxy(ctx, cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proxy.Close)
	cfg.DSN = proxy.DSN()
	writer, err := postgres.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Close() })
	h := hostFor(writer, owner, principal)
	original := command("commit-unknown", "input", "confirmed server commit", nil, future())
	proxy.Arm()
	out, err := h.Record(ctx, original, &principal)
	if err != nil {
		t.Fatalf("ambiguous COMMIT reported certain failure: %v", err)
	}
	select {
	case <-proxy.Acknowledged():
	case <-ctx.Done():
		t.Fatal("proxy did not observe server COMMIT confirmation")
	}
	unknown, ok := out.AsCommitUnknown()
	if !ok || unknown.CommandRef != (contract.CommandRef{Owner: owner, CommandID: "commit-unknown"}) {
		t.Fatal("COMMIT ambiguity lost original identity")
	}
	if _, err = contract.Encode(out); err != nil {
		t.Fatalf("Host returned invalid public unknown outcome: %v", err)
	}
	replacement := hostFor(observer, owner, principal)
	ref := unknown.CommandRef
	result, err := contract.GetCommand(ctx, readWire(ref), &principal, replacement.Permissions, replacement, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := result.AsFound()
	if !ok {
		t.Fatal("confirmed commit was not recoverable")
	}
	applied, ok := found.Receipt.AsApplied()
	if !ok || applied.Revision != "1" {
		t.Fatal("confirmed commit lost applied receipt")
	}
	replay, err := replacement.Record(ctx, original, &principal)
	assertReceiptSame(t, found.Receipt, assertReceived(t, replay, err))
	observed, err := replacement.Observe(ctx, "input", &principal)
	if err != nil || observed.Input.Revision != 1 || observed.Job.WorkRevision != 1 {
		t.Fatalf("commit recovery duplicated/lost responsibility: %+v %v", observed, err)
	}
}
