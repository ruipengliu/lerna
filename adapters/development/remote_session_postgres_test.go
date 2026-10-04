package development

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
)

// PG父/SQLite子沿原创建命令、准确配置和token恢复，不生成第二会话。
func TestConfiguredRemoteSessionPostgresParentKeepsOriginalIdentityAndConfiguration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a, b, p := configuredAgentPairWithParentDriver(t, "postgres")
	baseline, err := b.app.Interaction.ListSessions(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, api.ListInput{Limit: 100})
	if err != nil || !baseline.Exhausted || baseline.Partial || len(baseline.Gaps) != 0 {
		t.Fatal("original receiver Session baseline incomplete", err)
	}
	handle, create := configureOriginalRemoteSession(ctx, t, a, b, p)
	receiver, err := b.app.Store.LookupCommand(ctx, b.app.Scope, handle.SessionCommandRef.ObjectID)
	if err != nil || receiver.Receipt.Stage != "applied" || receiver.Command.Method != "collaboration.session.create" || receiver.Command.CommandID != handle.SessionCommandRef.ObjectID {
		t.Fatalf("genuine original Session command: stage=%s error=%v", receiver.Receipt.Stage, err)
	}
	var packet collaboration.RemoteSessionPacket
	if err = api.Decode(receiver.Command.Payload, &packet); err != nil || packet.Command.Method != "session.create" || packet.Command.CommandID != receiver.Command.CommandID || packet.Command.ExpiresAt != receiver.Command.ExpiresAt || packet.Binding != a.config.RemoteAgent.Sessions[0] {
		t.Fatal("original Session payload/configuration/TTL changed", err)
	}
	for _, endpoint := range []*configuredAgentEndpoint{a, b} {
		endpoint.reopenOriginal(t)
	}
	reopened, err := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, handle.ChildID)
	if err != nil || !api.Equal(reopened, handle) {
		t.Fatal("original PG handle changed on exact configuration reopen", err)
	}
	sessions, err := b.app.Interaction.ListSessions(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, api.ListInput{Limit: 100})
	if err != nil || !sessions.Exhausted || sessions.Partial || len(sessions.Gaps) != 0 || len(sessions.Items) != len(baseline.Items)+1 {
		t.Fatal("original public Session collection changed", err)
	}
	originalCount := 0
	for _, session := range sessions.Items {
		if session.SessionID == handle.ChildSessionRef.ObjectID {
			originalCount++
		}
	}
	if originalCount != 1 {
		t.Fatal("original Session command created more than one Session")
	}
	replayed, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(create))
	if err != nil || replayed.Stage != "applied" {
		t.Fatal("original PG creation replay changed", err)
	}
	t.Logf("original parent_owner=%s child_owner=%s create=%s session_command=%s session=%s", a.app.Scope.OwnerID, b.app.Scope.OwnerID, create.CommandID, handle.SessionCommandRef.ObjectID, handle.ChildSessionRef.ObjectID)
}
