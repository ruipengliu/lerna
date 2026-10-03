package collaboration

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/runtime"
)

// RemoteSessionBinding 是宿主事先配对的准确历史范围，不从正文或 Grant 推导。
type RemoteSessionBinding struct {
	ProfileRef       api.ComponentRef `json:"profile_ref"`
	SessionConfigRef api.ComponentRef `json:"session_config_ref"`
	AgentBindingRef  api.ObjectRef    `json:"agent_binding_ref"`
	InstallLockRef   api.ComponentRef `json:"install_lock_ref"`
	AccessScopeRef   api.ContentRef   `json:"access_scope_ref"`
}

// RemoteSessionAuthority 可选；未提供时不接纳可复用远端 Session。
// 只在本库短事务中提交固定原 session.create，不允许任意 SDK Session 命令。
type RemoteSessionAuthority interface {
	RemoteSessionBindings() []RemoteSessionBinding
	CreateRemoteSessionTx(context.Context, runtime.Tx, runtime.Auth, api.Command, interaction.CreateSessionInput) (interaction.SessionOutput, error)
}
