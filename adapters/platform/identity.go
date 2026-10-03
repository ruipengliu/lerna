package platform

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type Principal struct {
	Auth      runtime.Auth
	TokenHash string
}
type credential struct {
	Revision   uint64   `json:"revision"`
	SubjectID  string   `json:"subject_id"`
	Generation uint64   `json:"generation"`
	State      string   `json:"state"`
	Roles      []string `json:"roles"`
}
type browserSession struct {
	Revision  uint64       `json:"revision"`
	Auth      runtime.Auth `json:"auth"`
	CSRFHash  string       `json:"csrf_hash"`
	CSRFToken string       `json:"csrf_token"`
	ExpiresAt string       `json:"expires_at"`
	State     string       `json:"state"`
}
type connectionHolder struct {
	ID        string `json:"id"`
	ServiceID string `json:"service_id"`
	BootID    string `json:"boot_id"`
	ExpiresAt string `json:"expires_at"`
}
type connectionQuota struct {
	Revision uint64             `json:"revision"`
	Holders  []connectionHolder `json:"holders"`
}

// IdentityProvider 允许公司身份系统接入；开发 opaque token仅在显式dev装配启用。
type IdentityProvider interface {
	Authenticate(context.Context, *http.Request) (runtime.Auth, error)
	CheckCurrent(context.Context, runtime.Auth) error
}
type DevIdentity struct {
	Store      runtime.Store
	OwnerID    string
	Principals []Principal
	SessionTTL time.Duration
	BootID     string
}

func (i *DevIdentity) scope(a runtime.Auth) runtime.Scope {
	return runtime.Scope{TenantID: a.TenantID, OwnerID: i.OwnerID, DatabaseID: i.Store.ID()}
}

// Initialize 只由管理/开发setup入口调用。已有撤权或更高代次绝不被默认配置覆盖。
func (i *DevIdentity) Initialize(ctx context.Context) error {
	if i.SessionTTL <= 0 || i.SessionTTL > 24*time.Hour || len(i.Principals) < 1 || len(i.Principals) > 100 {
		return api.E("invalid_request", "invalid_identity_configuration")
	}
	if i.BootID == "" {
		i.BootID = api.NewID("boot")
	}
	for _, p := range i.Principals {
		if !api.ValidID(p.Auth.SubjectID) || !api.ValidID(p.Auth.TenantID) || p.Auth.CredentialGeneration == 0 || len(p.TokenHash) != 71 {
			return api.E("invalid_request", "invalid_principal_configuration")
		}
		status, e := i.Store.Within(ctx, i.scope(p.Auth), []string{"platform"}, func(tx runtime.Tx) error {
			var old credential
			_, e := tx.Get(ctx, "platform.credentials", p.Auth.SubjectID, &old)
			if e == nil {
				return nil
			}
			if !api.IsCode(e, "not_found") {
				return e
			}
			return tx.Create(ctx, "platform.credentials", p.Auth.SubjectID, "", credential{1, p.Auth.SubjectID, p.Auth.CredentialGeneration, "active", p.Auth.Roles})
		})
		if status == runtime.CommitUnknown {
			return runtime.ErrCommitUnknown
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func (i *DevIdentity) token(ctx context.Context, value string) (runtime.Auth, error) {
	digest := api.Hash([]byte(value))
	for _, p := range i.Principals {
		if subtle.ConstantTimeCompare([]byte(digest), []byte(p.TokenHash)) == 1 {
			if e := i.CheckCurrent(ctx, p.Auth); e != nil {
				return runtime.Auth{}, e
			}
			return p.Auth, nil
		}
	}
	return runtime.Auth{}, api.E("forbidden", "invalid_credentials")
}
func (i *DevIdentity) CheckCurrent(ctx context.Context, a runtime.Auth) error {
	var c credential
	_, e := i.Store.Read(ctx, i.scope(a), "platform.credentials", a.SubjectID, 0, &c)
	if e != nil {
		return api.E("forbidden", "credential_unavailable")
	}
	if c.State != "active" || c.Generation != a.CredentialGeneration || !api.Equal(c.Roles, a.Roles) {
		return api.E("forbidden", "credential_revoked")
	}
	return nil
}
func (i *DevIdentity) Authenticate(ctx context.Context, r *http.Request) (runtime.Auth, error) {
	authorization := r.Header.Get("Authorization")
	if strings.HasPrefix(authorization, "Bearer ") {
		return i.token(ctx, strings.TrimPrefix(authorization, "Bearer "))
	}
	cookie, e := r.Cookie("harness_session")
	if e != nil || !api.ValidID(cookie.Value) {
		return runtime.Auth{}, api.E("forbidden", "authentication_required")
	}
	for _, p := range i.Principals {
		var session browserSession
		_, e := i.Store.Read(ctx, i.scope(p.Auth), "platform.sessions", cookie.Value, 0, &session)
		if api.IsCode(e, "not_found") {
			continue
		}
		if e != nil {
			return runtime.Auth{}, api.E("dependency_unavailable", "session_unavailable")
		}
		expiry, e := api.ParseTime(session.ExpiresAt)
		if e != nil || !time.Now().Before(expiry) || session.State == "closed" {
			return runtime.Auth{}, api.E("expired", "session_expired")
		}
		if e = i.CheckCurrent(ctx, session.Auth); e != nil {
			return runtime.Auth{}, e
		}
		return session.Auth, nil
	}
	return runtime.Auth{}, api.E("forbidden", "session_not_found")
}
func (i *DevIdentity) Login(ctx context.Context, token string) (runtime.Auth, string, string, error) {
	a, e := i.token(ctx, token)
	if e != nil {
		return a, "", "", e
	}
	id, csrf := api.NewID("session"), api.NewID("csrf")
	status, e := i.Store.Within(ctx, i.scope(a), []string{"platform"}, func(tx runtime.Tx) error {
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		return tx.Create(ctx, "platform.sessions", id, a.SubjectID, browserSession{Revision: 1, Auth: a, CSRFHash: api.Hash([]byte(csrf)), CSRFToken: csrf, ExpiresAt: api.Time(now.Add(i.SessionTTL)), State: "open"})
	})
	if status == runtime.CommitUnknown {
		return a, "", "", runtime.ErrCommitUnknown
	}
	return a, id, csrf, e
}
func (i *DevIdentity) CheckCSRF(ctx context.Context, r *http.Request, a runtime.Auth) error {
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return nil
	}
	c, e := r.Cookie("harness_session")
	if e != nil {
		return api.E("forbidden", "csrf_required")
	}
	var s browserSession
	if _, e = i.Store.Read(ctx, i.scope(a), "platform.sessions", c.Value, 0, &s); e != nil {
		return e
	}
	if s.State == "closed" || !api.Equal(s.Auth, a) {
		return api.E("forbidden", "session_closed")
	}
	if subtle.ConstantTimeCompare([]byte(s.CSRFHash), []byte(api.Hash([]byte(r.Header.Get("X-CSRF-Token"))))) != 1 {
		return api.E("forbidden", "csrf_mismatch")
	}
	return nil
}

// CurrentSession 只返回已认证 HttpOnly 会话的 CSRF nonce，刷新不会换业务身份。
func (i *DevIdentity) CurrentSession(ctx context.Context, r *http.Request, a runtime.Auth) (string, error) {
	c, e := r.Cookie("harness_session")
	if e != nil {
		return "", api.E("forbidden", "browser_session_required")
	}
	var s browserSession
	if _, e = i.Store.Read(ctx, i.scope(a), "platform.sessions", c.Value, 0, &s); e != nil {
		return "", e
	}
	if !api.Equal(a, s.Auth) || s.State == "closed" || s.CSRFToken == "" {
		return "", api.E("forbidden", "session_unavailable")
	}
	return s.CSRFToken, nil
}

// Logout 保留原会话墓碑；它不改变 Task 控制或服务器工作责任。
func (i *DevIdentity) Logout(ctx context.Context, r *http.Request, a runtime.Auth) error {
	c, e := r.Cookie("harness_session")
	if e != nil {
		return api.E("forbidden", "browser_session_required")
	}
	status, e := i.Store.Within(ctx, i.scope(a), []string{"platform"}, func(tx runtime.Tx) error {
		var s browserSession
		rev, e := tx.Get(ctx, "platform.sessions", c.Value, &s)
		if e != nil {
			return e
		}
		if !api.Equal(a, s.Auth) {
			return api.E("forbidden", "session_identity_mismatch")
		}
		if s.State == "closed" {
			return nil
		}
		s.State = "closed"
		s.Revision++
		return tx.Put(ctx, "platform.sessions", c.Value, rev, s)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return e
}
func (i *DevIdentity) Revoke(ctx context.Context, a runtime.Auth) error {
	status, e := i.Store.Within(ctx, i.scope(a), []string{"platform"}, func(tx runtime.Tx) error {
		var c credential
		rev, e := tx.Get(ctx, "platform.credentials", a.SubjectID, &c)
		if e != nil {
			return e
		}
		if c.State == "revoked" {
			return nil
		}
		c.Revision++
		c.Generation++
		c.State = "revoked"
		return tx.Put(ctx, "platform.credentials", a.SubjectID, rev, c)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return e
}
func (i *DevIdentity) AcquireConnection(ctx context.Context, a runtime.Auth, service, id string) error {
	return i.updateConnection(ctx, a, service, id, "acquire")
}
func (i *DevIdentity) RefreshConnection(ctx context.Context, a runtime.Auth, service, id string) error {
	return i.updateConnection(ctx, a, service, id, "refresh")
}
func (i *DevIdentity) ReleaseConnection(ctx context.Context, a runtime.Auth, service, id string) error {
	return i.updateConnection(ctx, a, service, id, "release")
}
func (i *DevIdentity) updateConnection(ctx context.Context, a runtime.Auth, service, id, action string) error {
	status, e := i.Store.Within(ctx, i.scope(a), []string{"platform"}, func(tx runtime.Tx) error {
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		var q connectionQuota
		rev, e := tx.Get(ctx, "platform.connection_quotas", a.SubjectID, &q)
		if e != nil && !api.IsCode(e, "not_found") {
			return e
		}
		if rev == 0 {
			q = connectionQuota{1, []connectionHolder{}}
		}
		holders := []connectionHolder{}
		found := false
		serviceCount := 0
		for _, h := range q.Holders {
			until, e := api.ParseTime(h.ExpiresAt)
			if e != nil {
				return e
			}
			if h.ID == id {
				if h.ServiceID != service || h.BootID != i.BootID {
					return api.E("forbidden", "connection_binding_mismatch")
				}
				found = true
				if action == "release" {
					continue
				}
				if action == "refresh" && !now.Before(until) {
					return api.E("expired", "connection_lease_expired")
				}
				h.ExpiresAt = api.Time(now.Add(90 * time.Second))
			} else if !now.Before(until) {
				continue
			}
			if h.ServiceID == service {
				serviceCount++
			}
			holders = append(holders, h)
		}
		if action == "refresh" && !found {
			return api.E("forbidden", "connection_binding_missing")
		}
		if action == "acquire" && !found {
			if len(holders) >= 16 || serviceCount >= 2 {
				return api.E("overloaded", "connection_quota_exceeded")
			}
			holders = append(holders, connectionHolder{id, service, i.BootID, api.Time(now.Add(90 * time.Second))})
		}
		q.Holders = holders
		if rev == 0 {
			return tx.Create(ctx, "platform.connection_quotas", a.SubjectID, "", q)
		}
		q.Revision++
		return tx.Put(ctx, "platform.connection_quotas", a.SubjectID, rev, q)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return e
}

var _ IdentityProvider = (*DevIdentity)(nil)
