package development

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func TestRemoteParentFactoryClonesOnlyCurrentFlowAndKeepsJobsEmpty(t *testing.T) {
	// 只测纯入口载体，不把无签名的内存样本当成Task/来源授权。
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: api.NewID("db")}
	a := &App{Scope: scope, RemoteAgent: &collaboration.Remote{}}
	flow := runtime.Flow{Kind: "command", Scope: scope}
	current := a.remoteAgentEntryContext(context.Background(), flow)
	proof := collaboration.RemoteAllocationSnapshot{ScopeRevision: 2, Proof: "bounded-in-memory-carrier-sample", Packet: collaboration.RemoteCreateInput{ChildTaskID: api.NewID("task")}}
	var err error
	current, err = collaboration.WithParentScopes(current, []collaboration.RemoteAllocationSnapshot{proof})
	if err != nil {
		t.Fatal(err)
	}
	nested := a.remoteAgentEntryContext(current, runtime.Flow{Kind: "query", Scope: scope})
	older := proof
	older.ScopeRevision = 1
	if _, err = collaboration.WithParentScopes(nested, []collaboration.RemoteAllocationSnapshot{older}); !api.IsCode(err, "revision_conflict") {
		t.Fatalf("nested continuation lost this flow's current parent fact: %v", err)
	}
	for _, child := range []context.Context{
		a.remoteAgentEntryContext(current, runtime.Flow{Kind: "job", Scope: scope}),
		a.remoteAgentEntryContext(context.Background(), flow),
		a.remoteAgentEntryContext(current, runtime.Flow{Kind: "query", Scope: runtime.Scope{TenantID: scope.TenantID, OwnerID: api.NewID("owner"), DatabaseID: api.NewID("db")}}),
	} {
		if _, err = collaboration.WithParentScopes(child, []collaboration.RemoteAllocationSnapshot{older}); err != nil {
			t.Fatalf("independent entry inherited another entry's fact: %v", err)
		}
	}
	newer := proof
	newer.ScopeRevision = 3
	if _, err = collaboration.WithParentScopes(nested, []collaboration.RemoteAllocationSnapshot{newer}); err != nil {
		t.Fatal(err)
	}
	if _, err = collaboration.WithParentScopes(current, []collaboration.RemoteAllocationSnapshot{proof}); err != nil {
		t.Fatalf("nested mutation changed parent carrier: %v", err)
	}
}
