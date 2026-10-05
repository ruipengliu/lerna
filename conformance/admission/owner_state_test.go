package admission_test

import (
	"context"
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

// 规则：G2、G4、C7、准入-3、准入-4
// 后续控制工单尚未提供命令；本用例仅构造任务所属域的既有事实，断言仍通过公共准入与查询接口。
func TestOwnerTaskStatesCannotBypassAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(*v1.Task, *v1.PlanningState)
	}{
		{"draft", "REQUIREMENTS_NOT_ACCEPTED", func(task *v1.Task, _ *v1.PlanningState) {
			task.RequirementsStatus = v1.RequirementsStatus_REQUIREMENTS_STATUS_DRAFT
		}},
		{"unprocessed-input", "REQUIREMENTS_NOT_ACCEPTED", func(task *v1.Task, _ *v1.PlanningState) { task.BoundInputVersion = 0 }},
		{"closed", "TASK_NOT_ACTIVE", func(task *v1.Task, _ *v1.PlanningState) { task.Lifecycle = v1.TaskLifecycle_TASK_LIFECYCLE_CLOSED }},
		{"paused", "TASK_NOT_ACTIVE", func(task *v1.Task, _ *v1.PlanningState) { task.Control = v1.TaskControl_TASK_CONTROL_PAUSED }},
		{"cancelling", "TASK_NOT_ACTIVE", func(task *v1.Task, _ *v1.PlanningState) { task.Control = v1.TaskControl_TASK_CONTROL_CANCELLING }},
		{"verifying", "TASK_NOT_ACTIVE", func(_ *v1.Task, p *v1.PlanningState) { p.VerificationFreeze = 1 }},
		{"ancestor", "UNSUPPORTED_FEATURE", func(task *v1.Task, _ *v1.PlanningState) {
			task.ParentTaskRef = &v1.Ref{Name: task.TaskId, Revision: 1, SchemaId: "lerna.v1.Task"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			p, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			tc.change(task, p)
			store, e := sqlite.Open(f.path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			e = store.Transaction(f.ctx, "tasks.planning", func(tx context.Context) error {
				if e := store.SaveTask(tx, task); e != nil {
					return e
				}
				return store.SavePlanning(tx, p)
			})
			store.Close()
			if e != nil {
				t.Fatal(e)
			}
			proposal := f.propose(t, nil)
			r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant})
			if e != nil || r.Error.GetCode() != tc.code {
				t.Fatalf("got %v %v", r, e)
			}
			assertNoAdmission(t, f)
		})
	}
}
