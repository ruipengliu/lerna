package memorycheck

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lerna/adapters/memorylocal"
	"lerna/adapters/recoveryproof"
	"lerna/adapters/sqlitememory"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/sdk"
)

type recoveryFixture struct {
	source  *fixture
	restore *sqlitememory.Restore
	view    memory.QueryStore
	client  *sdk.MemoryClient
	binding memory.RecoveryBinding
	deleted *wire.MemoryDeletionQuery
}

func (r *recoveryFixture) close() {
	if r.restore != nil {
		r.restore.Close()
	}
	if r.source != nil {
		r.source.close()
		os.RemoveAll(r.source.config.Root)
	}
}

func openRecoveryFixture(ctx context.Context) (result *recoveryFixture, err error) {
	f, err := newFixture(ctx)
	if err != nil {
		return nil, err
	}
	r := &recoveryFixture{source: f}
	defer func() {
		if err != nil {
			r.close()
		}
	}()
	if err = f.put(ctx); err != nil {
		return nil, err
	}
	id, err := f.auth.NewOperation(ctx, f.config.Token)
	if err != nil {
		return nil, err
	}
	removed := f.write(id, 0, "to delete")
	removed.Write.Ref.Key = "removed"
	if _, err = f.client.Exchange(ctx, removed); err != nil {
		return nil, err
	}
	if err = f.store.Close(); err != nil {
		return nil, err
	}
	path := filepath.Join(f.config.Root, "memory.db")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	backup := filepath.Join(f.config.Root, "backup.db")
	if err = os.WriteFile(backup, raw, 0600); err != nil {
		return nil, err
	}
	f.store, err = sqlitememory.Open(path)
	if err != nil {
		return nil, err
	}
	f.client, err = f.bind(f.store, f.permits)
	if err != nil {
		return nil, err
	}
	deletionID, err := f.auth.NewOperation(ctx, f.config.Token)
	if err != nil {
		return nil, err
	}
	if _, err = f.client.Exchange(ctx, &wire.MemoryRequest{Method: "DELETE", Delete: &wire.MemoryDelete{OperationId: deletionID, Ref: removed.Write.Ref, ExpectedRevision: 1, Purpose: "assist"}}); err != nil {
		return nil, err
	}
	r.deleted = &wire.MemoryDeletionQuery{OperationId: deletionID, Ref: removed.Write.Ref, Purpose: "assist"}
	restored, err := sqlitememory.OpenRestored(backup)
	if err != nil {
		return nil, err
	}
	r.restore = restored
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	recoveryScope := memory.RecoveryScope{Namespace: "local", Collection: "personal"}
	source, err := recoveryproof.NewIssuer(f.store, recoveryproof.IssuerConfig{Authority: "primary", Epoch: 1, Scope: recoveryScope, Key: private})
	if err != nil {
		return nil, err
	}
	verifier, err := recoveryproof.NewVerifier(recoveryproof.VerifierConfig{Authority: "primary", Epoch: 1, Scope: recoveryScope, Key: public, MinPosition: 3})
	if err != nil {
		return nil, err
	}
	view, err := restored.ReadView(memory.RecoveryBinding{Scope: recoveryScope, Source: source, Verifier: verifier})
	if err != nil {
		return nil, err
	}
	client, err := f.bind(view, f.permits)
	if err != nil {
		return nil, err
	}
	r.view, r.client, r.binding = view, client, memory.RecoveryBinding{Scope: recoveryScope, Source: source, Verifier: verifier}
	return r, nil
}

// CheckRecovery uses an actual pre-deletion SQLite backup, independent current
// trust, and the original governed SDK. It makes no network or model requests.
func CheckRecovery(ctx context.Context) (memory.RecoveryProgress, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	fail := func(err error) (memory.RecoveryProgress, error) { return memory.RecoveryProgress{}, err }
	r, err := openRecoveryFixture(ctx)
	if err != nil {
		return fail(err)
	}
	defer r.close()
	f := r.source
	if err = r.checkCleanupReport(ctx, "pending", 0); err != nil {
		return fail(err)
	}
	query := &wire.MemoryRequest{Method: "QUERY", Query: f.query(), GrantMaterial: f.config.Grant}
	checkQuery := func() error {
		out, e := r.client.Exchange(ctx, query)
		if e != nil {
			return e
		}
		if len(out.GetResult().GetRecords()) != 1 || out.Result.Records[0].Ref.Key != "format" || string(out.Result.Records[0].Spec.Content.Json) != `{"text":"concise"}` {
			return fmt.Errorf("backup query resurrected deleted data or lost retained data")
		}
		return nil
	}
	if err = checkQuery(); err != nil {
		return fail(err)
	}
	progress, err := r.restore.InspectRecovery(ctx, r.binding.Scope)
	if err != nil {
		return fail(err)
	}
	if progress.State != "sanitized" || progress.OriginPosition != 2 || progress.VerifiedPosition != 2 || progress.AppliedPosition != 3 || progress.Retained != 1 || progress.Missing != 0 {
		return fail(fmt.Errorf("backup cleanup progress does not match the actual source deletion"))
	}
	if err = r.checkCleanupReport(ctx, "applied", 3); err != nil {
		return fail(err)
	}
	if _, err = r.view.Commit(ctx, memory.Change{}); err != memory.Quarantined {
		return fail(fmt.Errorf("recovered data view acquired mutation rights"))
	}
	for _, recipient := range []string{"cloud", "device-a"} {
		f.binding.Recipient = recipient
		id, e := f.auth.NewOperation(ctx, f.config.Token)
		if e != nil {
			return fail(e)
		}
		get := &wire.MemoryGet{ReadId: id, Ref: f.write(f.config.WriteID, 0, "concise").Write.Ref, Revision: 1, Purpose: "assist"}
		intent, e := memory.DescribeGet(f.binding, get)
		if e != nil {
			return fail(e)
		}
		grant, e := f.sign(ctx, intent)
		if e != nil {
			return fail(e)
		}
		client, e := f.bind(r.view, f.permits)
		if e != nil {
			return fail(e)
		}
		out, e := client.Exchange(ctx, &wire.MemoryRequest{Method: "GET", Get: get, GrantMaterial: grant})
		if recipient == "cloud" {
			if e != memory.Denied || out != nil {
				return fail(fmt.Errorf("recovery bypassed current recipient residency"))
			}
		} else if e != nil || len(out.GetResult().GetRecords()) != 1 {
			return fail(fmt.Errorf("permitted recipient could not read retained data: %v", e))
		}
	}
	if err = r.restore.Close(); err != nil {
		return fail(err)
	}
	backup := filepath.Join(f.config.Root, "backup.db")
	ordinary, e := sqlitememory.Open(backup)
	if ordinary != nil {
		ordinary.Close()
		return fail(fmt.Errorf("backup recovery activated ordinary runtime"))
	}
	if e != memory.Quarantined {
		return fail(fmt.Errorf("backup quarantine disappeared: %v", e))
	}
	r.restore, err = sqlitememory.OpenRestored(backup)
	if err != nil {
		return fail(err)
	}
	saved, err := r.restore.InspectRecovery(ctx, r.binding.Scope)
	if err != nil || saved != progress {
		return fail(fmt.Errorf("backup progress lost on reopen: %v", err))
	}
	r.view, err = r.restore.ReadView(r.binding)
	if err != nil {
		return fail(err)
	}
	r.client, err = f.bind(r.view, f.permits)
	if err != nil {
		return fail(err)
	}
	if err = checkQuery(); err != nil {
		return fail(err)
	}
	if err = r.checkCleanupReport(ctx, "applied", 3); err != nil {
		return fail(err)
	}
	policy, err := f.auth.GetPolicy(ctx, f.config.Token)
	if err != nil {
		return fail(err)
	}
	op, err := f.auth.NewOperation(ctx, f.config.Token)
	if err != nil {
		return fail(err)
	}
	_, err = f.auth.Execute(ctx, f.config.Token, authorization.Mutation{Namespace: "local", OperationID: op, Command: &wire.AuthorizationCommand{ExpectedRevision: policy.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{}}}}})
	if err != nil {
		return fail(err)
	}
	if out, e := r.client.Exchange(ctx, query); e == nil || out != nil {
		return fail(fmt.Errorf("backup reused current revoked authorization"))
	}
	return progress, nil
}

func (r *recoveryFixture) checkCleanupReport(ctx context.Context, state string, position uint64) error {
	f := r.source
	target, inspection, err := r.restore.CleanupTarget("controlled-memory-backup", r.binding)
	if err != nil {
		return err
	}
	reporter, err := memory.NewDeletionReporter(f.store, inspection, []memory.CleanupTarget{target, {Name: "unconnected-archive", Dimension: memory.DerivedCleanup}})
	if err != nil {
		return err
	}
	service, err := memory.New(f.store, f.authority, f.schemas, clock{}, memory.Config{Location: "device-a", Timeout: 5 * time.Second})
	if err != nil {
		return err
	}
	service, err = service.WithDeletionReporter(reporter)
	if err != nil {
		return err
	}
	reader, err := memory.NewReader(service, f.permits)
	if err != nil {
		return err
	}
	client := sdk.NewMemoryClient(memorylocal.Bind(service, reader, f.binding), "local")
	out, err := client.Exchange(ctx, &wire.MemoryRequest{Method: "DELETION_STATUS", DeletionQuery: r.deleted})
	if err != nil {
		return err
	}
	status := out.GetDeletion()
	report := status.GetReport()
	if status.GetState() != "committed" || len(report.GetDerivedArchives()) != 2 || len(report.GetReplicas()) != 1 || len(report.GetLocal()) != 1 {
		return fmt.Errorf("backup deletion report omitted configured scope")
	}
	backup, archive := report.DerivedArchives[0], report.DerivedArchives[1]
	if backup.Name != "controlled-memory-backup" || backup.State != state || backup.Position != position || archive.Name != "unconnected-archive" || archive.State != "not_covered" || report.Replicas[0].State != "not_covered" || report.Local[0].State != "not_covered" {
		return fmt.Errorf("backup report overstated actual cleanup scope")
	}
	return nil
}
