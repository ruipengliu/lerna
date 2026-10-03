package wss

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
	"io"
	"net/http"
)

type ContentUploader interface {
	ReceiveTransferBytes(context.Context, runtime.Scope, runtime.Auth, string, []byte) (memory.TransferStatus, error)
}
type DevelopmentConfiguration struct {
	TenantID              string             `json:"tenant_id"`
	ContentPolicyRef      api.ComponentRef   `json:"content_policy_ref"`
	TaskPolicyRef         api.ComponentRef   `json:"task_policy_ref"`
	Budget                []api.Amount       `json:"budget"`
	GoalSchema            api.Schema         `json:"goal_schema"`
	RetentionSeconds      uint64             `json:"retention_seconds"`
	TaskDeadlineSeconds   uint64             `json:"task_deadline_seconds"`
	ApplicationBindingRef *api.ObjectRef     `json:"application_binding_ref,omitempty"`
	ApplicationEvents     []DevelopmentEvent `json:"application_events,omitempty"`
}

type DevelopmentEvent struct {
	Name             string     `json:"name"`
	Schema           api.Schema `json:"schema"`
	RequiresRendered bool       `json:"requires_rendered"`
}

func (s *Server) currentSession(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && !s.allowedOrigin(r) {
		problem(w, api.E("forbidden", "origin_mismatch"))
		return
	}
	a, e := s.authenticate(r)
	if e != nil {
		problem(w, e)
		return
	}
	csrf, e := s.config.Identity.CurrentSession(r.Context(), r, a)
	if e != nil {
		problem(w, e)
		return
	}
	write(w, 200, struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrf_token"`
	}{true, csrf})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	a, e := s.authenticate(r)
	if e != nil {
		problem(w, e)
		return
	}
	if !s.allowedOrigin(r) {
		problem(w, api.E("forbidden", "origin_mismatch"))
		return
	}
	if e = s.config.Identity.CheckCSRF(r.Context(), r, a); e != nil {
		problem(w, e)
		return
	}
	if e = s.config.Identity.Logout(r.Context(), r, a); e != nil {
		problem(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "harness_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: !s.config.AllowInsecureLoopback, SameSite: http.SameSiteStrictMode})
	write(w, 200, struct {
		Authenticated bool `json:"authenticated"`
	}{false})
}
func (s *Server) development(w http.ResponseWriter, r *http.Request) {
	a, e := s.authenticate(r)
	if e != nil {
		problem(w, e)
		return
	}
	if s.config.Development == nil || s.config.Development.TenantID != a.TenantID {
		problem(w, api.E("unsupported", "development_configuration_disabled"))
		return
	}
	write(w, 200, s.config.Development)
}
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	a, e := s.authenticate(r)
	if e != nil {
		problem(w, e)
		return
	}
	if !s.allowedOrigin(r) {
		problem(w, api.E("forbidden", "origin_mismatch"))
		return
	}
	if e = s.config.Identity.CheckCSRF(r.Context(), r, a); e != nil {
		problem(w, e)
		return
	}
	if s.config.Uploader == nil {
		problem(w, api.E("unsupported", "content_transfer_not_supported"))
		return
	}
	id := r.PathValue("transfer_id")
	if !api.ValidID(id) {
		problem(w, api.E("invalid_request", "invalid_transfer_id"))
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(memory.MaxContentBytes)))
	if e != nil {
		problem(w, api.E("invalid_request", "content_too_large"))
		return
	}
	scope := runtime.Scope{TenantID: a.TenantID, OwnerID: s.config.OwnerID, DatabaseID: s.config.Store.ID()}
	out, e := s.config.Uploader.ReceiveTransferBytes(r.Context(), scope, a, id, body)
	if e != nil {
		problem(w, e)
		return
	}
	write(w, 200, out)
}
