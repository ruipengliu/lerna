package conformance_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 规则：G3
func TestCLIRequiresStableIdentityAndShowsPersistentDecision(t *testing.T) {
	h, err := assembly.Open(filepath.Join(t.TempDir(), "cli.db"), "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	cli := interaction.CLI{Sessions: h.Sessions, Tasks: h.Tasks, Durable: h.Durable, Caller: &v1.Caller{UserId: "alice", IssuerId: "cli"}, Domain: "local"}
	var output bytes.Buffer
	if err := cli.Run(context.Background(), []string{"submit", "--command", "cli-1", "--goal", "Write greeting"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "DECISION_ACCEPTED") {
		t.Fatalf("missing decision: %s", output.String())
	}
	output.Reset()
	if err := cli.Run(context.Background(), []string{"receipt", "cli-1"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "RECEIPT_QUERY_STATE_DECIDED") {
		t.Fatalf("missing durable receipt: %s", output.String())
	}
	if err := cli.Run(context.Background(), []string{"submit", "--goal", "missing identity"}, &output); err == nil {
		t.Fatal("command identity must be required for safe retry")
	}
}
