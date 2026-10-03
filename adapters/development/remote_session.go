package development

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/runtime"
)

func (g remoteAgentAuthority) RemoteSessionBindings() []collaboration.RemoteSessionBinding {
	if g.a.Config.RemoteAgent == nil {
		return nil
	}
	return append([]collaboration.RemoteSessionBinding{}, g.a.Config.RemoteAgent.Sessions...)
}
func (g remoteAgentAuthority) CreateRemoteSessionTx(ctx context.Context, tx runtime.Tx, actor runtime.Auth, c api.Command, in interaction.CreateSessionInput) (interaction.SessionOutput, error) {
	if tx.Scope() != g.a.Scope || g.a.Interaction == nil {
		return interaction.SessionOutput{}, api.E("unsupported", "remote_session_authority_unconfigured")
	}
	if err := currentCredentialTx(ctx, tx, actor); err != nil {
		return interaction.SessionOutput{}, err
	}
	return g.a.Interaction.CreateSessionTx(ctx, tx, actor, c, in)
}

var _ collaboration.RemoteSessionAuthority = remoteAgentAuthority{}

func remoteAgentSessionContracts(c Config) []api.MethodContract {
	if c.RemoteAgent == nil || len(c.RemoteAgent.Sessions) == 0 {
		return nil
	}
	return append(collaboration.RemoteSessionContracts(), collaboration.RemoteChildTransferContracts()...)
}

func validateRemoteSessions(c Config) error {
	if c.RemoteAgent == nil {
		return nil
	}
	if len(c.RemoteAgent.Sessions) > 64 {
		return api.E("invalid_request", "remote_session_binding_limit")
	}
	seen := map[string]bool{}
	for _, b := range c.RemoteAgent.Sessions {
		if api.ValidateRecord("ComponentRef", b.ProfileRef) != nil || api.ValidateRecord("ComponentRef", b.SessionConfigRef) != nil || api.ValidateRecord("ObjectRef", b.AgentBindingRef) != nil || api.ValidateRecord("ComponentRef", b.InstallLockRef) != nil || api.ValidateRecord("ContentRef", b.AccessScopeRef) != nil || b.AccessScopeRef.TenantID != c.TenantID {
			return api.E("invalid_request", "remote_session_binding_invalid")
		}
		key, _ := api.Digest(b)
		if seen[key] {
			return api.E("invalid_request", "remote_session_binding_duplicate")
		}
		seen[key] = true
		found := false
		for _, p := range c.RemoteAgent.Profiles {
			if p.ProfileRef == b.ProfileRef && p.Values.AgentBindingRef == b.AgentBindingRef && p.Values.InstallLockRef == b.InstallLockRef && b.AccessScopeRef.OwnerID == p.Values.ParentOwnerID {
				found = true
			}
		}
		if !found {
			return api.E("forbidden", "remote_session_binding_unpaired")
		}
	}
	return nil
}

func (a *App) isOriginalRemoteInputSubmission(ctx context.Context, scope runtime.Scope, actual api.Task, source api.SourceEvidence) (bool, error) {
	if a.RemoteAgent == nil {
		return false, nil
	}
	original, err := a.RemoteAgent.IsDelegationSubmission(ctx, scope, actual, source)
	if err != nil || original {
		return original, err
	}
	return a.RemoteAgent.IsOriginalChildTransferSubmission(ctx, scope, actual, source)
}
