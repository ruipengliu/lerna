package development

import (
	"context"
	"sort"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type grantMetadataGate struct{}

func (g grantMetadataGate) VisibilityTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth) (string, error) {
	if err := currentCredentialTx(ctx, tx, auth); err != nil {
		return "", err
	}
	var current currentCredential
	if _, err := tx.Get(ctx, "platform.credentials", auth.SubjectID, &current); err != nil {
		return "", err
	}
	actual := append([]string{}, current.Roles...)
	claimed := append([]string{}, auth.Roles...)
	sort.Strings(actual)
	sort.Strings(claimed)
	if current.SubjectID != auth.SubjectID || !api.Equal(actual, claimed) {
		return "", api.E("forbidden", "credential_roles_changed")
	}
	current.Roles = actual
	return api.Digest(current)
}
