package governance

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 在线原Use可以保留一项委派责任；离线Lease并不因此取得委派能力。
func onlineUseContract[I, O any](name, role string) api.MethodContract {
	c := api.Contract[I, O](name, Namespace, role, false, false)
	for _, schema := range []api.Schema{c.InputSchema, c.OutputSchema} {
		restrictUseTargets(schema, []any{"operation", "decision", "content_use", "delegation"})
	}
	return c
}

func restrictUseTargets(schema api.Schema, kinds []any) {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}
	if _, exists := properties["target_kind"]; exists {
		properties["target_kind"] = api.Schema{"type": "string", "enum": kinds}
	}
	for _, name := range []string{"scope", "use"} {
		switch nested := properties[name].(type) {
		case api.Schema:
			restrictUseTargets(nested, kinds)
		}
	}
}

func offlineLeaseContract[I, O any](name, role string, cas, accepted bool) api.MethodContract {
	c := api.Contract[I, O](name, Namespace, role, cas, accepted)
	for _, schema := range []api.Schema{c.InputSchema, c.OutputSchema} {
		restrictUseTargets(schema, []any{"operation", "decision", "content_use"})
	}
	return c
}

func registerOfflineLeaseCommand[I, O any](s *Service, r *runtime.Registry, name string, cas, accepted bool, apply func(context.Context, runtime.Tx, runtime.Auth, api.Command, I) (runtime.Outcome, error)) error {
	return r.Register(runtime.Method{Contract: offlineLeaseContract[I, O](name, "command", cas, accepted), Participants: s.participants(), Apply: func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command) (runtime.Outcome, error) {
		var input I
		if err := api.Decode(c.Payload, &input); err != nil {
			return runtime.Outcome{}, err
		}
		return apply(ctx, tx, auth, c, input)
	}})
}

func registerOfflineLeaseQuery[I, O any](r *runtime.Registry, name string, query func(context.Context, runtime.Store, runtime.Scope, runtime.Auth, api.Query, I) (O, error)) error {
	return r.Register(runtime.Method{Contract: offlineLeaseContract[I, O](name, "query", false, false), Query: func(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query) (any, error) {
		var input I
		if err := api.Decode(q.Payload, &input); err != nil {
			return nil, err
		}
		return query(ctx, store, scope, auth, q, input)
	}})
}

func (s *Service) registerOnlineGrantUse(r *runtime.Registry) error {
	return r.Register(runtime.Method{Contract: onlineUseContract[UseRequest, UseReceipt]("grant.use", "command"), Participants: s.participants(), Apply: func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command) (runtime.Outcome, error) {
		var in UseRequest
		if err := api.Decode(c.Payload, &in); err != nil {
			return runtime.Outcome{}, err
		}
		if auth.TenantID != tx.Scope().TenantID {
			return runtime.Outcome{}, api.E("forbidden", "tenant_mismatch")
		}
		out, err := s.UseTx(ctx, tx, auth, in)
		return runtime.Applied(out), err
	}})
}

func (s *Service) registerOnlineGrantCheck(r *runtime.Registry) error {
	return r.Register(runtime.Method{Contract: onlineUseContract[UseRequest, UseReceipt]("grant.check", "query"), Query: func(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query) (any, error) {
		var in UseRequest
		if err := api.Decode(q.Payload, &in); err != nil {
			return nil, err
		}
		return s.checkGrantQuery(ctx, store, scope, auth, q, in)
	}})
}

func (s *Service) registerOnlineGrantUseRead(r *runtime.Registry) error {
	read := queryByID[UseReceipt]("uses", func(a runtime.Auth, u UseReceipt) error {
		if a.SubjectID != u.SubjectRef.ObjectID && !a.HasRole("grant_authority") {
			return api.E("forbidden", "use_redacted")
		}
		return nil
	})
	return r.Register(runtime.Method{Contract: onlineUseContract[IDInput, UseReceipt]("grant.use.get", "query"), Query: func(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query) (any, error) {
		var in IDInput
		if err := api.Decode(q.Payload, &in); err != nil {
			return nil, err
		}
		return read(ctx, store, scope, auth, q, in)
	}})
}
