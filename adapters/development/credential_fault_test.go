package development

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type credentialFaultStore struct {
	runtime.Store
	err error
}

func (s *credentialFaultStore) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, participants, func(tx runtime.Tx) error {
		return fn(credentialFaultTx{Tx: tx, store: s})
	})
}

type credentialFaultTx struct {
	runtime.Tx
	store *credentialFaultStore
}

func (tx credentialFaultTx) Get(ctx context.Context, ns, id string, out any) (uint64, error) {
	if ns == "platform.credentials" && tx.store.err != nil {
		return 0, tx.store.err
	}
	return tx.Tx.Get(ctx, ns, id, out)
}

func (tx credentialFaultTx) Savepoint(ctx context.Context, fn func(runtime.Tx) error) error {
	return tx.Tx.Savepoint(ctx, func(inner runtime.Tx) error {
		return fn(credentialFaultTx{Tx: inner, store: tx.store})
	})
}

func TestTaskAdmissionPreservesCredentialPersistenceFaultAndRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root := t.TempDir()
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	a, err := OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	}()
	goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "text/plain", []byte("original plain goal"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := api.NewID("task")
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: "task.submit", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}, RequirementCandidates: []api.RequirementCandidate{}})}
	fault := errors.New("original credential database boundary failed")
	store := &credentialFaultStore{Store: a.Store, err: fault}
	a.Dispatcher.Store = store
	if r, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command)); !errors.Is(err, fault) {
		t.Fatalf("database fault became a credential business decision %+v %v", r, err)
	}
	if _, err := a.Dispatcher.Lookup(ctx, a.UserAuth, command.CommandID); !api.IsCode(err, "not_found") {
		t.Fatalf("rolled back admission saved a receipt: %v", err)
	}
	store.err = nil
	r, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
	if err != nil || r.Stage != "applied" {
		t.Fatalf("original command could not recover %+v %v", r, err)
	}
	if err = a.Identity.Revoke(ctx, a.UserAuth); err != nil {
		t.Fatal(err)
	}
	command.CommandID, command.TargetID = api.NewID("command"), api.NewID("task")
	r, err = a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
	if err != nil || r.Stage != "rejected" || !api.IsCode(r.Error, "forbidden") || r.Error.Reason != "credential_revoked" {
		t.Fatalf("real current revocation was not rejected %+v %v", r, err)
	}
}
