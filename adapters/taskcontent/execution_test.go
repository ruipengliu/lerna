package taskcontent_test

import (
	"context"
	"errors"
	"lerna/adapters/taskcontent"
	"lerna/artifacts"
	"lerna/execution"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"testing"
)

type executionQueriesFunc func(context.Context, tasks.ActionBinding, string) error

func (f executionQueriesFunc) ChargeExecutionQuery(ctx context.Context, a tasks.ActionBinding, key string) error {
	return f(ctx, a, key)
}

type contentFunc func(context.Context, artifacts.Binding, *wire.ContentRequest) (*wire.ContentResponse, error)

func (f contentFunc) Call(ctx context.Context, b artifacts.Binding, r *wire.ContentRequest) (*wire.ContentResponse, error) {
	return f(ctx, b, r)
}

func TestExecutionBudgetKeepsOriginalActionAndChargesBeforeIO(t *testing.T) {
	for _, denied := range []error{fetch.Denied, artifacts.Error("PERMISSION_DENIED")} {
		t.Run(denied.Error(), func(t *testing.T) {
			ctx := context.Background()
			q := tasks.Qualification{Ref: tasks.Ref{Namespace: "local", TaskID: "task"}, Version: 3}
			r := execution.Request{Qualification: q, OperationID: "action", DescriptorSHA256: "digest", InputRef: "input", ResourceVersion: 7, ControlVersion: 9}
			want := tasks.ActionBinding{Qualification: q, OperationID: "action", Descriptor: "digest", InputRef: "input", ResourceVersion: 7, ControlVersion: 9}
			keys := map[string]bool{}
			blocked, charges, reads := false, 0, 0
			queries := executionQueriesFunc(func(_ context.Context, action tasks.ActionBinding, key string) error {
				if action != want || key == "" || keys[key] {
					t.Fatal("changed original action or reused observation identity")
				}
				keys[key] = true
				if blocked {
					return denied
				}
				charges++
				return nil
			})
			budget := taskcontent.BindExecution(queries, r, denied)
			r.OperationID, r.InputRef = "replacement", "replacement"
			copy := budget.Action()
			copy.Qualification.Version++
			if budget.Action() != want || budget.ChargeQuery(ctx, copy.Qualification, "wrong-worker") != denied || len(keys) != 0 {
				t.Fatal("replacement qualification reached original query authority")
			}
			unavailable := errors.New("content unavailable")
			content := contentFunc(func(context.Context, artifacts.Binding, *wire.ContentRequest) (*wire.ContentResponse, error) {
				reads++
				if charges != reads {
					t.Fatal("content I/O preceded its query charge")
				}
				return nil, unavailable
			})
			metered, err := taskcontent.New(content, budget, q)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, err := metered.Call(ctx, artifacts.Binding{Namespace: "local"}, &wire.ContentRequest{Method: "READ"}); err != unavailable {
					t.Fatal("failed content read changed error semantics")
				}
			}
			blocked = true
			if _, err := metered.Call(ctx, artifacts.Binding{Namespace: "local"}, &wire.ContentRequest{Method: "READ"}); err != denied || reads != 2 || charges != 2 {
				t.Fatal("denied query reached Content or failed reads were refunded")
			}
		})
	}
	var unbound taskcontent.ExecutionBudget
	if unbound.ChargeQuery(context.Background(), tasks.Qualification{}, "unbound") == nil {
		t.Fatal("unbound budget granted a query")
	}
}
