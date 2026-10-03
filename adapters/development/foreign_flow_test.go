package development

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type foreignFlowProbe struct {
	ContentRef  api.ContentRef `json:"content_ref"`
	Purpose     string         `json:"purpose"`
	Prepare     bool           `json:"prepare"`
	Nested      bool           `json:"nested"`
	LowerRoles  bool           `json:"lower_roles"`
	NextPurpose string         `json:"next_purpose"`
}

// 使用原真实设备输出、ES256 Current、两个库的held-copy和公开Runtime入口；
// 不以载体自身的字段或磁盘镜像推断新入口是否获准。
func verifyForeignFlowIsolation(t *testing.T, base context.Context, a *App, ref api.ContentRef) {
	t.Helper()
	check := func(ctx context.Context, auth runtime.Auth, input foreignFlowProbe) (any, error) {
		status, err := a.Store.Within(ctx, a.Scope, []string{"content", "memory", "platform", "governance"}, func(tx runtime.Tx) error {
			_, err := a.Memory.CheckContentTx(ctx, tx, auth, input.ContentRef, input.Purpose, "cloud", false)
			return err
		})
		if status == runtime.CommitUnknown {
			return false, runtime.ErrCommitUnknown
		}
		return err == nil, err
	}
	query := func(ctx context.Context, auth runtime.Auth, name string, input foreignFlowProbe) ([]byte, error) {
		q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, QueryID: api.NewID("query"), TargetID: ref.ContentID, Method: name, Payload: api.Raw(input)}
		return a.Dispatcher.Query(ctx, auth, api.Raw(q))
	}
	const readMethod = "host.foreign_flow_read"
	const checkMethod = "host.foreign_flow_check"
	var preparedContext context.Context
	if err := a.Registry.Register(runtime.Method{Contract: api.Contract[foreignFlowProbe, bool](checkMethod, "content", "query", false, false), Query: func(ctx context.Context, _ runtime.Store, _ runtime.Scope, auth runtime.Auth, q api.Query) (any, error) {
		var in foreignFlowProbe
		if err := api.Decode(q.Payload, &in); err != nil {
			return false, err
		}
		return check(ctx, auth, in)
	}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Registry.Register(runtime.Method{Contract: api.Contract[foreignFlowProbe, bool](readMethod, "content", "query", false, false), Query: func(ctx context.Context, _ runtime.Store, _ runtime.Scope, auth runtime.Auth, q api.Query) (any, error) {
		var in foreignFlowProbe
		if err := api.Decode(q.Payload, &in); err != nil {
			return false, err
		}
		if in.Prepare {
			if _, err := a.ReadContent(ctx, a.Scope, auth, in.ContentRef, in.Purpose); err != nil {
				return false, err
			}
			preparedContext = ctx
		}
		if in.Nested {
			if in.LowerRoles {
				auth.Roles = []string{}
			}
			if in.NextPurpose != "" {
				in.Purpose = in.NextPurpose
			}
			raw, err := query(ctx, auth, checkMethod, in)
			var result bool
			if err == nil {
				err = api.Decode(raw, &result)
			}
			return result, err
		}
		return check(ctx, auth, in)
	}}); err != nil {
		t.Fatal(err)
	}
	in := foreignFlowProbe{ContentRef: ref, Purpose: "execution_result"}
	if _, err := query(base, a.UserAuth, checkMethod, in); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("new independent query borrowed disk proof: %v", err)
	}
	in.Prepare, in.Nested = true, true
	if raw, err := query(base, a.UserAuth, readMethod, in); err != nil || string(raw) != "true" {
		t.Fatalf("same-call nested continuation lost actual current signature: %v %s", err, raw)
	}
	in.LowerRoles = true
	if _, err := query(base, a.UserAuth, readMethod, in); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("different exact Auth inherited another flow proof: %v", err)
	}
	in.LowerRoles, in.NextPurpose = false, "task.complete"
	if _, err := query(base, a.UserAuth, readMethod, in); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("a nested use widened the original purpose: %v", err)
	}
	if _, err := query(base, a.UserAuth, checkMethod, foreignFlowProbe{ContentRef: ref, Purpose: "execution_result"}); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("next independent query reused previous flow signature: %v", err)
	}
	observed := make(chan error, 1)
	const kind = "platform.foreign_flow_isolation"
	if err := a.Registry.RegisterJob(kind, func(ctx context.Context, st runtime.Store, scope runtime.Scope, work runtime.Work) error {
		_, err := check(ctx, a.UserAuth, foreignFlowProbe{ContentRef: ref, Purpose: "execution_result"})
		observed <- err
		return runtime.Finish(ctx, st, scope, []string{"platform"}, work, runtime.Done(), nil)
	}); err != nil {
		t.Fatal(err)
	}
	status, err := a.Store.Within(base, a.Scope, []string{"platform"}, func(tx runtime.Tx) error {
		now, err := tx.Now(base)
		if err != nil {
			return err
		}
		_, err = tx.Raise(base, kind, api.NewID("work"), a.Scope.Ref(api.NewID("intent"), 1), now)
		return err
	})
	if status != runtime.Committed || err != nil {
		t.Fatal("actual new flow Job not committed", status, err)
	}
	if err = runtime.Drain(preparedContext, a.Store, a.Scope, a.Registry, 300); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-observed:
		if !api.IsCode(err, "dependency_unavailable") {
			t.Fatalf("new Job inherited old request signature: %v", err)
		}
	default:
		t.Fatal("actual new Job did not run")
	}
}
