package memory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

var Participants = []string{"content", "memory"}

type Service struct {
	Store               runtime.Store
	Objects             ObjectStore
	Authorization       Authorization
	Location            string
	SavingAuthorization SavingAuthorization
	registryOnce        sync.Once
	registry            *runtime.Registry
}

func New(store runtime.Store, objects ObjectStore) *Service {
	return &Service{Store: store, Objects: objects, Location: "local"}
}

func (s *Service) commands() *runtime.Registry {
	s.registryOnce.Do(func() { s.registry = runtime.NewRegistry(); s.Register(s.registry) })
	return s.registry
}

func contentKey(ref api.ContentRef) string {
	return ref.ContentID + ":" + strconv.FormatUint(ref.Version, 10)
}
func policyKey(ref api.ComponentRef) string {
	return ref.ComponentID + ":" + ref.Version + ":" + ref.Digest
}
func semanticID(prefix, value string) string {
	return prefix + "_" + strings.TrimPrefix(api.Hash([]byte(value)), "sha256:")[:32]
}

func NewPolicy(ref api.ComponentRef, values PolicyValues) (Policy, error) {
	digest, err := api.Digest(values)
	if err != nil {
		return Policy{}, err
	}
	if ref.Digest != digest {
		return Policy{}, api.E("invalid_request", "policy_digest_mismatch")
	}
	return Policy{PolicyRef: ref, Values: values, Revision: 1, State: "active"}, nil
}

func policyValid(p Policy) error {
	if err := api.ValidateRecord("ComponentRef", p.PolicyRef); err != nil {
		return err
	}
	if p.Revision != 1 || p.State != "active" || len(p.Values.Subjects) == 0 || len(p.Values.Subjects) > 100 || len(p.Values.Purposes) == 0 || len(p.Values.Purposes) > 100 || len(p.Values.Locations) == 0 || len(p.Values.Locations) > 100 {
		return api.E("invalid_request", "invalid_policy")
	}
	for _, id := range p.Values.Subjects {
		if !api.ValidID(id) {
			return api.E("invalid_request", "invalid_policy_subject")
		}
	}
	if _, err := api.ParseTime(p.Values.RetainUntil); err != nil {
		return api.E("invalid_request", "invalid_policy_retention")
	}
	digest, err := api.Digest(p.Values)
	if err != nil || digest != p.PolicyRef.Digest {
		return api.E("invalid_request", "policy_digest_mismatch")
	}
	for _, values := range [][]string{p.Values.Subjects, p.Values.Purposes, p.Values.Locations} {
		seen := map[string]bool{}
		for _, value := range values {
			if value == "" || len(value) > 128 || seen[value] {
				return api.E("invalid_request", "invalid_policy_scope")
			}
			seen[value] = true
		}
	}
	return nil
}

func (s *Service) within(ctx context.Context, scope runtime.Scope, fn func(runtime.Tx) error) error {
	if s.Store == nil || s.Objects == nil || scope.DatabaseID != s.Store.ID() || !api.ValidID(scope.TenantID) || !api.ValidID(scope.OwnerID) {
		return api.E("dependency_unavailable", "memory_not_configured")
	}
	status, err := s.Store.Within(ctx, scope, Participants, fn)
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}

func checkAuth(scope runtime.Scope, auth runtime.Auth) error {
	if auth.TenantID != scope.TenantID || !api.ValidID(auth.SubjectID) || auth.CredentialGeneration == 0 {
		return api.E("forbidden", "invalid_identity")
	}
	return nil
}

func checkContentRef(scope runtime.Scope, ref api.ContentRef) error {
	if err := api.ValidateRecord("ContentRef", ref); err != nil {
		return err
	}
	if ref.TenantID != scope.TenantID {
		return api.E("forbidden", "reference_scope_mismatch")
	}
	if ref.OwnerID != scope.OwnerID {
		return api.E("dependency_unavailable", "source_authority_unavailable")
	}
	return nil
}

func (s *Service) InstallPolicy(ctx context.Context, scope runtime.Scope, auth runtime.Auth, policy Policy) error {
	return s.within(ctx, scope, func(tx runtime.Tx) error { return s.InstallPolicyTx(ctx, tx, auth, policy) })
}

// InstallPolicyTx 是显式管理入口，不在构造函数或恢复过程中安装默认许可。
func (s *Service) InstallPolicyTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, policy Policy) error {
	if err := checkAuth(tx.Scope(), auth); err != nil {
		return err
	}
	if !auth.HasRole("content_admin") && !auth.HasRole("memory_admin") {
		return api.E("forbidden", "policy_management_required")
	}
	if err := policyValid(policy); err != nil {
		return err
	}
	var old Policy
	_, err := tx.Get(ctx, "content.policies", policyKey(policy.PolicyRef), &old)
	if err == nil {
		if !api.Equal(old.PolicyRef, policy.PolicyRef) || !api.Equal(old.Values, policy.Values) {
			return api.E("idempotency_conflict", "policy_changed")
		}
		return nil
	}
	if !api.IsCode(err, "not_found") {
		return err
	}
	if err = tx.Create(ctx, "content.policies", policyKey(policy.PolicyRef), "", policy); err != nil {
		return err
	}
	head, err := loadHead(ctx, tx)
	if err != nil {
		return err
	}
	head.RegistryVersion++
	head.VisibilityRevision++
	return saveHead(ctx, tx, head)
}

func loadHead(ctx context.Context, tx runtime.Tx) (ChangeHead, error) {
	var head ChangeHead
	_, err := tx.Get(ctx, "memory.heads", tx.Scope().OwnerID, &head)
	if api.IsCode(err, "not_found") {
		head = ChangeHead{Revision: 1, RegistryVersion: 1, VisibilityRevision: 1}
		if err = tx.Create(ctx, "memory.heads", tx.Scope().OwnerID, "", head); err != nil {
			return ChangeHead{}, err
		}
		return head, nil
	}
	return head, err
}

func saveHead(ctx context.Context, tx runtime.Tx, head ChangeHead) error {
	old := head.Revision
	if old >= api.MaxSafeInteger {
		return api.E("overloaded", "revision_exhausted")
	}
	head.Revision++
	return tx.Put(ctx, "memory.heads", tx.Scope().OwnerID, old, head)
}

func (s *Service) policy(ctx context.Context, tx runtime.Tx, ref api.ComponentRef) (Policy, error) {
	var p Policy
	_, err := tx.Get(ctx, "content.policies", policyKey(ref), &p)
	if api.IsCode(err, "not_found") {
		return p, api.E("forbidden", "saving_policy_unregistered")
	}
	if err != nil {
		return p, err
	}
	if !api.Equal(ref, p.PolicyRef) || p.State != "active" {
		return p, api.E("forbidden", "policy_unavailable")
	}
	return p, nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}
func subset(a, b []string) bool {
	for _, v := range a {
		if !contains(b, v) {
			return false
		}
	}
	return true
}

func narrower(next, previous PolicyValues) bool {
	n, ne := api.ParseTime(next.RetainUntil)
	p, pe := api.ParseTime(previous.RetainUntil)
	return ne == nil && pe == nil && !n.After(p) && subset(next.Subjects, previous.Subjects) && subset(next.Purposes, previous.Purposes) && subset(next.Locations, previous.Locations) && (!next.Continuous || previous.Continuous) && (!next.IndependentDerived || previous.IndependentDerived)
}

func (s *Service) allowed(ctx context.Context, tx runtime.Tx, auth runtime.Auth, policyRef api.ComponentRef, purpose, location string, continuous bool) (Policy, error) {
	if err := checkAuth(tx.Scope(), auth); err != nil {
		return Policy{}, err
	}
	p, err := s.policy(ctx, tx, policyRef)
	if err != nil {
		return p, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return p, err
	}
	expiry, _ := api.ParseTime(p.Values.RetainUntil)
	if !now.Before(expiry) {
		return p, api.E("gone", "retention_expired")
	}
	if !contains(p.Values.Subjects, auth.SubjectID) || !contains(p.Values.Purposes, purpose) || !contains(p.Values.Locations, location) || continuous && !p.Values.Continuous {
		return p, api.E("forbidden", "source_forbidden")
	}
	if s.Authorization != nil {
		if _, err = s.Authorization.Check(ctx, tx, auth, policyRef, purpose, location, continuous); err != nil {
			return p, err
		}
	}
	return p, nil
}

func (s *Service) visibility(ctx context.Context, tx runtime.Tx, auth runtime.Auth) (string, error) {
	head, err := loadHead(ctx, tx)
	if err != nil {
		return "", err
	}
	grant := "local_policies"
	if s.Authorization != nil {
		grant, err = s.Authorization.Visibility(ctx, tx, auth)
		if err != nil {
			return "", err
		}
	}
	return api.Digest([]any{head.RegistryVersion, head.VisibilityRevision, auth.SubjectID, auth.CredentialGeneration, grant})
}

func future(ctx context.Context, tx runtime.Tx, value string) (time.Time, error) {
	t, err := api.ParseTime(value)
	if err != nil {
		return t, api.E("invalid_request", "invalid_time")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return t, err
	}
	if !now.Before(t) {
		return t, api.E("gone", "retention_expired")
	}
	return t, nil
}

func compareExpected(expected *uint64, actual uint64) error {
	if expected == nil {
		return api.E("invalid_request", "expected_revision_required")
	}
	if *expected != actual {
		return runtime.ErrConflict
	}
	return nil
}

func validateSources(scope runtime.Scope, sources []api.ContentRef) error {
	if len(sources) > 100 {
		return api.E("invalid_request", "source_limit_exceeded")
	}
	seen := map[string]bool{}
	for _, ref := range sources {
		if err := checkContentRef(scope, ref); err != nil {
			return err
		}
		key := ref.OwnerID + ":" + contentKey(ref)
		if seen[key] {
			return api.E("invalid_request", "duplicate_source")
		}
		seen[key] = true
	}
	return nil
}

func orderedIDs(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}
func isUnavailable(err error) bool {
	return api.IsCode(err, "dependency_unavailable") || errors.Is(err, runtime.ErrCommitUnknown)
}
func parseCursor(cursor, id string) (string, int, error) {
	parts := strings.Split(cursor, ":")
	if len(parts) != 3 || parts[0] != id {
		return "", 0, api.E("invalid_request", "invalid_cursor")
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil || n < 0 {
		return "", 0, api.E("invalid_request", "invalid_cursor")
	}
	return parts[1], n, nil
}
func cursorFor(id, digest string, position int) string {
	return fmt.Sprintf("%s:%s:%d", id, strings.TrimPrefix(digest, "sha256:"), position)
}
