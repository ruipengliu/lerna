//go:build integration

package decisionfixture

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	decisionpg "github.com/ruipengliu/lerna/adapters/postgres/decision_engine"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

type frozenEntry struct {
	Artifact, Destination, SHA256 string
	Bytes                         int
	Role                          string
}
type frozenProvenance struct {
	Format              string        `json:"format"`
	SourceCommit        string        `json:"source_commit"`
	SourcePackages      []string      `json:"source_packages"`
	Files               []frozenEntry `json:"files"`
	ProductionFileCount int           `json:"production_file_count"`
	ProductionBytes     int           `json:"production_bytes"`
	FileCount           int           `json:"file_count"`
	PayloadBytes        int           `json:"payload_bytes"`
}
type upgradeMigration struct {
	Version  int64  `json:"version"`
	Checksum string `json:"checksum"`
}
type upgradeCase struct {
	Name              string             `json:"name"`
	SourceSchema      string             `json:"source_schema"`
	DecisionSchema    string             `json:"decision_schema"`
	SourceOwner       v.OwnerRef         `json:"source_owner"`
	Request           json.RawMessage    `json:"request"`
	Subject           json.RawMessage    `json:"subject"`
	GetRequest        json.RawMessage    `json:"get_request"`
	CommandGetRequest json.RawMessage    `json:"command_get_request"`
	AcceptedReceipt   json.RawMessage    `json:"accepted_receipt"`
	DecisionBefore    json.RawMessage    `json:"decision_before"`
	Migrations        []upgradeMigration `json:"migrations"`
	PublishedRefs     []v.ContentRef     `json:"published_refs"`
	PublishedBodies   []string           `json:"published_bodies"`
	WritersDrained    bool               `json:"writers_drained"`
}
type upgradeReady struct {
	Protocol string        `json:"protocol"`
	Cases    []upgradeCase `json:"cases"`
}

// The old producer is frozen production code, not a simulation of old tables.
type frozenWriterSession struct {
	Frame         []byte
	Registry      string
	CurrentClosed bool
}

// startFrozenWriter owns only finite build, pipes, process group and exact
// acknowledged cleanup. Each consumer decodes its own business protocol and
// must register new writer closure before reporting any open/migration error.
func startFrozenWriter(t *testing.T, dir, producer string) *frozenWriterSession {
	t.Helper()
	if os.Getenv("LERNA_TEST_POSTGRES_DSN") == "" || os.Getenv("LERNA_TEST_OWNED_SCOPE_REGISTRY") == "" {
		t.Fatal("upgrade requires configured PostgreSQL and exact owned-scope registry")
	}
	switch producer {
	case "TestFrozenLegacyWriter", "TestFrozenFinal01ControlWriter", "TestFrozenFinal01ProposalWriter":
	default:
		t.Fatal("unsupported finite frozen producer")
	}
	session := &frozenWriterSession{CurrentClosed: true}
	registry := filepath.Join(dir, "created-scopes.registry")
	session.Registry = registry
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 40*time.Second)
	build := exec.CommandContext(buildCtx, "go", "test", "-c", "-o", filepath.Join(dir, "legacy.test"), "./conformance/internal/decisionfixture")
	build.Dir = dir
	setUpgradeProcessBounds(build)
	var diagnostics upgradeOutput
	build.Stdout = &diagnostics
	build.Stderr = &diagnostics
	if err := build.Run(); err != nil {
		buildCancel()
		t.Logf("bounded frozen writer build diagnostics: %s", diagnostics.safeText(os.Getenv("LERNA_TEST_POSTGRES_DSN")))
		t.Fatal(upgradeCause("frozen writer build", err))
	}
	buildCancel()
	childCtx, childCancel := context.WithTimeout(context.Background(), 65*time.Second)
	child := exec.CommandContext(childCtx, filepath.Join(dir, "legacy.test"), "-test.run=^"+producer+"$", "-test.timeout=60s")
	child.Dir = dir
	setUpgradeProcessBounds(child)
	child.Env = append(os.Environ(), "LERNA_TEST_OWNED_SCOPE_REGISTRY="+registry)
	stdin, stdout, closePipes, err := openUpgradePipes(child)
	started := false
	// Before Start succeeds we own every acquired pipe end. Register cleanup
	// before handling allocation errors; cancellation alone closes no pipes.
	t.Cleanup(func() {
		if started {
			return
		} // Successful Start/Wait uses the cleanup below.
		childCancel()
		if closePipes != nil {
			if err := closePipes(); err != nil {
				t.Error("pre-start pipe close unconfirmed; retain exact directory:", dir, err)
				return
			}
		}
		if child.Process != nil {
			t.Error("pre-start process state unconfirmed; retain exact directory:", dir)
			return
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Error(upgradeCause("pre-start directory cleanup", err))
		}
	})
	if err != nil {
		childCancel()
		t.Fatal(upgradeCause("writer pipe allocation", err))
	}
	child.Stderr = &diagnostics
	if err = child.Start(); err != nil {
		childCancel()
		t.Fatal(upgradeCause("writer start", err))
	}
	started = true
	readyCh := make(chan []byte, 1)
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		scanner := bufio.NewScanner(io.LimitReader(stdout, 256*1024))
		scanner.Buffer(make([]byte, 4096), 128*1024+32)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "LERNA_LEGACY_READY ") {
				readyCh <- []byte(strings.TrimPrefix(line, "LERNA_LEGACY_READY "))
				return
			}
		}
		readyCh <- nil
	}()
	waited := make(chan error, 1)
	go func() { waited <- child.Wait() }()
	// Registered before opening new writers: subsequent cleanup closes them first.
	t.Cleanup(func() {
		if !session.CurrentClosed {
			childCancel()
			select {
			case waitErr := <-waited:
				if waitErr != nil {
					t.Error("historical admin cancellation:", waitErr)
				}
			case <-time.After(3 * time.Second):
				t.Error("historical admin exit unconfirmed")
			}
			if err := stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				t.Error(err)
			}
			if err := stdout.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				t.Error(err)
			}
			t.Errorf("new writer closure unconfirmed; retain exact historical directory %s and registered scopes", dir)
			return
		}
		_, releaseErr := io.WriteString(stdin, "RELEASE\n")
		stdinErr := stdin.Close()
		var waitErr error
		confirmed := false
		select {
		case waitErr = <-waited:
			confirmed = true
		case <-time.After(8 * time.Second):
			if err := syscall.Kill(-child.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				t.Error(err)
			}
			select {
			case waitErr = <-waited:
				confirmed = true
			case <-time.After(3 * time.Second):
			}
		}
		childCancel()
		if err := stdout.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
		select {
		case <-scanDone:
		case <-time.After(time.Second):
			t.Error("historical stdout reader did not drain")
		}
		groupErr := confirmUpgradeGroupExit(child.Process.Pid)
		if !confirmed || groupErr != nil {
			t.Errorf("historical process/group exit unconfirmed; retain exact directory %s and registered scopes: %v", dir, groupErr)
			return
		}
		if releaseErr != nil || stdinErr != nil || waitErr != nil {
			t.Error("historical producer did not complete normal release:", errors.Join(releaseErr, stdinErr, waitErr))
		}
		if err := cleanupUpgradeScopes(registry); err != nil {
			t.Error("exact historical scope cleanup failed; retain owned directory:", err)
			return
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	var frame []byte
	select {
	case frame = <-readyCh:
	case <-time.After(35 * time.Second):
		t.Fatal("historical producer ready deadline")
	}
	if len(frame) == 0 || len(frame) > 128*1024 {
		t.Fatal("historical bounded ready frame missing")
	}
	session.Frame = frame
	return session
}

func TestFrozenLegacyWriterUpgrade(t *testing.T) {
	t.Helper()
	session := startFrozenWriter(t, restoreFrozenWriter(t), "TestFrozenLegacyWriter")
	frame, registry := session.Frame, session.Registry
	var ready upgradeReady
	decoder := json.NewDecoder(bytes.NewReader(frame))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&ready); err != nil {
		t.Fatal(upgradeCause("ready frame decode", err))
	}
	if ready.Protocol != "lerna-legacy-decision-970fd90-1" || len(ready.Cases) != 5 {
		t.Fatal("historical protocol mismatch")
	}
	created, err := readUpgradeBounded(registry, 4096)
	if err != nil {
		t.Fatal("historical CREATE registry unavailable:", err)
	}
	acknowledged := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(created)), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 || parts[0] != "postgres" || !testIdentifier.MatchString(parts[1]) || acknowledged[parts[1]] {
			t.Fatal("invalid historical CREATE acknowledgment")
		}
		acknowledged[parts[1]] = true
	}
	if len(acknowledged) != 10 {
		t.Fatal("historical exact CREATE count mismatch")
	}
	for _, old := range ready.Cases {
		if !acknowledged[old.SourceSchema] || !acknowledged[old.DecisionSchema] || old.SourceSchema == old.DecisionSchema {
			t.Fatal("historical ready pair lacks successful CREATE registration")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	seen := map[string]bool{}
	for _, old := range ready.Cases {
		if seen[old.Name] || (old.Name != "accepted" && old.Name != "running" && old.Name != "completed" && old.Name != "failed" && old.Name != "publication_running") || !old.WritersDrained || !testIdentifier.MatchString(old.SourceSchema) || !testIdentifier.MatchString(old.DecisionSchema) {
			t.Fatal("untrusted historical case identity/drain")
		}
		seen[old.Name] = true
		verifyUpgradeCase(t, ctx, old, &session.CurrentClosed)
	}
	fresh := NewWorld(t, ctx)
	scene := fresh.Scenario()
	service := fresh.Service()
	raw, err := v.Encode(scene.Request)
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.Decide(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := response.AsReceived()
	if !ok {
		t.Fatal("v2 normal acceptance unavailable")
	}
	if _, ok = received.Receipt.AsAccepted(); !ok {
		t.Fatal("v2 normal counterpart rejected")
	}
	step, err := service.Step(ctx)
	if err != nil || step.Processed != 1 {
		t.Fatalf("v2 normal step: %v", err)
	}
	view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("v2 normal fact missing")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok || completed.Usage.RuleStarts != "1" || completed.Usage.RuleSteps != "1" || completed.Usage.ModelRequests != "0" || !completed.Usage.MeasurementsComplete || completed.Usage.Cost != (v.Amount{Unit: "fixture", IntegerValue: "1"}) {
		t.Fatal("v2 durable-start accounting counterpart failed")
	}
}

func verifyUpgradeCase(t *testing.T, ctx context.Context, old upgradeCase, currentClosed *bool) {
	t.Helper()
	cfg := postgres.Config{DSN: os.Getenv("LERNA_TEST_POSTGRES_DSN"), Schema: old.DecisionSchema, MaxOpenConnections: 4, TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second}
	store, err := decisionpg.Open(ctx, cfg)
	if store != nil {
		t.Cleanup(func() {
			if err := store.Close(); err != nil {
				*currentClosed = false
				t.Error(err)
			}
		})
	}
	if err != nil {
		t.Fatal(upgradeCause("new Decision writer open", err))
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	versions, err := store.MigrationVersions(ctx)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	if len(old.Migrations) != 1 || len(versions) != 2 || versions[0].Version != old.Migrations[0].Version || versions[0].Checksum != old.Migrations[0].Checksum || versions[1].Version != 2 {
		t.Fatal("upgrade replaced original checksum or lacks owner0002")
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	again, err := store.MigrationVersions(ctx)
	if err != nil || !reflect.DeepEqual(versions, again) {
		t.Fatal("repeated migration changed acknowledged ledger", upgradeCause("repeated migration versions", err))
	}
	cfg.Schema = old.SourceSchema
	source, err := Open(ctx, cfg, old.SourceOwner)
	if source != nil {
		t.Cleanup(func() {
			if err := source.Close(); err != nil {
				*currentClosed = false
				t.Error(err)
			}
		})
	}
	if err != nil {
		t.Fatal(upgradeCause("new fixture reader open", err))
	}
	subject, err := v.Decode[v.SubjectBinding](old.Subject)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	request, err := v.Decode[v.DecisionDecideRequest](old.Request)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	service, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: request.Target.TenantID, OwnerID: request.Target.OwnerID}, Store: store, Authority: source, Source: source, Publisher: source, Component: request.Payload.ComponentRef, Worker: "upgrade-worker", Lease: 5 * time.Second, PoolControl: true})
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	// DDL classifies measurements only. Business closure belongs to the real
	// current owner's Maintain transaction and trusted database clock.
	migrated, err := service.Get(ctx, old.GetRequest, &subject)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	migratedFound, ok := migrated.AsFound()
	if !ok {
		t.Fatal("classified old fact unavailable")
	}
	migratedBytes, err := v.Encode(migratedFound.Decision)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	var beforeDDL, afterDDL map[string]json.RawMessage
	_ = json.Unmarshal(old.DecisionBefore, &beforeDDL)
	_ = json.Unmarshal(migratedBytes, &afterDDL)
	for _, key := range []string{"status", "revision"} {
		if !sameUpgradeJSON(beforeDDL[key], afterDDL[key]) {
			t.Fatalf("migration changed business %s before owner Maintain", key)
		}
	}
	if _, err = service.Maintain(ctx); err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	view, err := service.Get(ctx, old.GetRequest, &subject)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("old public fact unavailable after upgrade")
	}
	current, err := v.Encode(found.Decision)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	var prior, next map[string]json.RawMessage
	_ = json.Unmarshal(old.DecisionBefore, &prior)
	_ = json.Unmarshal(current, &next)
	var priorUsage, nextUsage map[string]json.RawMessage
	_ = json.Unmarshal(prior["usage"], &priorUsage)
	_ = json.Unmarshal(next["usage"], &nextUsage)
	for _, key := range []string{"input_bytes", "output_bytes", "rule_steps", "model_requests", "cost"} {
		if !sameUpgradeJSON(priorUsage[key], nextUsage[key]) {
			t.Fatalf("legacy %s changed original lower-bound %s", old.Name, key)
		}
	}
	if string(nextUsage["rule_starts"]) != `"0"` || string(nextUsage["measurements_complete"]) != fmt.Sprint(old.Name == "accepted") {
		t.Fatal("legacy upgrade invented a measured rule start or fee")
	}
	if old.Name == "completed" {
		completed, ok := found.Decision.AsCompleted()
		if !ok {
			t.Fatal("old completion retired")
		}
		for _, key := range []string{"proposal", "proposal_ref", "artifact_refs"} {
			if !sameUpgradeJSON(prior[key], next[key]) {
				t.Fatalf("legacy completion changed %s", key)
			}
		}
		permit, err := source.Authorize(ctx, subject, request.Target, "get", nil)
		if err != nil {
			t.Fatal(upgradeCause("legacy "+old.Name, err))
		}
		if len(completed.ArtifactRefs) != 1 {
			t.Fatal("legacy artifact identity missing")
		}
		body, err := source.ReadPublished(ctx, completed.ArtifactRefs[0], permit)
		if err != nil || string(body) != "fixture result: alpha\n" {
			t.Fatal("legacy original artifact unreadable", upgradeCause("artifact readback", err))
		}
	} else if old.Name == "failed" {
		if !sameUpgradeJSON(prior["failure"], next["failure"]) {
			t.Fatal("old actual failure changed")
		}
		if _, ok := found.Decision.AsFailed(); !ok {
			t.Fatal("old actual failure unavailable")
		}
	} else {
		failed, ok := found.Decision.AsFailed()
		expected := "billing_basis_unsupported"
		if old.Name == "running" || old.Name == "publication_running" {
			expected = "usage_unavailable"
		}
		if !ok || string(failed.Failure) != expected {
			t.Fatalf("legacy %s wrong retirement reason", old.Name)
		}
	}
	if old.Name == "publication_running" {
		if len(old.PublishedRefs) != 2 || len(old.PublishedBodies) != 2 {
			t.Fatal("old publication window frame missing")
		}
		permit, err := source.Authorize(ctx, subject, request.Target, "get", nil)
		if err != nil {
			t.Fatal(upgradeCause("legacy "+old.Name, err))
		}
		for i, ref := range old.PublishedRefs {
			actual, err := source.ReadPublished(ctx, ref, permit)
			if err != nil || string(actual) != old.PublishedBodies[i] {
				t.Fatal("old publication window changed exact original bytes/ref", upgradeCause("publication window readback", err))
			}
		}
	}
	command, err := service.GetCommand(ctx, old.CommandGetRequest, &subject)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	fixed, ok := command.AsFound()
	if !ok {
		t.Fatal("old fixed command receipt unavailable")
	}
	encoded, err := v.Encode(fixed.Receipt)
	if err != nil || !sameUpgradeJSON(encoded, old.AcceptedReceipt) {
		t.Fatal("upgrade changed original accepted receipt", upgradeCause("original receipt encode", err))
	}
	replay, err := service.Decide(ctx, old.Request, &subject)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	receipt, ok := replay.AsReceived()
	if !ok {
		t.Fatal("old accepted replay unavailable")
	}
	encoded, err = v.Encode(receipt.Receipt)
	if err != nil || !sameUpgradeJSON(encoded, old.AcceptedReceipt) {
		t.Fatal("old replay changed original receipt", upgradeCause("replayed receipt encode", err))
	}
	// Seed only a fixture permission for a new identity; no business record or job.
	permit, err := source.Authorize(ctx, subject, request.Target, "get", nil)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	snapshot, err := source.ReadSnapshot(ctx, request.Payload.SnapshotRef, permit, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	materials := make([]Material, 0, len(snapshot.MaterialRefs))
	for _, ref := range snapshot.MaterialRefs {
		body, err := source.ReadMaterial(ctx, ref, "rule.input", permit, v.MaxBodyBytes)
		if err != nil {
			t.Fatal(upgradeCause("legacy "+old.Name, err))
		}
		materials = append(materials, Material{Ref: ref, Bytes: body})
	}
	newRef := request.Target
	newRef.ID = v.ID("upgrade-new-decision-" + old.Name)
	if _, err = source.Seed(ctx, Bundle{DecisionRef: newRef, Permission: permit, Snapshot: snapshot, Materials: materials, Purposes: []string{"decide", "get", "command.get", "start", "material", "rule.input", "fixture.lock", "publish", "proposal.publish", "artifact.publish"}, RuleVersion: "fixture-rule/1"}); err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	request.Target = newRef
	request.Payload.DecisionID = newRef.ID
	// A new command on the durable old binding must be fixed unsupported.
	request.CommandID = v.ID("upgrade-new-" + old.Name)
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	rejected, err := service.Decide(ctx, raw, &subject)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	durable, ok := rejected.AsReceived()
	if !ok {
		t.Fatal("old binding rejection not durable")
	}
	reason, ok := durable.Receipt.AsRejected()
	if !ok || reason.Reason != "unsupported" {
		t.Fatal("new command on old binding accepted")
	}
	repeated, err := service.Decide(ctx, raw, &subject)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	fixedRejected, ok := repeated.AsReceived()
	if !ok {
		t.Fatal("unsupported replay missing")
	}
	a, err := v.Encode(durable.Receipt)
	if err != nil {
		t.Fatal(upgradeCause("unsupported original receipt encode", err))
	}
	b, err := v.Encode(fixedRejected.Receipt)
	if err != nil {
		t.Fatal(upgradeCause("unsupported repeated receipt encode", err))
	}
	if !sameUpgradeJSON(a, b) {
		t.Fatal("unsupported fixed replay changed")
	}
	query, err := v.Decode[v.CommandGetRequest](old.CommandGetRequest)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	query.Payload.CommandRef.CommandID = request.CommandID
	query.Target.ID = request.CommandID
	queryRaw, err := v.Encode(query)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	queried, err := service.GetCommand(ctx, queryRaw, &subject)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	queriedFixed, ok := queried.AsFound()
	if !ok {
		t.Fatal("unsupported command fixed query unavailable")
	}
	queriedBytes, err := v.Encode(queriedFixed.Receipt)
	if err != nil {
		t.Fatal(upgradeCause("unsupported queried receipt encode", err))
	}
	if !sameUpgradeJSON(a, queriedBytes) {
		t.Fatal("unsupported query changed fixed receipt")
	}
	originalReplay, err := service.Decide(ctx, old.Request, &subject)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	originalFixed, ok := originalReplay.AsReceived()
	if !ok {
		t.Fatal("new refusal obscured original accepted replay")
	}
	originalBytes, err := v.Encode(originalFixed.Receipt)
	if err != nil {
		t.Fatal(upgradeCause("original accepted receipt encode", err))
	}
	if !sameUpgradeJSON(originalBytes, old.AcceptedReceipt) {
		t.Fatal("new refusal changed original accepted replay")
	}
	var get v.DecisionGetRequest
	get, err = v.Decode[v.DecisionGetRequest](old.GetRequest)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	get.Target = newRef
	get.Payload.DecisionRef = newRef
	getRaw, err := v.Encode(get)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	absent, err := service.Get(ctx, getRaw, &subject)
	if err != nil {
		t.Fatal(upgradeCause("legacy "+old.Name, err))
	}
	if _, ok := absent.AsResultUnavailable(); !ok {
		t.Fatal("unsupported old binding created a business Decision")
	}
	claim, err := service.Claim(ctx)
	if err != nil || claim != nil {
		t.Fatal("legacy retirement or unsupported command left a runnable job", upgradeCause("final Claim", err))
	}
}

func sameUpgradeJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
func setUpgradeProcessBounds(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

func confirmUpgradeGroupExit(pid int) error {
	until := time.Now().Add(time.Second)
	for {
		err := syscall.Kill(-pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil {
			return err
		}
		if !time.Now().Before(until) {
			return errors.New("historical process group still exists")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type upgradeOutput struct {
	mu    sync.Mutex
	bytes []byte
}

func (w *upgradeOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if available := 32*1024 - len(w.bytes); available > 0 {
		if len(p) > available {
			p = p[:available]
		}
		w.bytes = append(w.bytes, p...)
	}
	return n, nil
}

func (w *upgradeOutput) safeText(dsn string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	text := string(w.bytes)
	if dsn != "" {
		text = strings.ReplaceAll(text, dsn, "[connection redacted]")
	}
	if parsed, err := url.Parse(dsn); err == nil && parsed.User != nil {
		for _, secret := range []string{parsed.User.Username(), parsed.User.String()} {
			if secret != "" {
				text = strings.ReplaceAll(text, secret, "[credential redacted]")
			}
		}
		if secret, ok := parsed.User.Password(); ok && secret != "" {
			text = strings.ReplaceAll(text, secret, "[credential redacted]")
		}
	}
	return text
}

func restoreFrozenWriter(t *testing.T) string {
	return restoreFrozenProduction(t, false, nil)
}

// restoreFinal01Writer restores only the byte-exact final01 production closure.
// Each consuming ticket supplies its own bounded original public-API driver;
// that driver is never made part of the immutable archived production source.
func restoreFinal01Writer(t *testing.T, driver []byte) string {
	t.Helper()
	if len(driver) == 0 || len(driver) > 32*1024 {
		t.Fatal("final01 driver exceeds finite bound")
	}
	return restoreFrozenProduction(t, true, driver)
}

// Exactly two historical archives are supported. This mechanical restore has
// no business state or arbitrary source/version registration.
func restoreFrozenProduction(t *testing.T, final01 bool, driver []byte) string {
	t.Helper()
	archive, format, commit := "legacy-970fd90", "lerna-legacy-decision-closure-1", "970fd90260b5c4936cdb5c2b7a8589623126a5c2"
	fileCount, productionBytes, payloadBytes := 69, 422534, 430396
	if final01 {
		archive, format, commit = "legacy-final01", "lerna-final01-production-closure-1", "696ac49846105a16f33e5de86dc621a3858651b2"
		fileCount, productionBytes, payloadBytes = 68, 447637, 447637
	}
	root := filepath.Join("testdata", archive)
	sums, err := os.ReadFile(filepath.Join(root, "SHA256SUMS"))
	if err != nil || len(sums) > 16*1024 {
		t.Fatal("frozen checksum list unavailable", upgradeCause("checksum read", err))
	}
	checked := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != 64 || !safeUpgradePath(fields[1]) || checked[fields[1]] {
			t.Fatal("invalid frozen checksum manifest")
		}
		body, err := readUpgradeBounded(filepath.Join(root, fields[1]), 1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != fields[0] {
			t.Fatal("frozen historical checksum mismatch")
		}
		checked[fields[1]] = true
	}
	body, err := os.ReadFile(filepath.Join(root, "provenance.json"))
	if err != nil || !checked["provenance.json"] {
		t.Fatal("frozen provenance missing", upgradeCause("provenance read", err))
	}
	var provenance frozenProvenance
	if err = json.Unmarshal(body, &provenance); err != nil || provenance.Format != format || provenance.SourceCommit != commit || len(provenance.Files) != fileCount || provenance.ProductionFileCount != 68 || provenance.ProductionBytes != productionBytes || provenance.PayloadBytes != payloadBytes {
		t.Fatal("frozen historical provenance mismatch", upgradeCause("provenance decode", err))
	}
	registry := os.Getenv("LERNA_TEST_OWNED_SCOPE_REGISTRY")
	if !filepath.IsAbs(registry) {
		t.Fatal("absolute root owned registry required")
	}
	dir, err := os.MkdirTemp("", "lerna-03-legacy-upgrade-")
	if err != nil {
		t.Fatal(err)
	}
	// No child exists yet. Protect this exact successful creation before any
	// subsequent registration, fsync or archive restore operation can fail.
	success := false
	defer func() {
		if !success {
			if err := os.RemoveAll(dir); err != nil {
				t.Error("exact historical restore cleanup:", err)
			}
		}
	}()
	ledger, err := os.OpenFile(registry, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := fmt.Fprintln(ledger, "directory", dir)
	err = errors.Join(writeErr, ledger.Sync(), ledger.Close())
	if err != nil {
		t.Fatal(err)
	}
	parent, err := os.Open(filepath.Dir(registry))
	if err != nil {
		t.Fatal(err)
	}
	err = errors.Join(parent.Sync(), parent.Close())
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	production := 0
	actualProductionBytes := 0
	for _, entry := range provenance.Files {
		if !safeUpgradePath(entry.Artifact) || !safeUpgradePath(entry.Destination) || !checked[entry.Artifact] || entry.Artifact != entry.Destination+".txt" {
			t.Fatal("unsafe historical restore path")
		}
		content, err := readUpgradeBounded(filepath.Join(root, entry.Artifact), 1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(content)
		if len(content) != entry.Bytes || hex.EncodeToString(hash[:]) != entry.SHA256 {
			t.Fatal("historical provenance content mismatch")
		}
		total += len(content)
		if total > 1024*1024 {
			t.Fatal("historical closure exceeds bound")
		}
		switch entry.Role {
		case "archived_production":
			production++
			actualProductionBytes += len(content)
		case "added_driver":
			if final01 || entry.Destination != "conformance/internal/decisionfixture/legacy_writer_test.go" {
				t.Fatal("unexpected historical driver")
			}
		default:
			t.Fatal("unknown frozen provenance role")
		}
		destination := filepath.Join(dir, entry.Destination)
		if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(destination, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if total != provenance.PayloadBytes || production != 68 || actualProductionBytes != productionBytes {
		t.Fatal("historical closure accounting mismatch")
	}
	if final01 {
		destination := filepath.Join(dir, "conformance", "internal", "decisionfixture", "final01_writer_test.go")
		if err := os.WriteFile(destination, driver, 0600); err != nil {
			t.Fatal("final01 consumer driver write:", err)
		}
	}
	success = true
	return dir
}
func safeUpgradePath(path string) bool {
	return path != "" && !filepath.IsAbs(path) && filepath.Clean(path) == path && path != ".." && !strings.HasPrefix(path, "../")
}
func readUpgradeBounded(path string, max int64) (body []byte, result error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, file.Close()) }()
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("bounded frozen file stat: %w", err)
	}
	if !stat.Mode().IsRegular() || stat.Size() > max {
		return nil, errors.New("invalid bounded frozen file")
	}
	return io.ReadAll(io.LimitReader(file, max+1))
}

// Database diagnostics retain their original cause while printing only the
// cleanup stage; connection configuration and driver credentials are private.
type upgradeStageError struct {
	stage string
	cause error
}

func (e *upgradeStageError) Error() string {
	message := "historical upgrade " + e.stage + " failed"
	if errors.Is(e.cause, context.Canceled) {
		return message + ": context cancelled"
	}
	if errors.Is(e.cause, context.DeadlineExceeded) {
		return message + ": deadline exceeded"
	}
	var exit *exec.ExitError
	if errors.As(e.cause, &exit) {
		return fmt.Sprintf("%s: exit status %d", message, exit.ExitCode())
	}
	var errno syscall.Errno
	if errors.As(e.cause, &errno) {
		return fmt.Sprintf("%s: errno %d", message, errno)
	}
	return message
}
func (e *upgradeStageError) Unwrap() error { return e.cause }
func upgradeCause(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &upgradeStageError{stage: stage, cause: err}
}

func cleanupUpgradeScopes(registry string) (result error) {
	body, err := readUpgradeBounded(registry, 4096)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "postgres" || !testIdentifier.MatchString(fields[1]) || len(names) >= 10 {
			return errors.New("invalid exact old CREATE registry")
		}
		names[fields[1]] = true
		if err = RegisterOwnedSchema(fields[1]); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", os.Getenv("LERNA_TEST_POSTGRES_DSN"))
	if err != nil {
		return upgradeCause("cleanup open", err)
	}
	defer func() { result = errors.Join(result, upgradeCause("cleanup close", db.Close())) }()
	db.SetMaxOpenConns(1)
	for name := range names {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return upgradeCause("cleanup begin", err)
		}
		_, err = tx.ExecContext(ctx, "SET LOCAL statement_timeout='2s'; SET LOCAL lock_timeout='1s'")
		if err == nil {
			_, err = tx.ExecContext(ctx, `DROP SCHEMA IF EXISTS "`+name+`" CASCADE`)
		}
		if err != nil {
			return errors.Join(upgradeCause("cleanup exec", err), upgradeCause("cleanup rollback", tx.Rollback()))
		}
		if err = tx.Commit(); err != nil {
			return upgradeCause("cleanup commit", err)
		}
	}
	return nil
}

// openUpgradePipes owns only this command's four explicit pipe ends before
// successful Start. Its cleanup is retained even if the second allocation fails.
func openUpgradePipes(child *exec.Cmd) (io.WriteCloser, io.ReadCloser, func() error, error) {
	stdin, err := child.StdinPipe()
	if err != nil {
		return nil, nil, nil, upgradeCause("stdin pipe allocation", err)
	}
	stdinChild := child.Stdin.(io.Closer) // StdinPipe itself assigned this OS file.
	var stdout io.ReadCloser
	var stdoutChild io.Closer
	var once sync.Once
	var closeErr error
	cleanup := func() error {
		once.Do(func() {
			closeErr = errors.Join(upgradeCause("pre-start stdin parent close", stdin.Close()), upgradeCause("pre-start stdin child close", stdinChild.Close()))
			if stdout != nil {
				closeErr = errors.Join(closeErr, upgradeCause("pre-start stdout parent close", stdout.Close()), upgradeCause("pre-start stdout child close", stdoutChild.Close()))
			}
		})
		return closeErr
	}
	stdout, err = child.StdoutPipe()
	if err != nil {
		return stdin, nil, cleanup, upgradeCause("stdout pipe allocation", err)
	}
	stdoutChild = child.Stdout.(io.Closer) // StdoutPipe itself assigned this OS file.
	return stdin, stdout, cleanup, nil
}
