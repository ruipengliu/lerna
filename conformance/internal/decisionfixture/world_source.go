package decisionfixture

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	process "github.com/ruipengliu/lerna/conformance/internal/testkit/process"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

type Scenario struct {
	Request                 v.DecisionDecideRequest
	Subject                 v.SubjectBinding
	DecisionRef             v.DecisionRef
	MaterialRef             v.ContentRef
	ManifestRef             v.ContentRef
	GetJSON, CommandGetJSON []byte
}

// SourceWorld owns the administrative connection independently of each source
// generation. It never recovers CREATE ownership by scanning names.
type SourceWorld struct {
	t             *testing.T
	cfg           postgres.Config
	owner         v.OwnerRef
	admin, source *Store
	scenario      Scenario
	owns          bool
	closing       bool
	workerExits   []<-chan struct{}
	children      []*process.Child
}

func NewSourceWorld(t *testing.T, ctx context.Context) *SourceWorld {
	t.Helper()
	w, err := createSourceWorld(t, ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Cleanup(); err != nil {
			t.Error(err)
		}
	})
	return w
}
func createSourceWorld(t *testing.T, ctx context.Context) (w *SourceWorld, err error) {
	w = &SourceWorld{t: t, owner: v.OwnerRef{TenantID: "fixture-tenant", OwnerID: "fixture-source"}}
	defer func() {
		if err != nil {
			err = errors.Join(err, w.Cleanup())
		}
	}()
	dsn := os.Getenv("LERNA_TEST_POSTGRES_DSN")
	if dsn == "" {
		return w, errors.New("LERNA_TEST_POSTGRES_DSN is required")
	}
	if os.Getenv("LERNA_TEST_OWNED_SCOPE_REGISTRY") == "" {
		return w, errors.New("LERNA_TEST_OWNED_SCOPE_REGISTRY is required")
	}
	w.cfg, err = ownedConfig(dsn)
	if err != nil {
		return w, err
	}
	w.admin, err = Open(ctx, w.cfg, w.owner)
	if err != nil {
		return w, err
	}
	if err = w.admin.CreateSchema(ctx); err != nil {
		return w, err
	}
	w.owns = true
	if err = RegisterOwnedSchema(w.cfg.Schema); err != nil {
		return w, err
	}
	w.source, err = Open(ctx, w.cfg, w.owner)
	if err != nil {
		return w, err
	}
	if err = w.source.Migrate(ctx); err != nil {
		return w, err
	}
	w.scenario, err = seedScenario(ctx, w.source)
	return w, err
}
func ownedConfig(dsn string) (postgres.Config, error) {
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return postgres.Config{}, err
	}
	return postgres.Config{DSN: dsn, Schema: "lerna_test_" + hex.EncodeToString(nonce[:]), MaxOpenConnections: 4, TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second}, nil
}
func RegisterOwnedSchema(schema string) error {
	if !testIdentifier.MatchString(schema) {
		return errors.New("invalid owned schema identity")
	}
	path := os.Getenv("LERNA_TEST_OWNED_SCOPE_REGISTRY")
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("absolute owned schema registry required")
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintln(file, "postgres", schema)
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
func (w *SourceWorld) Source() *Store     { return w.source }
func (w *SourceWorld) Scenario() Scenario { return w.scenario }
func (w *SourceWorld) Reopen(ctx context.Context) {
	w.t.Helper()
	if w.closing {
		w.t.Fatal("fixture is closing")
	}
	if err := waitWorkerExits(ctx, w.workerExits); err != nil {
		w.t.Fatal(err)
	}
	if err := joinChildren(ctx, w.children); err != nil {
		w.t.Fatal(err)
	}
	if err := w.source.Close(); err != nil {
		w.t.Fatal(err)
	}
	w.source = nil
	var err error
	// Open may return a nonnil holder together with an unconfirmed Close error.
	// Retain it before a fatal test return so Cleanup cannot drop its schema.
	w.source, err = Open(ctx, w.cfg, w.owner)
	if err != nil {
		w.t.Fatal(err)
	}
}
func (w *SourceWorld) Cleanup() error {
	w.closing = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	joined, childErr := stopChildren(w.children)
	if !joined {
		return childErr
	}
	if err := waitWorkerExits(ctx, w.workerExits); err != nil {
		return errors.Join(childErr, err)
	}
	if w.source != nil {
		if err := w.source.Close(); err != nil {
			return errors.Join(childErr, err)
		}
		w.source = nil
	}
	if w.owns {
		if err := w.admin.DropTestSchema(ctx); err != nil {
			return errors.Join(childErr, err)
		}
		w.owns = false
	}
	if w.admin != nil {
		if err := w.admin.Close(); err != nil {
			return errors.Join(childErr, err)
		}
		w.admin = nil
	}
	return childErr
}
func seedScenario(ctx context.Context, source *Store) (Scenario, error) {
	var scene Scenario
	scene.DecisionRef = v.DecisionRef{TenantID: source.owner.TenantID, OwnerID: "fixture-decision", Kind: "decision", ID: "original-decision"}
	scene.Subject = v.SubjectBinding{TenantID: source.owner.TenantID, SubjectID: "fixture-principal", DelegationChain: []v.DelegatedSubject{}}
	scene.MaterialRef = source.Ref("material-alpha", "text/plain", []byte("alpha\n"))
	component := v.ComponentRef{ComponentID: "fixture-rule", ContractVersion: v.Version, ArtifactDigest: v.SchemaDigest(digest([]byte("fixture-rule/2"))), ConfigDigest: v.SchemaDigest(digest([]byte("candidate_result"))), InstallLockRef: v.InstallLockRef{TenantID: source.owner.TenantID, OwnerID: source.owner.OwnerID, Kind: "install_lock", ID: "fixture-rule-lock", Revision: "1"}}
	task := v.TaskObjectRef{TenantID: source.owner.TenantID, OwnerID: source.owner.OwnerID, Kind: "task", ID: "fixture-task"}
	use := v.UseRef{TenantID: source.owner.TenantID, OwnerID: source.owner.OwnerID, Kind: "fixture_use", ID: "fixture-use", Revision: "1"}
	snapshot := decision.Snapshot{Ref: v.SnapshotRef{TenantID: source.owner.TenantID, OwnerID: source.owner.OwnerID, Kind: "snapshot", ID: "fixture-snapshot", Revision: "1"}, TaskRef: task, GoalRevision: "1", ControlRevision: "1", ComponentRef: component, MaterialRefs: []v.ContentRef{scene.MaterialRef}, RequirementRefs: []v.RequirementRef{{TenantID: source.owner.TenantID, OwnerID: source.owner.OwnerID, Kind: "requirement", ID: "fixture-requirement", Revision: "1"}}, CapabilityBindings: []decision.CapabilityBinding{}, AnswerSchemaRefs: []v.ContentRef{}, UseRefs: []v.UseRef{use}, Rule: "candidate_result"}
	until := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Microsecond)
	permission := decision.Permission{Subject: scene.Subject, DecisionOwner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, TaskRef: task, ComponentRef: component, UseRefs: []v.UseRef{use}, ValidUntil: until, RuleVersion: "fixture-rule/2", ChargeBasis: "durable_rule_start", RuleStartCharge: v.Amount{Unit: "fixture", IntegerValue: "1"}}
	var err error
	scene.ManifestRef, err = source.Seed(ctx, Bundle{DecisionRef: scene.DecisionRef, Permission: permission, Snapshot: snapshot, Materials: []Material{{Ref: scene.MaterialRef, Bytes: []byte("alpha\n")}}, Purposes: []string{"decide", "get", "command.get", "start", "material", "rule.input", "fixture.lock", "publish", "proposal.publish", "artifact.publish"}, RuleVersion: permission.RuleVersion, ChargeBasis: permission.ChargeBasis, RuleStartCharge: permission.RuleStartCharge})
	if err != nil {
		return scene, err
	}
	cutoff := v.Time(until.Format("2006-01-02T15:04:05.000000Z"))
	scene.Request = v.DecisionDecideRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: "original-command", Target: scene.DecisionRef, Method: "decision_engine.decide", AcceptBefore: cutoff, Payload: v.DecisionDecidePayload{DecisionID: scene.DecisionRef.ID, TaskRef: task, SnapshotRef: snapshot.Ref, ComponentRef: component, UseRefs: []v.UseRef{use}, Limits: v.DecisionLimits{MaxInputBytes: "1048576", MaxOutputBytes: "1048576", MaxRuleSteps: "1024", MaxActions: "4", MaxCost: v.Amount{Unit: "fixture", IntegerValue: "1024"}}, Deadline: cutoff}}
	scene.GetJSON, err = v.Encode(v.DecisionGetRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: "get-original", Target: scene.DecisionRef, Method: "decision_engine.get", AcceptBefore: cutoff, Payload: v.DecisionGetPayload{DecisionRef: scene.DecisionRef}})
	if err != nil {
		return scene, err
	}
	scene.CommandGetJSON, err = v.Encode(v.CommandGetRequest{ContractVersion: v.Version, Profile: "command", CommandID: "query-original-command", Target: v.CommandTarget{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID, Kind: "command", ID: scene.Request.CommandID}, Method: "command.get", AcceptBefore: cutoff, Payload: v.CommandGetPayload{CommandRef: v.CommandRef{Owner: permission.DecisionOwner, CommandID: scene.Request.CommandID}}})
	return scene, err
}
