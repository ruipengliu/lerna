package interaction_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/runtime"
)

func TestSessionBranchChangesFutureContextWithoutCreatingTask(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "interaction.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: api.NewID("subject"), CredentialGeneration: 1}
	s, err := interaction.New(interaction.Config{DiscoveryOwnerID: scope.OwnerID}, interaction.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	registry := runtime.NewRegistry()
	if err = s.Register(registry); err != nil {
		t.Fatal(err)
	}
	d := runtime.Dispatcher{Store: store, OwnerID: scope.OwnerID, Registry: registry}
	config := api.ComponentRef{ComponentID: api.NewID("config"), Version: "1.0.0", Digest: api.Hash([]byte("session-test"))}
	session, branch := api.NewID("session"), api.NewID("branch")
	command := func(method, target string, revision *uint64, input any) api.Receipt {
		t.Helper()
		c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: api.NewID("command"), Method: method, TargetID: target, ExpectedRevision: revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(input)}
		r, err := d.Command(ctx, auth, api.Raw(c))
		if err != nil || r.Stage != "applied" {
			t.Fatalf("%s %+v %v", method, r, err)
		}
		return r
	}
	command("session.create", scope.OwnerID, nil, interaction.CreateSessionInput{SessionID: session, DefaultBranchID: branch, ConfigRef: config})
	var view interaction.SessionView
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, QueryID: api.NewID("query"), Method: "session.read", TargetID: session, Payload: api.Raw(interaction.ReadInput{})}
	raw, err := d.Query(ctx, auth, api.Raw(q))
	if err != nil {
		t.Fatal(err)
	}
	if err = api.Decode(raw, &view); err != nil {
		t.Fatal(err)
	}
	if view.Session.State != "open" || len(view.Branches) != 1 || view.Sequence != 0 {
		t.Fatalf("unexpected creation: %+v", view)
	}
	newBranch := api.NewID("branch")
	command("session.branch.create", session, nil, interaction.CreateBranchInput{BranchID: newBranch, SourceBranchRef: scope.Ref(branch, 1), ExpectedSourceRevision: 1, ConfigRef: config})
	revision := uint64(2)
	command("session.branch.select", session, &revision, interaction.SelectBranchInput{BranchID: newBranch})
	raw, err = d.Query(ctx, auth, api.Raw(q))
	if err != nil {
		t.Fatal(err)
	}
	if err = api.Decode(raw, &view); err != nil {
		t.Fatal(err)
	}
	if view.Session.DefaultBranchID != newBranch || view.Sequence != 0 || len(view.Branches) != 2 {
		t.Fatalf("branch created work or lost origin: %+v", view)
	}
	works, _, err := store.Claim(ctx, scope, api.NewID("boot"), []string{interaction.JobDispatch}, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 0 {
		t.Fatalf("context branch created background work: %+v", works)
	}
	other := auth
	other.SubjectID = api.NewID("subject")
	if _, err = d.Query(ctx, other, api.Raw(q)); !api.IsCode(err, "forbidden") {
		t.Fatalf("session crossed subject: %v", err)
	}
}
