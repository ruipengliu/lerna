package development

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 故障只发生在真实接收方已耐久接纳后、网络回执到达父方前。
func TestConfiguredRemoteSessionLostReplyRecoversOriginalCommand(t *testing.T) {
	verifyConfiguredRemoteSessionLostReplyRecoversOriginalCommand(t, "sqlite")
}

func verifyConfiguredRemoteSessionLostReplyRecoversOriginalCommand(t *testing.T, parentDriver string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a, b, p := configuredAgentPairWithParentDriver(t, parentDriver)
	_, _ = configureOriginalRemoteSession(ctx, t, a, b, p)
	binding := a.config.RemoteAgent.Sessions[0]
	baseline, err := b.app.Interaction.ListSessions(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, api.ListInput{Limit: 100})
	if err != nil || !baseline.Exhausted || baseline.Partial || len(baseline.Gaps) != 0 || baseline.NextCursor != "" {
		t.Fatal("original public Session baseline incomplete")
	}
	childID := api.NewID("child")
	deadline := api.Time(time.Now().Add(time.Minute))
	create := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "child.create", TargetID: childID, ExpiresAt: deadline, Payload: api.Raw(task.ChildCreateInput{ChildID: childID, SessionOwnerID: b.app.Scope.OwnerID, SessionConfigRef: binding.SessionConfigRef, AgentBindingRef: binding.AgentBindingRef, InstallLockRef: binding.InstallLockRef, AccessScopeRef: binding.AccessScopeRef, PrepareDeadline: deadline})}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(create))
	if err != nil || receipt.Stage != "accepted" {
		t.Fatalf("original creation: %s %v", receipt.Stage, err)
	}
	original, err := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, childID)
	if err != nil {
		t.Fatal(err)
	}
	oldHandler := b.server.Config.Handler
	var dropped atomic.Bool
	failures := make(chan error, 1)
	b.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(io.LimitReader(r.Body, api.MaxJSONBytes+1))
		if readErr != nil {
			failures <- readErr
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var frame struct {
			Kind    string          `json:"kind"`
			Payload json.RawMessage `json:"payload"`
		}
		var cmd api.Command
		if api.Decode(body, &frame) != nil || frame.Kind != "command" || api.Decode(frame.Payload, &cmd) != nil || cmd.Method != "collaboration.session.create" || cmd.CommandID != original.SessionCommandRef.ObjectID || !dropped.CompareAndSwap(false, true) {
			oldHandler.ServeHTTP(w, r)
			return
		}
		recorded := httptest.NewRecorder()
		oldHandler.ServeHTTP(recorded, r)
		stored, lookupErr := b.app.Store.LookupCommand(r.Context(), b.app.Scope, cmd.CommandID)
		if lookupErr != nil || stored.Receipt.Stage != "accepted" {
			failures <- api.E("invalid_state", "fault_did_not_follow_durable_original_acceptance")
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			failures <- api.E("unsupported", "fault_transport_cannot_drop_reply")
			return
		}
		connection, _, hijackErr := hijacker.Hijack()
		if hijackErr != nil {
			failures <- hijackErr
			return
		}
		_ = connection.Close()
	})
	works, status, err := a.app.Store.Claim(ctx, a.app.Scope, api.NewID("boot"), []string{task.JobChildPrepare}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatal("original preparation claim unavailable")
	}
	handler, ok := a.app.Registry.Job(task.JobChildPrepare)
	if !ok {
		t.Fatal("original preparation handler missing")
	}
	observedErr := handler(ctx, a.app.Store, a.app.Scope, works[0])
	if !dropped.Load() {
		t.Fatal("original reply was not dropped")
	}
	select {
	case failure := <-failures:
		t.Fatal(failure)
	default:
	}
	// 首次丢回复允许原Job返回传输错误；不把它伪造为业务拒绝。
	t.Logf("original accepted Session reply lost; parent handler error=%v", observedErr)
	before, err := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, childID)
	if err != nil || before.State != "preparing" || before.SessionCommandRef != original.SessionCommandRef || before.PrepareDeadline != deadline || before.ChildSessionRef != nil {
		t.Fatal("unknown creation replaced original responsibility")
	}
	if len(a.app.remoteAgents.journals) != 1 {
		t.Fatal("test requires exact configured peer journal")
	}
	entry, err := a.app.remoteAgents.journals[0].Read(ctx, original.SessionCommandRef.ObjectID)
	if err != nil || entry.Command.CommandID != original.SessionCommandRef.ObjectID || entry.Command.Method != "collaboration.session.create" || entry.Command.ExpiresAt != deadline || entry.Receipt != nil {
		t.Fatal("unknown original SDK journal identity changed")
	}
	a.reopenOriginal(t)
	b.reopenOriginal(t)
	// 原Claim尚存，不能发明新Job/扩大租期；用原Work沿真实handler恢复。
	handler, ok = a.app.Registry.Job(task.JobChildPrepare)
	if !ok {
		t.Fatal("reopened original handler missing")
	}
	if err = handler(ctx, a.app.Store, a.app.Scope, works[0]); err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		configuredAgentStep(ctx, t, b, collaboration.JobRemoteSessionCreate)
		configuredAgentStep(ctx, t, a, task.JobChildPrepare)
		actual, readErr := a.app.Task.ChildRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, childID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if actual.State == "open" && actual.ChildSessionRef != nil {
			if actual.SessionCommandRef != original.SessionCommandRef || actual.PrepareDeadline != deadline {
				t.Fatal("recovery refreshed original creation identity")
			}
			recovered, readErr := a.app.remoteAgents.journals[0].Read(ctx, original.SessionCommandRef.ObjectID)
			if readErr != nil || !api.Equal(recovered.Command, entry.Command) || recovered.MethodSchemaDigest != entry.MethodSchemaDigest || recovered.SchemaDigest != entry.SchemaDigest || recovered.Receipt == nil || recovered.Receipt.Stage != "applied" {
				t.Fatal("recovery changed original journal contract")
			}
			sessions, listErr := b.app.Interaction.ListSessions(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, api.ListInput{Limit: 100})
			if listErr != nil || !sessions.Exhausted || sessions.Partial || len(sessions.Gaps) != 0 || sessions.NextCursor != "" || len(sessions.Items) != len(baseline.Items)+1 {
				t.Fatal("recovered original Session not unique and complete")
			}
			count := 0
			baselineRefs := map[string]api.Session{}
			for _, session := range baseline.Items {
				baselineRefs[session.SessionID] = session
			}
			for _, session := range sessions.Items {
				if session.SessionID == actual.ChildSessionRef.ObjectID {
					count++
					continue
				}
				old, ok := baselineRefs[session.SessionID]
				if !ok || !api.Equal(old, session) {
					t.Fatal("recovery changed another Session")
				}
			}
			if count != 1 {
				t.Fatal("recovery created another original Session")
			}
			replay, replayErr := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(create))
			if replayErr != nil || replay.Stage != "applied" {
				t.Fatal("original child.create receipt did not settle")
			}
			t.Logf("original child=%s session_command=%s session=%s dropped=true original_journal_applied=true", childID, original.SessionCommandRef.ObjectID, actual.ChildSessionRef.ObjectID)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("original unknown Session did not recover within original bound")
}
