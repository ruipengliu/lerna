package platform_test

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type identityReadFaultStore struct {
	runtime.Store
	err error
	ns  string
}

func (s *identityReadFaultStore) Read(ctx context.Context, scope runtime.Scope, ns, id string, version uint64, out any) (uint64, error) {
	if s.err != nil && ns == s.ns {
		return 0, s.err
	}
	return s.Store.Read(ctx, scope, ns, id, version, out)
}

func TestOriginalBearerAndCookieAuthenticationPreserveIdentityReadFailures(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "identity.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store := &identityReadFaultStore{Store: s}
	a := runtime.Auth{TenantID: api.NewID("tenant"), SubjectID: api.NewID("subject"), CredentialGeneration: 3, Roles: []string{"browser"}}
	token := "fixed local identity fault fixture token"
	i := &platform.DevIdentity{Store: store, OwnerID: api.NewID("owner"), SessionTTL: time.Minute, Principals: []platform.Principal{{Auth: a, TokenHash: api.Hash([]byte(token))}}}
	if err = i.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	_, session, _, err := i.Login(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	bearer, _ := http.NewRequest("GET", "https://local.invalid/ws", nil)
	bearer.Header.Set("Authorization", "Bearer "+token)
	cookie, _ := http.NewRequest("GET", "https://local.invalid/ws", nil)
	cookie.AddCookie(&http.Cookie{Name: "harness_session", Value: session})
	for _, fault := range []error{api.E("dependency_unavailable", "original_identity_database_unavailable"), context.DeadlineExceeded, errors.New("original native identity database fault")} {
		store.err, store.ns = fault, "platform.credentials"
		for _, r := range []*http.Request{bearer, cookie} {
			if _, err := i.Authenticate(ctx, r); !errors.Is(err, fault) {
				t.Fatalf("credential read failure relabelled: %v; want %v", err, fault)
			}
		}
		store.ns = "platform.sessions"
		if _, err := i.Authenticate(ctx, cookie); !errors.Is(err, fault) {
			t.Fatalf("original cookie lookup lost cause: %v", err)
		}
	}
	store.err = nil
	for _, r := range []*http.Request{bearer, cookie} {
		if got, err := i.Authenticate(ctx, r); err != nil || !api.Equal(got, a) {
			t.Fatalf("original credentials could not recover %+v %v", got, err)
		}
	}
	if err = i.Revoke(ctx, a); err != nil {
		t.Fatal(err)
	}
	for _, r := range []*http.Request{bearer, cookie} {
		if _, err := i.Authenticate(ctx, r); !api.IsCode(err, "forbidden") {
			t.Fatalf("real revocation was admitted: %v", err)
		}
	}
}
