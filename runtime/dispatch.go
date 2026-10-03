package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/api"
)

type Outcome struct {
	Stage  string
	Output any
}

func Applied(v any) Outcome  { return Outcome{Stage: "applied", Output: v} }
func Accepted(v any) Outcome { return Outcome{Stage: "accepted", Output: v} }

type Method struct {
	Contract     api.MethodContract
	Participants []string
	Apply        func(context.Context, Tx, Auth, api.Command) (Outcome, error)
	Query        func(context.Context, Store, Scope, Auth, api.Query) (any, error)
	input        *api.Validator
	output       *api.Validator
}
type JobHandler func(context.Context, Store, Scope, Work) error
type Registry struct {
	mu      sync.RWMutex
	methods map[string]Method
	jobs    map[string]JobHandler
}

func NewRegistry() *Registry {
	return &Registry{methods: map[string]Method{}, jobs: map[string]JobHandler{}}
}
func (r *Registry) Register(m Method) error {
	if m.Contract.Name == "" || m.Contract.Owner == "" {
		return fmt.Errorf("method metadata missing")
	}
	if m.Contract.Kind == "command" && m.Apply == nil || m.Contract.Kind == "query" && m.Query == nil {
		return fmt.Errorf("method handler missing")
	}
	var err error
	m.input, err = api.NewValidator(m.Contract.InputSchema)
	if err != nil {
		return fmt.Errorf("%s input: %w", m.Contract.Name, err)
	}
	m.output, err = api.NewValidator(m.Contract.OutputSchema)
	if err != nil {
		return fmt.Errorf("%s output: %w", m.Contract.Name, err)
	}
	d, err := api.Digest([]any{m.Contract.InputSchema, m.Contract.OutputSchema})
	if err != nil {
		return err
	}
	m.Contract.SchemaDigest = d
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.methods[m.Contract.Name]; ok {
		return fmt.Errorf("method already registered: %s", m.Contract.Name)
	}
	r.methods[m.Contract.Name] = m
	return nil
}
func (r *Registry) MustRegister(m Method) {
	if err := r.Register(m); err != nil {
		panic(err)
	}
}
func (r *Registry) RegisterJob(kind string, h JobHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if kind == "" || h == nil {
		return fmt.Errorf("job handler missing")
	}
	if _, ok := r.jobs[kind]; ok {
		return fmt.Errorf("job handler already registered: %s", kind)
	}
	r.jobs[kind] = h
	return nil
}
func (r *Registry) MustRegisterJob(kind string, h JobHandler) {
	if err := r.RegisterJob(kind, h); err != nil {
		panic(err)
	}
}
func (r *Registry) Method(name string) (Method, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.methods[name]
	return m, ok
}
func (r *Registry) Job(kind string) (JobHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.jobs[kind]
	return h, ok
}
func (r *Registry) JobKinds() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a := []string{}
	for k := range r.jobs {
		a = append(a, k)
	}
	sort.Strings(a)
	return a
}
func (r *Registry) Contracts() []api.MethodContract {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a := []api.MethodContract{}
	for _, m := range r.methods {
		a = append(a, m.Contract)
	}
	sort.Slice(a, func(i, j int) bool { return a[i].Name < a[j].Name })
	return a
}

type Dispatcher struct {
	Store    Store
	OwnerID  string
	Registry *Registry
}

func (d *Dispatcher) Scope(auth Auth) Scope {
	return Scope{TenantID: auth.TenantID, OwnerID: d.OwnerID, DatabaseID: d.Store.ID()}
}
func validAuth(a Auth) error {
	if !api.ValidID(a.TenantID) || !api.ValidID(a.SubjectID) || a.CredentialGeneration == 0 {
		return api.E("forbidden", "invalid_identity")
	}
	return nil
}
func checkEnvelope(protocol, profile, service, id, target, owner string) error {
	if protocol != api.Protocol || profile != api.Profile {
		return api.E("unsupported", "profile_not_supported")
	}
	if service != owner {
		return api.E("invalid_request", "wrong_logical_service")
	}
	if !api.ValidID(service) || !api.ValidID(id) || !api.ValidID(target) {
		return api.E("invalid_request", "invalid_identity")
	}
	return nil
}
func (d *Dispatcher) Command(ctx context.Context, auth Auth, raw []byte) (api.Receipt, error) {
	if err := validAuth(auth); err != nil {
		return api.Receipt{}, err
	}
	var c api.Command
	if err := api.Decode(raw, &c); err != nil {
		return api.Receipt{}, err
	}
	if err := checkEnvelope(c.Protocol, c.Profile, c.LogicalServiceID, c.CommandID, c.TargetID, d.OwnerID); err != nil {
		return api.Receipt{}, err
	}
	deadline, err := api.ParseTime(c.ExpiresAt)
	if err != nil {
		return api.Receipt{}, api.E("invalid_request", "invalid_expiry")
	}
	if c.ExpectedRevision != nil && (*c.ExpectedRevision == 0 || *c.ExpectedRevision > api.MaxSafeInteger) {
		return api.Receipt{}, api.E("invalid_request", "invalid_revision")
	}
	canonical, err := api.Canonical(raw)
	if err != nil {
		return api.Receipt{}, err
	}
	digest := api.Hash(canonical)
	m, exists := d.Registry.Method(c.Method)
	parts := []string{}
	if exists {
		parts = m.Participants
	}
	var receipt api.Receipt
	status, err := d.Store.Within(ctx, d.Scope(auth), parts, func(tx Tx) error {
		original, err := tx.LoadCommand(ctx, c.CommandID)
		if err == nil {
			if original.PrincipalID != auth.SubjectID || original.Digest != digest {
				return api.E("idempotency_conflict", "command_input_changed")
			}
			if original.Tombstone {
				return api.E("gone", "receipt_collected")
			}
			receipt = original.Receipt
			return nil
		}
		if !errors.Is(err, ErrNotFound) && !api.IsCode(err, "not_found") {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		receipt = api.Receipt{CommandID: c.CommandID, RequestDigest: digest}
		var businessErr error
		switch {
		case !exists || m.Contract.Kind != "command":
			businessErr = api.E("unsupported", "method_not_supported")
		case !now.Before(deadline):
			businessErr = api.E("expired", "command_expired")
		case m.Contract.CAS && c.ExpectedRevision == nil:
			businessErr = api.E("invalid_request", "expected_revision_required")
		default:
			businessErr = m.input.Validate(c.Payload)
		}
		var result Outcome
		if businessErr == nil {
			businessErr = tx.Savepoint(ctx, func(inner Tx) error {
				var err error
				result, err = m.Apply(ctx, inner, auth, c)
				if err != nil {
					return err
				}
				if result.Stage != "applied" && (result.Stage != "accepted" || !m.Contract.AllowsAccepted) {
					return fmt.Errorf("invalid receipt phase for %s", c.Method)
				}
				if err = m.output.Validate(api.Raw(result.Output)); err != nil {
					return fmt.Errorf("invalid output for %s: %w", c.Method, err)
				}
				return nil
			})
		}
		if businessErr != nil {
			var e *api.Error
			if !errors.As(businessErr, &e) {
				return businessErr
			}
			if e.Code == "dependency_unavailable" || e.Code == "overloaded" || e.Code == "effect_unknown" || e.Code == "accounting_unknown" {
				return businessErr
			}
			receipt.Stage = "rejected"
			receipt.DecidedAt = api.Time(now)
			receipt.Error = e
		} else {
			receipt.Stage = result.Stage
			receipt.Output = api.Raw(result.Output)
			if result.Stage == "accepted" {
				receipt.AcceptedAt = api.Time(now)
			} else {
				receipt.DecidedAt = api.Time(now)
			}
		}
		return tx.SaveCommand(ctx, StoredCommand{Command: c, PrincipalID: auth.SubjectID, Digest: digest, Receipt: receipt})
	})
	if status == CommitUnknown {
		original, lookupErr := d.Store.LookupCommand(ctx, d.Scope(auth), c.CommandID)
		if lookupErr == nil && original.Digest == digest && original.PrincipalID == auth.SubjectID && !original.Tombstone {
			return original.Receipt, nil
		}
		return api.Receipt{}, &api.Error{Code: "dependency_unavailable", Scope: "command", Reason: "commit_unknown", Retry: "query_original", Cause: ErrCommitUnknown}
	}
	if err != nil {
		return api.Receipt{}, err
	}
	return receipt, nil
}
func (d *Dispatcher) Lookup(ctx context.Context, auth Auth, id string) (api.Receipt, error) {
	if err := validAuth(auth); err != nil {
		return api.Receipt{}, err
	}
	original, err := d.Store.LookupCommand(ctx, d.Scope(auth), id)
	if err != nil {
		return api.Receipt{}, err
	}
	if original.PrincipalID != auth.SubjectID {
		return api.Receipt{}, api.E("forbidden", "receipt_redacted")
	}
	if original.Tombstone {
		return api.Receipt{}, api.E("gone", "receipt_collected")
	}
	return original.Receipt, nil
}
func (d *Dispatcher) Query(ctx context.Context, auth Auth, raw []byte) (json.RawMessage, error) {
	if err := validAuth(auth); err != nil {
		return nil, err
	}
	var q api.Query
	if err := api.Decode(raw, &q); err != nil {
		return nil, err
	}
	if err := checkEnvelope(q.Protocol, q.Profile, q.LogicalServiceID, q.QueryID, q.TargetID, d.OwnerID); err != nil {
		return nil, err
	}
	m, ok := d.Registry.Method(q.Method)
	if !ok || m.Contract.Kind != "query" {
		return nil, api.E("unsupported", "method_not_supported")
	}
	if err := m.input.Validate(q.Payload); err != nil {
		return nil, err
	}
	bindings, ok := d.Store.(QueryBindingStore)
	if !ok {
		return nil, api.E("unsupported", "query_identity_store_unavailable")
	}
	if _, status, err := bindings.PruneQueries(ctx, d.Scope(auth), 16); status == CommitUnknown {
		return nil, ErrCommitUnknown
	} else if err != nil {
		return nil, err
	}
	digest, err := api.Digest(q)
	if err != nil {
		return nil, err
	}
	roles := append([]string{}, auth.Roles...)
	sort.Strings(roles)
	rolesDigest, err := api.Digest(roles)
	if err != nil {
		return nil, err
	}
	binding, status, err := bindings.BindQuery(ctx, d.Scope(auth), QueryBindingInput{QueryID: q.QueryID, PrincipalID: auth.SubjectID, CredentialGeneration: auth.CredentialGeneration, RolesDigest: rolesDigest, QueryDigest: digest, TTL: 5 * time.Minute})
	if status == CommitUnknown {
		return nil, ErrCommitUnknown
	}
	if err != nil {
		return nil, err
	}
	ctx = WithQueryBinding(ctx, binding)
	v, err := m.Query(ctx, d.Store, d.Scope(auth), auth, q)
	if err != nil {
		return nil, err
	}
	b := api.Raw(v)
	if err := m.output.Validate(b); err != nil {
		return nil, fmt.Errorf("invalid query output for %s: %w", q.Method, err)
	}
	resultDigest, err := api.Digest(v)
	if err != nil {
		return nil, err
	}
	if _, status, err := bindings.SealQuery(ctx, d.Scope(auth), binding, resultDigest); status == CommitUnknown {
		return nil, ErrCommitUnknown
	} else if err != nil {
		return nil, err
	}
	return b, nil
}

// Decide 推进原 accepted 回执；不同终态决定不能覆盖。
func Decide(ctx context.Context, tx Tx, id string, output any, rejection *api.Error) error {
	old, err := tx.LoadCommand(ctx, id)
	if err != nil {
		return err
	}
	if old.Receipt.Stage != "accepted" {
		if rejection == nil && old.Receipt.Stage == "applied" && api.Equal(old.Receipt.Output, api.Raw(output)) {
			return nil
		}
		return api.E("invalid_state", "command_already_decided")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	old.Receipt.DecidedAt = api.Time(now)
	if rejection != nil {
		old.Receipt.Stage = "rejected"
		old.Receipt.Output = nil
		old.Receipt.Error = rejection
	} else {
		old.Receipt.Stage = "applied"
		old.Receipt.Output = api.Raw(output)
		old.Receipt.Error = nil
	}
	return tx.SaveCommand(ctx, old)
}
