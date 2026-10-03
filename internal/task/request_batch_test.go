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

func requestBatchFixture(t *testing.T, h *harness) []api.ObjectRef {
	t.Helper()
	schema := api.Object(map[string]any{"answer": api.String()}, "answer")
	digest, _ := api.Digest(schema)
	ref := api.ComponentRef{ComponentID: api.NewID("schema"), Version: "1.0.0", Digest: digest}
	service, err := task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, AnswerSchemas: []task.AnswerSchemaDefinition{{Ref: ref, Schema: schema}}}, task.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	h.service = service
	h.dispatch.Registry = runtime.NewRegistry()
	if err = service.Register(h.dispatch.Registry); err != nil {
		t.Fatal(err)
	}
	refs := []api.ObjectRef{}
	for i := 0; i < 2; i++ {
		current := h.submit(t)
		request := api.InputRequest{RequestID: api.NewID("request"), TargetRef: h.scope.Ref(current.TaskID, current.Revision), GoalRevision: &current.GoalRevision, Purpose: "clarify_goal", QuestionRef: h.content("explicit preapproved missing criterion"), AnswerSchemaRef: ref, PreviewRefs: []api.ContentRef{current.GoalRef}, ExpiresAt: api.Time(time.Now().Add(time.Minute)), State: "pending"}
		var requestRef api.ObjectRef
		status, err := h.store.Within(context.Background(), h.scope, []string{"task"}, func(tx runtime.Tx) error {
			var err error
			requestRef, err = service.CreateInputTx(context.Background(), tx, h.trusted(), current.TaskID, request)
			return err
		})
		if err != nil || status != runtime.Committed {
			t.Fatalf("accurate InputRequest creation: %v %v", status, err)
		}
		refs = append(refs, requestRef)
	}
	return refs
}

func TestRequestBatchKeepsInputOrderAndRejectsScopeOrIdentityAmbiguity(t *testing.T) {
	h := newHarness(t, task.Ports{})
	refs := requestBatchFixture(t, h)
	ctx := context.Background()
	views := func(auth runtime.Auth, inputs []api.ObjectRef) ([]task.InputRequestView, error) {
		var out []task.InputRequestView
		_, err := h.store.Within(ctx, h.scope, []string{"task"}, func(tx runtime.Tx) error {
			var err error
			out, err = h.service.RequestViewsTx(ctx, tx, auth, inputs)
			return err
		})
		return out, err
	}
	out, err := views(h.auth, []api.ObjectRef{refs[1], refs[0]})
	if err != nil || len(out) != 2 || !api.Equal(out[0].RequestRef, refs[1]) || !api.Equal(out[1].RequestRef, refs[0]) || len(out[0].AnswerSchema) == 0 {
		t.Fatalf("batch changed accurate input order/schema: %+v %v", out, err)
	}
	if _, err = views(h.auth, []api.ObjectRef{refs[0], refs[0]}); !api.IsCode(err, "invalid_request") {
		t.Fatalf("duplicate request accepted: %v", err)
	}
	crossScope := refs[0]
	crossScope.TenantID = api.NewID("tenant")
	if _, err = views(h.auth, []api.ObjectRef{refs[1], crossScope}); !api.IsCode(err, "forbidden") {
		t.Fatalf("cross-tenant request accepted: %v", err)
	}
	other := h.auth
	other.SubjectID = api.NewID("subject")
	if _, err = views(other, refs); !api.IsCode(err, "forbidden") {
		t.Fatalf("wrong request principal accepted: %v", err)
	}
	stale := refs[0]
	stale.Revision++
	if _, err = views(h.auth, []api.ObjectRef{refs[1], stale}); !api.IsCode(err, "revision_conflict") {
		t.Fatalf("inexact request version accepted: %v", err)
	}
	if _, err = views(h.auth, make([]api.ObjectRef, 21)); !api.IsCode(err, "invalid_request") {
		t.Fatalf("unbounded batch accepted: %v", err)
	}
}

// 仅人为放大原数据库行锁交错，不以包装Tx替代持久数据库。
type delayedFirstTaskTx struct {
	runtime.Tx
	delayed bool
}

func (tx *delayedFirstTaskTx) Get(ctx context.Context, ns, id string, value any) (uint64, error) {
	revision, err := tx.Tx.Get(ctx, ns, id, value)
	if err == nil && ns == "task.tasks" && !tx.delayed {
		tx.delayed = true
		time.Sleep(100 * time.Millisecond)
	}
	return revision, err
}

func TestPostgresReversedRequestBatchesLockTaskRootsBeforeAllRequests(t *testing.T) {
	dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("HARNESS_TEST_POSTGRES_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	h := harnessForStore(t, store, task.Ports{})
	refs := requestBatchFixture(t, h)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, batch := range [][]api.ObjectRef{{refs[0], refs[1]}, {refs[1], refs[0]}} {
		go func(batch []api.ObjectRef) {
			<-start
			status, err := store.Within(ctx, h.scope, []string{"task"}, func(tx runtime.Tx) error {
				views, err := h.service.RequestViewsTx(ctx, &delayedFirstTaskTx{Tx: tx}, h.auth, batch)
				if err != nil {
					return err
				}
				if len(views) != 2 || !api.Equal(views[0].RequestRef, batch[0]) || !api.Equal(views[1].RequestRef, batch[1]) {
					return api.E("invalid_state", "inexact_batch_output")
				}
				return nil
			})
			if err == nil && status != runtime.Committed {
				err = api.E("invalid_state", "batch_did_not_commit")
			}
			results <- err
		}(batch)
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err = <-results; err != nil {
			t.Fatalf("reversed real PG batch did not share one lock order: %v", err)
		}
	}
}
