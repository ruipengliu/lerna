//go:build fault

package fault_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

// 规则：G3、G11
func TestLostReceiptKeepsOriginalResponsibility(t *testing.T) {
	for _, point := range []string{"content.stage", "durable.submit", "durable.decide"} {
		t.Run(point, func(t *testing.T) {
			h, err := assembly.Open(filepath.Join(t.TempDir(), "lost.db"), "alice", "local")
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			ctx, err := sqlite.WithFault(context.Background(), point, sqlite.LoseReceipt)
			if err != nil {
				t.Fatal(err)
			}
			caller, c := goal()
			receipt, err := h.Sessions.SubmitGoal(ctx, caller, c)
			if point == "durable.decide" {
				if err != nil {
					t.Fatal(err)
				}
				err = h.Sessions.ProcessPending(ctx, caller)
			} else if receipt != nil {
				t.Fatalf("lost receipt leaked acknowledgement: %v", receipt)
			}
			var failure *command.Failure
			if !errors.As(err, &failure) || failure.Detail.CommandAcceptance != v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN || failure.Detail.Category != v1.ErrorCategory_ERROR_CATEGORY_INDETERMINATE {
				t.Fatalf("receipt loss must be unknown: %v", err)
			}
			q, err := h.Durable.QueryReceipt(context.Background(), caller, c.Identity)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]v1.ReceiptQueryState{"content.stage": v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND, "durable.submit": v1.ReceiptQueryState_RECEIPT_QUERY_STATE_SUBMITTED, "durable.decide": v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED}[point]
			if q.State != want {
				t.Fatalf("lost reply changed durable state: %v", q)
			}
			if _, err := h.Sessions.SubmitGoal(context.Background(), caller, c); err != nil {
				t.Fatal(err)
			}
			if err := h.Sessions.ProcessPending(context.Background(), caller); err != nil {
				t.Fatal(err)
			}
			assertOneTask(t, h, caller, c)
		})
	}
}
