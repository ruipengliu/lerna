package development

import (
	"context"
	"os"
	"strings"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (a *App) forwardCredential(ctx context.Context, auth runtime.Auth) (string, error) {
	if auth.TenantID != a.UserAuth.TenantID || auth.SubjectID != a.UserAuth.SubjectID || auth.CredentialGeneration != a.UserAuth.CredentialGeneration || !api.Equal(auth.Roles, a.UserAuth.Roles) {
		return "", api.E("unsupported", "original_subject_forward_credential_not_configured")
	}
	if err := a.Identity.CheckCurrent(ctx, auth); err != nil {
		return "", err
	}
	value, err := os.ReadFile(a.Config.TokenFile)
	if err != nil {
		return "", api.E("dependency_unavailable", "original_subject_forward_credential_unavailable")
	}
	token := strings.TrimSpace(string(value))
	if api.Hash([]byte(token)) != a.Identity.Principals[0].TokenHash {
		return "", api.E("forbidden", "original_subject_forward_credential_changed")
	}
	return token, nil
}
