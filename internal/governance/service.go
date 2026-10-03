package governance

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func ns(kind string) string { return Namespace + "/" + kind }
func digestID(prefix string, v any) string {
	d, err := api.Digest(v)
	if err != nil {
		panic(err)
	}
	return prefix + "_" + strings.TrimPrefix(d, "sha256:")[:32]
}
func componentKey(ref api.ComponentRef) string { return digestID("component", ref) }
func refKey(ref api.ObjectRef) string {
	return fmt.Sprintf("%s/%s/%s/%d", ref.TenantID, ref.OwnerID, ref.ObjectID, ref.Revision)
}
func requireRole(auth runtime.Auth, role string) error {
	if !auth.HasRole(role) {
		return api.E("forbidden", "trusted_"+role+"_required")
	}
	return nil
}
func ownerRef(scope runtime.Scope, ref api.ObjectRef) error {
	if err := runtime.CheckRef(scope, ref); err != nil {
		return err
	}
	if ref.OwnerID != scope.OwnerID {
		return api.E("unsupported", "wrong_authority")
	}
	return nil
}
func requireCAS(c api.Command, revision uint64) error {
	if c.ExpectedRevision == nil || *c.ExpectedRevision != revision {
		return api.E("revision_conflict", "revision_changed")
	}
	return nil
}
func before(now time.Time, cutoff string) error {
	until, err := api.ParseTime(cutoff)
	if err != nil {
		return api.E("invalid_request", "invalid_expiry")
	}
	if !now.Before(until) {
		return api.E("invalid_state", "window_expired")
	}
	return nil
}
func minTime(a, b string) string {
	x, e := api.ParseTime(a)
	if e != nil {
		return b
	}
	y, e := api.ParseTime(b)
	if e != nil || x.Before(y) {
		return a
	}
	return b
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func subset(xs, allowed []string) bool {
	if len(xs) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, x := range xs {
		if x == "" || seen[x] || !contains(allowed, x) {
			return false
		}
		seen[x] = true
	}
	return true
}
func amountValue(as []api.Amount, unit string) string {
	for _, a := range as {
		if a.Unit == unit {
			return a.Value
		}
	}
	return "0"
}
func amountsAdd(a, b []api.Amount) ([]api.Amount, error) {
	if err := api.ValidateAmounts(a); err != nil {
		return nil, err
	}
	if err := api.ValidateAmounts(b); err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, v := range a {
		m[v.Unit] = v.Value
	}
	for _, v := range b {
		old := m[v.Unit]
		if old == "" {
			old = "0"
		}
		n, err := api.AddDecimal(old, v.Value)
		if err != nil {
			return nil, err
		}
		m[v.Unit] = n
	}
	return amountsMap(m), nil
}
func amountsMap(m map[string]string) []api.Amount {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]api.Amount, 0, len(keys))
	for _, k := range keys {
		out = append(out, api.Amount{Unit: k, Value: m[k]})
	}
	return out
}
func amountsRemaining(original, cumulative []api.Amount) ([]api.Amount, error) {
	m := map[string]string{}
	for _, v := range original {
		n, err := api.SubDecimal(v.Value, amountValue(cumulative, v.Unit))
		if err != nil {
			n = "0"
		}
		m[v.Unit] = n
	}
	return amountsMap(m), nil
}
func amountsSubtract(a, b []api.Amount) ([]api.Amount, error) {
	m := map[string]string{}
	for _, v := range a {
		m[v.Unit] = v.Value
	}
	for _, v := range b {
		old := m[v.Unit]
		if old == "" {
			old = "0"
		}
		n, err := api.SubDecimal(old, v.Value)
		if err != nil {
			return nil, err
		}
		m[v.Unit] = n
	}
	return amountsMap(m), nil
}
func bounded(a, limits []api.Amount) bool {
	for _, v := range a {
		limit := amountValue(limits, v.Unit)
		cmp, err := api.CompareDecimal(v.Value, limit)
		if err != nil || cmp > 0 {
			return false
		}
	}
	return true
}
func amountsMonotone(old, next []api.Amount) bool {
	for _, v := range old {
		cmp, err := api.CompareDecimal(amountValue(next, v.Unit), v.Value)
		if err != nil || cmp < 0 {
			return false
		}
	}
	return true
}
func errMissing(err error) bool {
	return errors.Is(err, runtime.ErrNotFound) || api.IsCode(err, "not_found")
}

func registerCommand[I, O any](service *Service, registry *runtime.Registry, name string, cas, accepted bool, fn func(context.Context, runtime.Tx, runtime.Auth, api.Command, I) (runtime.Outcome, error)) error {
	return registry.Register(runtime.Method{Contract: api.Contract[I, O](name, Namespace, "command", cas, accepted), Participants: service.participants(), Apply: func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command) (runtime.Outcome, error) {
		var input I
		if err := api.Decode(c.Payload, &input); err != nil {
			return runtime.Outcome{}, err
		}
		return fn(ctx, tx, auth, c, input)
	}})
}
func registerQuery[I, O any](registry *runtime.Registry, name string, fn func(context.Context, runtime.Store, runtime.Scope, runtime.Auth, api.Query, I) (O, error)) error {
	return registry.Register(runtime.Method{Contract: api.Contract[I, O](name, Namespace, "query", false, false), Query: func(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query) (any, error) {
		var input I
		if err := api.Decode(q.Payload, &input); err != nil {
			return nil, err
		}
		return fn(ctx, store, scope, auth, q, input)
	}})
}
func queryByID[T any](kind string, authorize func(runtime.Auth, T) error) func(context.Context, runtime.Store, runtime.Scope, runtime.Auth, api.Query, IDInput) (T, error) {
	return func(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query, in IDInput) (T, error) {
		var out T
		_, err := store.Read(ctx, scope, ns(kind), in.ID, 0, &out)
		if err != nil {
			return out, err
		}
		if authorize != nil {
			err = authorize(auth, out)
		}
		return out, err
	}
}
func queryPage[T any](kind string, role string) func(context.Context, runtime.Store, runtime.Scope, runtime.Auth, api.Query, api.ListInput) (api.Page[T], error) {
	return func(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query, in api.ListInput) (api.Page[T], error) {
		out := api.Page[T]{Items: []T{}, Gaps: []string{}}
		if role != "" {
			if err := requireRole(auth, role); err != nil {
				return out, err
			}
		}
		if in.Limit < 1 || in.Limit > 100 {
			return out, api.E("invalid_request", "invalid_page_limit")
		}
		rows, err := store.List(ctx, scope, ns(kind), "", in.Cursor, int(in.Limit)+1)
		if err != nil {
			return out, err
		}
		out.Exhausted = len(rows) <= int(in.Limit)
		if !out.Exhausted {
			rows = rows[:in.Limit]
		}
		for _, row := range rows {
			var item T
			if err = row.Decode(&item); err != nil {
				return out, err
			}
			out.Items = append(out.Items, item)
			out.NextCursor = row.ID
		}
		if out.Exhausted {
			out.NextCursor = ""
		}
		out.CollectionRevision = 1
		return out, nil
	}
}

func (s *Service) Register(registry *runtime.Registry) error {
	if s.Store == nil {
		return fmt.Errorf("governance store required")
	}
	s.registry = registry
	for _, register := range []func(*runtime.Registry) error{s.registerGrants, s.registerEvidence, s.registerExtensions, s.registerEvaluation} {
		if err := register(registry); err != nil {
			return err
		}
	}
	for kind, handler := range map[string]runtime.JobHandler{"governance.lease_report": s.continueLeaseReport, "governance.confirmation": s.continueConfirmation, "governance.settle": s.continueSettlement, "governance.defect": s.continueDefect, "governance.prepare": s.continuePrepare, "governance.activate": s.continueActivate, "governance.stop": s.continueStop, "governance.dispose": s.continueDispose, "governance.approval_stop": s.continueApprovalStop, "governance.plan": s.continuePlan, "governance.evaluation": s.continueEvaluation, "governance.eval_cancel": s.continueEvaluationCancel, "governance.exposure": s.continueExposure} {
		if err := registry.RegisterJob(kind, handler); err != nil {
			return err
		}
	}
	return nil
}
