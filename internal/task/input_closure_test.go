package task_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type closeAfterContent struct {
	*contentBridge
	afterRead    func() error
	afterPublish func() error
}

func (p *closeAfterContent) Read(ctx context.Context, sc runtime.Scope, a runtime.Auth, ref api.ContentRef) ([]byte, error) {
	result, err := p.contentBridge.Read(ctx, sc, a, ref)
	if err == nil && p.afterRead != nil {
		fn := p.afterRead
		p.afterRead = nil
		err = fn()
	}
	return result, err
}
func (p *closeAfterContent) Publish(ctx context.Context, sc runtime.Scope, id, media string, body []byte) (api.ContentRef, error) {
	result, err := p.contentBridge.Publish(ctx, sc, id, media, body)
	if err == nil && p.afterPublish != nil {
		fn := p.afterPublish
		p.afterPublish = nil
		err = fn()
	}
	return result, err
}

func TestOriginalInputAndSteerCannotConsumeAfterActualIncomingAllocationCloses(t *testing.T) {
	for _, kind := range []string{"input", "steer"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			content := &closeAfterContent{contentBridge: &contentBridge{}}
			var h *harness
			if dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN"); dsn != "" {
				store, err := postgres.Open(ctx, dsn)
				if err != nil {
					t.Fatal(err)
				}
				if err = store.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := store.Close(); err != nil {
						t.Error(err)
					}
				})
				h = harnessForStore(t, store, task.Ports{Content: content})
			} else {
				h = newHarness(t, task.Ports{Content: content})
			}
			configureContent(t, h, content.contentBridge)
			schema := api.Object(map[string]any{"value": api.String()}, "value")
			digest, _ := api.Digest(schema)
			schemaRef := api.ComponentRef{ComponentID: api.NewID("schema"), Version: "1", Digest: digest}
			var err error
			h.service, err = task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, AnswerSchemas: []task.AnswerSchemaDefinition{{Ref: schemaRef, Schema: schema}}}, task.Ports{Content: content})
			if err != nil {
				t.Fatal(err)
			}
			h.dispatch.Registry = runtime.NewRegistry()
			if err = h.service.Register(h.dispatch.Registry); err != nil {
				t.Fatal(err)
			}
			parentOwner := api.NewID("parent")
			parent := api.ObjectRef{TenantID: h.scope.TenantID, OwnerID: parentOwner, ObjectID: api.NewID("task"), Revision: 1}
			allocation := parent
			allocation.ObjectID = api.NewID("allocation")
			budget := []api.Amount{{Unit: "USD", Value: "5"}}
			deadline := api.Time(time.Now().Add(10 * time.Minute))
			status, err := h.store.Within(ctx, h.scope, []string{"task"}, func(tx runtime.Tx) error {
				_, err := h.service.ReceiveAllocationTx(ctx, tx, h.trusted(), allocation, task.Allocation{AllocationID: allocation.ObjectID, Revision: 1, ParentTaskRef: parent, ReceiverID: h.scope.OwnerID, Limits: budget, Deadline: deadline, State: "open"})
				return err
			})
			if err != nil || status != runtime.Committed {
				t.Fatalf("original parent allocation: %v %v", status, err)
			}
			goal, err := content.Publish(ctx, h.scope, api.NewID("upload"), "text/plain", []byte("exact original child goal"))
			if err != nil {
				t.Fatal(err)
			}
			id := api.NewID("task")
			delegation := &api.DelegationContext{DelegationID: api.NewID("delegation"), ParentTaskRef: parent, ParentGoalRevision: 1, AncestorTaskRefs: []api.ObjectRef{parent}, AgentBindingRef: parent, AllocationRef: allocation, PermissionRefs: []api.ObjectRef{allocation}, ParentControlSnapshot: api.ControlSnapshot{OrchestratorID: parentOwner, TaskID: parent.ObjectID, GoalRevision: 1, ControlRevision: 1, Status: "active", Control: "running", IssuedAt: api.Time(time.Now()), StartBefore: deadline, WindowID: api.NewID("window"), ProofRef: goal}, ContextDigest: api.Hash([]byte("explicit trusted allocation fixture")), ParentProofRef: goal}
			submission := h.command("task.submit", id, nil, task.SubmitInput{OrchestratorID: h.scope.OwnerID, GoalRef: goal, PolicyRef: h.policy.PolicyRef, Deadline: deadline, Budget: budget, DelegationContext: delegation})
			r, err := h.dispatch.Command(ctx, h.auth, api.Raw(submission))
			if err != nil || r.Stage != "applied" {
				t.Fatalf("original child submit: %+v %v", r, err)
			}
			original, err := h.service.Read(ctx, h.store, h.scope, h.auth, id)
			if err != nil {
				t.Fatal(err)
			}
			answer, err := content.Publish(ctx, h.scope, api.NewID("upload"), "application/json", []byte(`{"value":"exact original supplement"}`))
			if err != nil {
				t.Fatal(err)
			}
			closeOriginal := func() error {
				r, err := h.dispatch.Command(ctx, h.trusted(), api.Raw(h.command("budget.close", allocation.ObjectID, nil, task.AllocationCloseInput{AllocationRef: allocation, ParentTaskRef: parent, Reason: "original parent closed during content IO"})))
				if err != nil {
					return err
				}
				if r.Stage != "applied" {
					t.Fatalf("original allocation close: %+v", r)
				}
				return nil
			}
			var command api.Command
			var requestRef api.ObjectRef
			job := task.JobSteer
			if kind == "input" {
				req := api.InputRequest{RequestID: api.NewID("request"), TargetRef: h.scope.Ref(id, original.Revision), GoalRevision: &original.GoalRevision, Purpose: "clarify_goal", QuestionRef: goal, AnswerSchemaRef: schemaRef, PreviewRefs: []api.ContentRef{goal}, ExpiresAt: deadline, State: "pending"}
				status, err = h.store.Within(ctx, h.scope, []string{"task"}, func(tx runtime.Tx) error {
					var err error
					requestRef, err = h.service.CreateInputTx(ctx, tx, h.trusted(), id, req)
					return err
				})
				if err != nil || status != runtime.Committed {
					t.Fatalf("original input request: %v %v", status, err)
				}
				command = h.command("task.input", id, nil, task.InputAnswer{TaskID: id, RequestRef: requestRef, GoalRevision: 1, AnswerRef: answer})
				content.afterRead = closeOriginal
				job = task.JobInput
			} else {
				command = h.command("task.steer", id, nil, task.SteerInput{TaskID: id, BaseGoalRevision: 1, AmendmentRef: answer, SourceSubmissionRef: h.scope.Ref(submission.CommandID, 1), PrepareDeadline: deadline})
				content.afterPublish = closeOriginal
			}
			r, err = h.dispatch.Command(ctx, h.auth, api.Raw(command))
			if err != nil || r.Stage != "accepted" {
				t.Fatalf("original %s admission: %+v %v", kind, r, err)
			}
			drainKind(t, h, job)
			final, err := h.dispatch.Lookup(ctx, h.auth, command.CommandID)
			if err != nil || final.Stage != "rejected" || final.Error == nil || final.Error.Reason != "allocation_closed" {
				t.Fatalf("original %s consumed closed incoming permission: %+v %v", kind, final, err)
			}
			after, err := h.service.ContextFacts(ctx, h.store, h.scope, h.auth, id)
			if err != nil || after.Task.GoalRevision != 1 || !api.Equal(after.Task.GoalRef, goal) || len(after.SourceRefs) != 1 {
				t.Fatalf("late %s changed original goal/source: %+v %v", kind, after, err)
			}
			if kind == "input" {
				view, err := h.service.InputRequestRead(ctx, h.store, h.scope, h.auth, requestRef.ObjectID, 0)
				if err != nil || view.Request.State != "pending" || view.Request.AnswerRef != nil || view.Request.ConsumedBy != "" {
					t.Fatalf("late answer consumed original request: %+v %v", view, err)
				}
			}
			drainKind(t, h, job)
			again, err := h.dispatch.Lookup(ctx, h.auth, command.CommandID)
			if err != nil || !api.Equal(final, again) {
				t.Fatalf("rejected original command remained undecided: %+v %v", again, err)
			}
		})
	}
}
