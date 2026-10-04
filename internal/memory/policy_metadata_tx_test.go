package memory_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/development"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 真实宿主提供当前身份与已安装许可，仅精确读故障和可信时间通过批准端口注入。
func TestRegisteredPolicyMetadataKeepsCurrentAuthorityScopeAndOriginalFailures(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			root := t.TempDir()
			cfg, err := development.InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			app, err := development.OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if closeErr := app.Close(); closeErr != nil {
					t.Errorf("close actual policy metadata host: %v", closeErr)
				}
			}()
			missing := app.ContentPolicy.PolicyRef
			missing.ComponentID = api.NewID("policy")
			wrongScope := app.Scope
			wrongScope.OwnerID = api.NewID("owner")
			wrongAuth := app.ServiceAuth
			wrongAuth.TenantID = api.NewID("tenant")
			ordinary := app.ServiceAuth
			ordinary.Roles = []string{"service"}
			expires, _ := api.ParseTime(app.ContentPolicy.Values.RetainUntil)
			expired := expires.Add(time.Second)
			readFault := errors.New("original policy repository read failed")
			clockFault := errors.New("original policy adjudication clock unavailable")
			cases := []struct {
				name         string
				scope        runtime.Scope
				auth         runtime.Auth
				ref          api.ComponentRef
				getError     error
				clockError   error
				now          *time.Time
				nilAuthority bool
				wantFound    bool
				wantCode     string
				wantReason   string
				wantError    error
				wantReads    int
			}{
				{name: "original", scope: app.Scope, auth: app.ServiceAuth, ref: app.ContentPolicy.PolicyRef, wantFound: true, wantReads: 1},
				{name: "exact_absence", scope: app.Scope, auth: app.ServiceAuth, ref: missing, wantReads: 1},
				{name: "wrong_owner_scope", scope: wrongScope, auth: app.ServiceAuth, ref: app.ContentPolicy.PolicyRef, wantCode: "forbidden", wantReason: "policy_scope_mismatch"},
				{name: "wrong_tenant_actor", scope: app.Scope, auth: wrongAuth, ref: app.ContentPolicy.PolicyRef, wantCode: "forbidden", wantReason: "invalid_identity"},
				{name: "caller_role_is_not_management", scope: app.Scope, auth: ordinary, ref: app.ContentPolicy.PolicyRef, wantCode: "forbidden", wantReason: "policy_management_required"},
				{name: "missing_current_authority", scope: app.Scope, auth: app.ServiceAuth, ref: app.ContentPolicy.PolicyRef, nilAuthority: true, wantCode: "dependency_unavailable", wantReason: "policy_authority_not_configured"},
				{name: "expired_original_policy", scope: app.Scope, auth: app.ServiceAuth, ref: app.ContentPolicy.PolicyRef, now: &expired, wantFound: true, wantReads: 1, wantCode: "gone", wantReason: "retention_expired"},
				{name: "raw_repository_failure", scope: app.Scope, auth: app.ServiceAuth, ref: app.ContentPolicy.PolicyRef, getError: readFault, wantError: readFault, wantReads: 1},
				{name: "mixed_missing_and_repository_failure", scope: app.Scope, auth: app.ServiceAuth, ref: app.ContentPolicy.PolicyRef, getError: errors.Join(runtime.ErrNotFound, readFault), wantError: readFault, wantReads: 1},
				{name: "wrapped_missing_is_not_proven_absence", scope: app.Scope, auth: app.ServiceAuth, ref: app.ContentPolicy.PolicyRef, getError: fmt.Errorf("unclassified wrapper: %w", runtime.ErrNotFound), wantError: runtime.ErrNotFound, wantReads: 1},
				{name: "unknown_clock", scope: app.Scope, auth: app.ServiceAuth, ref: app.ContentPolicy.PolicyRef, clockError: clockFault, wantFound: true, wantError: clockFault, wantReads: 1},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					authority := app.Memory.Authorization
					if tc.nilAuthority {
						app.Memory.Authorization = nil
					}
					defer func() { app.Memory.Authorization = authority }()
					reads := 0
					var policy memory.Policy
					var found bool
					_, err := app.Store.Within(ctx, app.Scope, []string{"content", "memory", "platform"}, func(tx runtime.Tx) error {
						var e error
						policy, found, e = app.Memory.RegisteredPolicyTx(ctx, policyMetadataFaultTx{Tx: tx, reads: &reads, getError: tc.getError, clockError: tc.clockError, now: tc.now}, tc.scope, tc.auth, tc.ref)
						return e
					})
					if reads != tc.wantReads || found != tc.wantFound {
						t.Fatalf("metadata entered wrong original authority/read path: reads=%d found=%v error=%v", reads, found, err)
					}
					if tc.wantError != nil {
						if !errors.Is(err, tc.wantError) {
							t.Fatalf("original failure became absence: %v", err)
						}
					} else if tc.wantCode != "" {
						var e *api.Error
						if !errors.As(err, &e) || e.Code != tc.wantCode || e.Reason != tc.wantReason {
							t.Fatalf("original refusal changed: %v", err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
					if err == nil && found && !api.Equal(policy, app.ContentPolicy) {
						t.Fatal("metadata substituted stored policy")
					}
				})
			}
		})
	}
}

type policyMetadataFaultTx struct {
	runtime.Tx
	reads      *int
	getError   error
	clockError error
	now        *time.Time
}

func (tx policyMetadataFaultTx) Get(ctx context.Context, ns, id string, value any) (uint64, error) {
	if ns == "content.policies" {
		*tx.reads++
		if tx.getError != nil {
			return 0, tx.getError
		}
	}
	return tx.Tx.Get(ctx, ns, id, value)
}
func (tx policyMetadataFaultTx) Now(ctx context.Context) (time.Time, error) {
	if tx.clockError != nil {
		return time.Time{}, tx.clockError
	}
	if tx.now != nil {
		return *tx.now, nil
	}
	return tx.Tx.Now(ctx)
}
