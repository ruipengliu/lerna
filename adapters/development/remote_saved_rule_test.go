package development

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/executor"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
)

type remoteReportObserver func(context.Context, *App, *executor.Host, task.ContextFacts, int32) error

// 原 Cloud Task→两次独立设备操作→当前准确 ConditionResult。
// 先独立核真实设备文件及两份原回执；不把 write cache 当作 readback 证据。
func TestSavedRuleVerifiesOriginalPairedDeviceWriteAndIndependentRead(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			observed := false
			observe := func(ctx context.Context, a *App, device *executor.Host, facts task.ContextFacts, posts int32) error {
				if observed || len(facts.Operations) != 2 || len(facts.Checks) != 2 {
					return nil
				}
				observed = true
				var savedCheck *api.ConditionResult
				for i := range facts.Checks {
					if api.Equal(facts.Checks[i].RuleRef, a.SavedRule) {
						savedCheck = &facts.Checks[i]
					}
				}
				if savedCheck == nil {
					return fmt.Errorf("SavedRule result was omitted")
				}
				// 成功收束会提升当前control；操作归属于准确原Check的Task版本。
				scopeBytes, err := a.ReadContent(ctx, a.Scope, a.ServiceAuth, savedCheck.ScopeRef, "task.context")
				if err != nil {
					return err
				}
				var scopeFields map[string]json.RawMessage
				if err = json.Unmarshal(scopeBytes, &scopeFields); err != nil {
					return err
				}
				var checkedTaskRef api.ObjectRef
				if err = api.Decode(scopeFields["task_ref"], &checkedTaskRef); err != nil {
					return err
				}
				if checkedTaskRef.TenantID != a.Scope.TenantID || checkedTaskRef.OwnerID != a.Scope.OwnerID || checkedTaskRef.ObjectID != facts.Task.TaskID {
					return fmt.Errorf("original SavedRule Task scope changed")
				}
				originalTaskBytes, err := a.queryAs(ctx, a.UserAuth, "task.read", facts.Task.TaskID, task.ReadInput{Revision: checkedTaskRef.Revision})
				if err != nil {
					return err
				}
				var checkedTask api.Task
				if err = api.Decode(originalTaskBytes, &checkedTask); err != nil {
					return err
				}
				if checkedTask.GoalRevision != facts.Task.GoalRevision || !api.Equal(checkedTask.GoalRef, facts.Task.GoalRef) || checkedTask.Status != "active" || checkedTask.Control != "running" || savedCheck.GoalRevision != checkedTask.GoalRevision {
					return fmt.Errorf("original checked goal changed")
				}
				if facts.Task.ControlRevision != checkedTask.ControlRevision && (facts.Task.Status != "succeeded" || facts.Task.ResultRef == nil || facts.Task.ControlRevision != checkedTask.ControlRevision+1) {
					return fmt.Errorf("original control changed outside successful completion")
				}
				goal := brain.GoalSpec{Kind: "report", Title: "Device report", Body: "The original cloud Task saved this exact report on its paired device.", SavePath: "remote-report.md"}
				var read target.FileReadResult
				for _, operation := range facts.Operations {
					if operation.Intent.ExecutorID != device.Scope.OwnerID || operation.Intent.TaskRef.ObjectID != checkedTask.TaskID || operation.Intent.GoalRevision != checkedTask.GoalRevision || operation.Intent.ControlRevision != checkedTask.ControlRevision || !operation.Fact.Closed || operation.Fact.MayApplyLater {
						return fmt.Errorf("original paired device or Task generation changed")
					}
					route, err := a.remoteExecutors.client(ctx, operation.Intent.ExecutorID)
					if err != nil {
						return err
					}
					actual, err := route.Client.Get(ctx, operation.Intent.OperationID)
					if err != nil || actual.Operation.ResultRef == nil || len(actual.Attempts.Items) != 1 || actual.Attempts.Items[0].StartedAt == "" || !actual.ActuallyStopped || !actual.NewAttemptsClosed || !actual.Operation.UsageFinal || actual.Attempts.Partial || !actual.Attempts.Exhausted || !api.Equal(actual.Operation.TaskRef, operation.Intent.TaskRef) {
						return fmt.Errorf("original independent read Attempt did not close: %w", err)
					}
					if !api.Equal(operation.Intent.CapabilityRef, target.FileReadCapability().Ref) {
						continue
					}
					bytes, err := a.ReadContent(ctx, a.Scope, a.UserAuth, *actual.Operation.ResultRef, "execution_result")
					if err != nil {
						return err
					}
					if err = api.Decode(bytes, &read); err != nil {
						return err
					}
					data, err := base64.StdEncoding.Strict().DecodeString(read.DataBase64)
					if err != nil || read.Path != goal.SavePath || string(data) != originalRemoteReport {
						return fmt.Errorf("original actual read bytes do not match the exact report")
					}
					verifyRemoteReportTarget(t, ctx, a, device, route.Client, facts, goal, read)
				}
				if read.Version == "" || posts != 4 {
					return fmt.Errorf("original report did not retain both effects and four physical model calls")
				}
				for _, check := range facts.Checks {
					if api.Equal(check.RuleRef, a.SavedRule) {
						t.Logf("SAVED_RULE_ORIGINAL scope=%s task=%s goal=%d control=%d device=%s check=%s verdict=%s original_read_hash=%s original_read_length=%d", api.Raw(a.Scope), facts.Task.TaskID, facts.Task.GoalRevision, facts.Task.ControlRevision, device.Scope.OwnerID, check.CheckID, check.Verdict, api.Hash([]byte(originalRemoteReport)), len(originalRemoteReport))
						if check.Verdict != "pass" || check.Applicability != "usable" || check.Basis != "verified" {
							return fmt.Errorf("SavedRule rejected the actual original device target: %s/%s/%s", check.Verdict, check.Applicability, check.Basis)
						}
						return nil
					}
				}
				return fmt.Errorf("SavedRule result was omitted")
			}
			runRemoteExecutorTask(t, driver, true, true, observe)
			if !observed {
				t.Fatal("original SavedRule result was never observed")
			}
		})
	}
}
