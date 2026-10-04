package interaction_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/runtime"
)

// 夹具明确装配纯 Tx 当前主体端口；凭据由真实 DevIdentity 初始化，重开继续读取原记录。
type currentSubjectGate struct{}

func (currentSubjectGate) CheckSubjectTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth) error {
	var current struct {
		Generation uint64   `json:"generation"`
		State      string   `json:"state"`
		Roles      []string `json:"roles"`
	}
	if _, err := tx.Get(ctx, "platform.credentials", auth.SubjectID, &current); err != nil {
		return err
	}
	if current.State != "active" || current.Generation != auth.CredentialGeneration {
		return api.E("forbidden", "credential_revoked")
	}
	for _, role := range auth.Roles {
		found := false
		for _, actual := range current.Roles {
			found = found || actual == role
		}
		if !found {
			return api.E("forbidden", "role_changed")
		}
	}
	return nil
}

func installCurrentSubject(t *testing.T, store runtime.Store, scope runtime.Scope, auth runtime.Auth) {
	t.Helper()
	identity := platform.DevIdentity{Store: store, OwnerID: scope.OwnerID, SessionTTL: time.Hour, Principals: []platform.Principal{{Auth: auth, TokenHash: api.Hash([]byte("explicit-interaction-fixture-identity"))}}}
	if err := identity.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSessionDisclosureWithoutCurrentAuthorityReturnsNoData(t *testing.T) {
	f := newApplication(t)
	if _, err := f.s.ReadSession(f.ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{}); err != nil {
		t.Fatalf("explicit current authority rejected original session: %v", err)
	}
	ports := f.appPorts
	ports.SubjectGate = nil
	service, err := interaction.New(f.appConfig, ports)
	if err != nil {
		t.Fatal(err)
	}
	checkUnavailable := func(err error) {
		t.Helper()
		var refusal *api.Error
		if !errors.As(err, &refusal) || refusal.Code != "dependency_unavailable" || refusal.Reason != "current_subject_authority_unavailable" {
			t.Fatalf("missing current authority did not fail closed: %v", err)
		}
	}
	view, err := service.ReadSession(f.ctx, f.store, f.scope, f.auth, f.session, interaction.ReadInput{})
	checkUnavailable(err)
	if view.Session.SessionID != "" || len(view.Branches) != 0 {
		t.Fatal("rejected session read disclosed records")
	}
	page, err := service.ListMessages(f.ctx, f.store, f.scope, f.auth, f.session, api.ListInput{Limit: 10})
	checkUnavailable(err)
	if len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatal("rejected message page disclosed records or a cursor")
	}
}
