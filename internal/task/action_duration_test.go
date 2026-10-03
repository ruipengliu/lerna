package task_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
)

func TestAdmissionFreezesTrustedActionDurationWithoutExtendingTaskDeadline(t *testing.T) {
	for _, seconds := range []uint64{0, 2, 3600, 3601} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			ctx := context.Background()
			proof := &localProofFixture{}
			h := newHarness(t, task.Ports{ActionAuthorization: proof})
			proof.install(t, h.scope)
			original := h.submit(t)
			prepared := h.prepared(original, "0")
			if _, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared); err != nil {
				t.Fatal(err)
			}
			action := preparedAction(h, "0", "finite_original_duration")
			action.SafeRequirementCheck = true
			// 受信 producer 的机械编码边界，不向 Brain 草稿开放此字段。
			var frozen map[string]any
			if err := json.Unmarshal(api.Raw(action), &frozen); err != nil {
				t.Fatal(err)
			}
			frozen["max_duration_seconds"] = seconds
			if err := json.Unmarshal(api.Raw(frozen), &action); err != nil {
				t.Fatal(err)
			}
			before := time.Now()
			proposal := task.Proposal{DecisionID: prepared.DecisionID, Kind: "act", ReasonRef: original.GoalRef, Actions: []task.PreparedAction{action}}
			out, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), proposal, nil)
			if err != nil {
				t.Fatal(err)
			}
			after := time.Now()
			if seconds > 3600 {
				if out.Outcome != "rejected" || len(out.AdmittedOperationIDs) != 0 {
					t.Fatalf("invalid trusted duration admitted: %+v", out)
				}
				return
			}
			if out.Outcome != "adopted" || len(out.AdmittedOperationIDs) != 1 {
				t.Fatalf("legal original action was not admitted: %+v", out)
			}
			intent, err := h.service.ReadOperationIntent(ctx, h.store, h.scope, h.trusted(), action.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			deadline, err := api.ParseTime(intent.Deadline)
			if err != nil {
				t.Fatal(err)
			}
			if seconds == 2 {
				if deadline.Before(before.Add(2*time.Second-time.Millisecond)) || deadline.After(after.Add(2*time.Second)) {
					t.Fatalf("trusted action duration was lost: before=%v deadline=%v after=%v", before, deadline, after)
				}
			} else if intent.Deadline != original.Deadline {
				t.Fatalf("legacy or broader duration changed original Task deadline: %+v", intent)
			}
			if replay, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), proposal, nil); err != nil || !api.Equal(out, replay) {
				t.Fatalf("replay changed original admission: %+v %v", replay, err)
			}
			again, err := h.service.ReadOperationIntent(ctx, h.store, h.scope, h.trusted(), action.OperationID)
			if err != nil || !api.Equal(intent, again) {
				t.Fatalf("replay renewed action deadline/identity: %+v %v", again, err)
			}
		})
	}
}
