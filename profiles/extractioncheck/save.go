package extractioncheck

import (
	"context"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/executionlocal"
	"lerna/adapters/extractionauth"
	"lerna/adapters/extractionexecution"
	"lerna/adapters/memoryauth"
	"lerna/adapters/sqlitememory"
	"lerna/authorization"
	"lerna/execution"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/schema"
	"lerna/sdk"
	"path/filepath"
	"time"
)

// CheckSave combines actual Core/SDK extraction with the bounded auto-save
// service and actual Memory authorization/persistence. It does not claim the
// subsequent independent-task read or source cleanup acceptance criteria.
func CheckSave(ctx context.Context) error {
	return checkSave(ctx, "normal")
}

func CheckSaveFault(ctx context.Context, mode string) error {
	if mode != "lost-reply" && mode != "before-commit" && mode != "save-denied" && mode != "retain-revoked-before-plan" && mode != "source-invalidated-after-save" && !cleanupCase(mode) {
		return fmt.Errorf("unsupported save fault")
	}
	return checkSave(ctx, mode)
}

func checkSave(ctx context.Context, mode string) error {
	h, err := fresh(ctx)
	if err != nil {
		return err
	}
	defer h.destroy()
	r, grant, err := h.request(ctx)
	if err != nil {
		return err
	}
	definition := extraction.SavedSchema()
	schemas, err := schema.New([]schema.Resource{definition})
	if err != nil {
		return err
	}
	policy, err := memoryauth.New(h.auth, h.source, []memoryauth.Collection{{Namespace: "local", Name: "personal", Resource: "root", PolicyRef: "local-extracted", Purpose: "task", Storage: []string{"local"}, Processing: []string{"local"}, Recipients: []string{"local"}, Schemas: map[string]string{definition.Type: schema.Digest(definition.Document)}}})
	if err != nil {
		return err
	}
	store, err := sqlitememory.Open(filepath.Join(h.root, "memory.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	var writes memory.Store = store
	if mode == "lost-reply" || mode == "before-commit" {
		writes = saveFault{Store: store, mode: mode}
	}
	if mode == "source-invalidated-after-save" || cleanupCase(mode) {
		writes = sourceInvalidatingCommit{Store: store, h: h}
	}
	service, err := memory.New(writes, policy, schemas, h.clock, memory.Config{Location: "local", Timeout: time.Second})
	if err != nil {
		return err
	}
	auth, err := extractionauth.New(h.auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "task", Location: "local"})
	if err != nil {
		return err
	}
	b := memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}
	saver, err := extraction.NewSaver(h.candidates, auth, h.source, service, h.clock, b, extraction.SaveTarget{Collection: "personal", PolicyRef: "local-extracted", Purpose: "task"}, h.operation)
	if err != nil {
		return err
	}
	var candidateDriver execution.Driver = h.target
	if mode == "retain-revoked-before-plan" {
		candidateDriver = retentionRevocation{Driver: h.target, h: h}
	}
	driver, err := extractionexecution.WithMemory(candidateDriver, saver)
	if err != nil {
		return err
	}
	h.exec, err = execution.New(h.grants, h.work, h.access, driver, h.binding, h.cap, config(), h.operation)
	if err != nil {
		return err
	}
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	if _, err = h.client.Invoke(ctx, r, grant); err != nil {
		return err
	}
	if mode == "save-denied" {
		if err := revokeSaveAction(ctx, h, "memory.put"); err != nil {
			return err
		}
	}
	outcome, err := h.exec.Run(ctx, r.OperationID)
	if err != nil {
		return err
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		return err
	}
	task, err := h.core.Get(ctx, h.token, r.Qualification.Ref)
	if err != nil {
		return err
	}
	if mode == "save-denied" || mode == "retain-revoked-before-plan" {
		if task.State == "COMPLETED" || outcome.Reference != "" || outcome.Result == "SUCCESS" {
			return fmt.Errorf("denied save completed task")
		}
		if _, e := h.candidates.LookupSave(ctx, "local", "operator", r.OperationID); e != memory.Missing {
			return fmt.Errorf("denied save retained intent: %v", e)
		}
		candidate, e := h.candidates.Inspect(ctx, "local", r.OperationID)
		if e != nil || !candidate.Committed {
			return fmt.Errorf("lost separately authorized candidate: %v", e)
		}
		changes, e := store.ReadChanges(ctx, "local", "personal", 0, 16)
		if e != nil || len(changes) != 0 {
			return fmt.Errorf("denied save committed Memory: %v", e)
		}
		return nil
	}
	if mode == "before-commit" {
		state, e := saver.Inspect(ctx, r.OperationID)
		if e != nil || state.State != "unknown" || outcome.Result == "SUCCESS" || outcome.Reference != "" || task.State == "COMPLETED" {
			return fmt.Errorf("unknown save claimed success: %s %s %v", state.State, task.State, e)
		}
		intent, e := h.candidates.LookupSave(ctx, "local", "operator", r.OperationID)
		if e != nil {
			return e
		}
		replay, e := saver.Save(ctx, r.OperationID)
		if e != nil || replay.State != "unknown" {
			return fmt.Errorf("unknown original save replaced: %s %v", replay.State, e)
		}
		after, e := h.candidates.LookupSave(ctx, "local", "operator", r.OperationID)
		if e != nil || after.Request.OperationId != intent.Request.OperationId {
			return fmt.Errorf("original intent replaced: %v", e)
		}
		changes, e := store.ReadChanges(ctx, "local", "personal", 0, 16)
		if e != nil || len(changes) != 0 {
			return fmt.Errorf("unknown save invented commit: %v", e)
		}
		return nil
	}
	if mode == "source-invalidated-after-save" || cleanupCase(mode) {
		state, e := saver.Inspect(ctx, r.OperationID)
		if e != nil || state.State != "committed" || state.ContentAvailability != "unavailable" {
			return fmt.Errorf("invalidated Memory availability: %+v %v", state, e)
		}
		if task.State == "COMPLETED" || outcome.Reference != "" || outcome.Effect != "CONFIRMED" || outcome.Result != "FAILURE" {
			return fmt.Errorf("invalidated save task: %s %s/%s", task.State, outcome.Result, outcome.Effect)
		}
		intent, e := h.candidates.LookupSave(ctx, "local", "operator", r.OperationID)
		if e != nil || intent.State != "retired" || intent.Request.Spec != nil {
			return fmt.Errorf("invalidated save intent still usable: %v", e)
		}
		if cleanupCase(mode) {
			return checkMemoryCleanup(ctx, h, store, policy, schemas, r.OperationID, mode)
		}
		return nil
	}
	if err != nil || task.State != "COMPLETED" || outcome.Result != "SUCCESS" {
		return fmt.Errorf("saving task: %s %s %v", task.State, outcome.Result, err)
	}
	state, err := saver.Inspect(ctx, r.OperationID)
	if err != nil || state.State != "committed" || state.Receipt == nil {
		return fmt.Errorf("automatic Memory save: %s %v", state.State, err)
	}
	replay, err := saver.Save(ctx, r.OperationID)
	if err != nil || replay.Receipt == nil || *replay.Receipt != *state.Receipt {
		return fmt.Errorf("original save reconciliation: %v", err)
	}
	intent, err := h.candidates.LookupSave(ctx, "local", "operator", r.OperationID)
	if err != nil || intent.Request.OperationId != state.Receipt.OperationID {
		return fmt.Errorf("lost save association: %v", err)
	}
	row, err := store.Read(ctx, state.Receipt.Ref, 1)
	if err != nil {
		return err
	}
	record := new(wire.MemoryRecord)
	if protojson.Unmarshal(row.Document, record) != nil || record.Spec.Kind != "preference" || string(record.Spec.Content.Json) != `{"attribute":"format","value":"concise"}` || len(record.Spec.Sources) != 1 || record.Spec.Sources[0].Ref.Revision != 1 {
		return fmt.Errorf("wrong persisted extraction")
	}
	changes, err := store.ReadChanges(ctx, "local", "personal", 0, 16)
	if err != nil || len(changes) != 1 {
		return fmt.Errorf("duplicate Memory save: %v", err)
	}
	return nil
}

// Exact commit-boundary faults preserve the real authorization admission and
// SQLite implementation. A lost reply performs the actual commit first.
type saveFault struct {
	memory.Store
	mode string
}

func (f saveFault) Commit(ctx context.Context, change memory.Change) (memory.Receipt, error) {
	if f.mode == "before-commit" {
		return memory.Receipt{}, memory.Unavailable
	}
	if _, err := f.Store.Commit(ctx, change); err != nil {
		return memory.Receipt{}, err
	}
	return memory.Receipt{}, memory.Unavailable
}

func revokeSaveAction(ctx context.Context, h *harness, deniedAction string) error {
	snapshot, e := h.db.Load(ctx)
	if e != nil {
		return e
	}
	scope := proto.Clone(snapshot.State.Rules[0].Scope).(*wire.AuthorizationScope)
	var actions []string
	for _, action := range scope.Actions {
		if action != deniedAction {
			actions = append(actions, action)
		}
	}
	scope.Actions = actions
	op, e := h.operation(ctx)
	if e != nil {
		return e
	}
	_, e = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: &wire.AuthorizationCommand{ExpectedRevision: snapshot.State.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "no-save", Scope: scope}}}}}})
	if e != nil {
		return e
	}
	return nil
}

type retentionRevocation struct {
	execution.Driver
	h *harness
}

type sourceInvalidatingCommit struct {
	memory.Store
	h *harness
}

func (s sourceInvalidatingCommit) Commit(ctx context.Context, c memory.Change) (memory.Receipt, error) {
	r, err := s.Store.Commit(ctx, c)
	if err != nil {
		return r, err
	}
	_, err = s.h.candidates.InvalidateSource(ctx, extraction.SourceInvalidation{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 1})
	return r, err
}

func (d retentionRevocation) Start(ctx context.Context, c execution.Call) error {
	if err := d.Driver.Start(ctx, c); err != nil {
		return err
	}
	return revokeSaveAction(ctx, d.h, "memory.candidate.retain")
}

func cleanupCase(mode string) bool {
	switch mode {
	case "cleanup-after-invalidation", "cleanup-lost-reply", "cleanup-before-delete", "cleanup-worker":
		return true
	}
	return false
}
