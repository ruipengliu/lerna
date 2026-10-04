//go:build integration

package decisionfixture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	decisionpg "github.com/ruipengliu/lerna/adapters/postgres/decision_engine"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

type final01ControlPublication struct {
	Key     string         `json:"key"`
	Body    []byte         `json:"body"`
	Sources []v.ContentRef `json:"sources"`
	Ref     v.ContentRef   `json:"ref"`
}
type final01ControlCase struct {
	upgradeCase
	Planned []final01ControlPublication `json:"planned"`
}
type final01ControlReady struct {
	Protocol string               `json:"protocol"`
	Cases    []final01ControlCase `json:"cases"`
}

func TestFinal01ControlUpgradePreservesOriginalStatesAndReceipts(t *testing.T) {
	driver, err := readUpgradeBounded(filepath.Join("testdata", "final01_control_writer_test.go.txt"), 32*1024)
	if err != nil {
		t.Fatal(upgradeCause("final01 control driver read", err))
	}
	session := startFrozenWriter(t, restoreFinal01Writer(t, driver), "TestFrozenFinal01ControlWriter")
	var ready final01ControlReady
	decoder := json.NewDecoder(bytes.NewReader(session.Frame))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ready); err != nil {
		t.Fatal(upgradeCause("final01 control frame decode", err))
	}
	if ready.Protocol != "lerna-final01-control-696ac49-1" || len(ready.Cases) != 6 {
		t.Fatal("final01 control protocol mismatch")
	}
	created, err := readUpgradeBounded(session.Registry, 4096)
	if err != nil {
		t.Fatal(upgradeCause("final01 exact CREATE registry read", err))
	}
	acknowledged := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(created)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "postgres" || !testIdentifier.MatchString(fields[1]) || acknowledged[fields[1]] {
			t.Fatal("invalid final01 exact CREATE acknowledgement")
		}
		acknowledged[fields[1]] = true
	}
	if len(acknowledged) != 12 {
		t.Fatal("final01 exact owner scope count mismatch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	seen := map[string]bool{}
	for _, old := range ready.Cases {
		if seen[old.Name] || !old.WritersDrained || !acknowledged[old.SourceSchema] || !acknowledged[old.DecisionSchema] || old.SourceSchema == old.DecisionSchema {
			t.Fatal("unconfirmed final01 source/Decision handoff")
		}
		switch old.Name {
		case "accepted", "running", "prepared_waiting", "publication_running", "completed", "failed":
		default:
			t.Fatal("unknown final01 control state")
		}
		seen[old.Name] = true
		verifyFinal01ControlCase(t, ctx, old, session)
	}
}

func verifyFinal01ControlCase(t *testing.T, ctx context.Context, old final01ControlCase, session *frozenWriterSession) {
	t.Helper()
	subject, err := v.Decode[v.SubjectBinding](old.Subject)
	if err != nil {
		t.Fatal(upgradeCause("final01 original subject decode", err))
	}
	request, err := v.Decode[v.DecisionDecideRequest](old.Request)
	if err != nil {
		t.Fatal(upgradeCause("final01 original request decode", err))
	}
	cfg := postgres.Config{DSN: os.Getenv("LERNA_TEST_POSTGRES_DSN"), Schema: old.DecisionSchema, MaxOpenConnections: 4, TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second}
	var store *decisionpg.Store
	var source *Store
	closeOwners := func() error {
		var joined error
		if store != nil {
			joined = errors.Join(joined, upgradeCause("final01 Decision close", store.Close()))
		}
		if source != nil {
			joined = errors.Join(joined, upgradeCause("final01 Source close", source.Close()))
		}
		if joined != nil {
			session.CurrentClosed = false
		}
		return joined
	}
	t.Cleanup(func() {
		if err := closeOwners(); err != nil {
			t.Error(err)
		}
	})
	openOwners := func() {
		var err error
		store, err = decisionpg.Open(ctx, cfg)
		if err != nil {
			t.Fatal(upgradeCause("final01 Decision open", err))
		}
		sourceCfg := cfg
		sourceCfg.Schema = old.SourceSchema
		source, err = Open(ctx, sourceCfg, old.SourceOwner)
		if err != nil {
			t.Fatal(upgradeCause("final01 Source open", err))
		}
		if err = store.Migrate(ctx); err != nil {
			t.Fatal(upgradeCause("final01 Decision migrate", err))
		}
		if err = source.Migrate(ctx); err != nil {
			t.Fatal(upgradeCause("final01 Source migrate", err))
		}
	}
	openOwners()
	versions, err := store.MigrationVersions(ctx)
	if err != nil {
		t.Fatal(upgradeCause("final01 Decision ledger", err))
	}
	if len(old.Migrations) != 2 || len(versions) != 3 || versions[2].Version != 3 {
		t.Fatal("final01 append0003 ledger absent")
	}
	for i, prior := range old.Migrations {
		if versions[i].Version != prior.Version || versions[i].Checksum != prior.Checksum {
			t.Fatal("final01 original Decision migration bytes changed")
		}
	}
	build := func() *decision.Service {
		s, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: request.Target.TenantID, OwnerID: request.Target.OwnerID}, Store: store, Authority: source, ControlAuthority: source, Source: source, Publisher: source, Component: request.Payload.ComponentRef, Worker: "final01-control-upgrade", Lease: time.Second, PoolControl: true})
		if err != nil {
			t.Fatal(upgradeCause("final01 current component assembly", err))
		}
		return s
	}
	s := build()
	read := func() v.DecisionGetResponseFound {
		response, err := s.Get(ctx, old.GetRequest, &subject)
		if err != nil {
			t.Fatal(upgradeCause("final01 public Decision read", err))
		}
		found, ok := response.AsFound()
		if !ok {
			t.Fatal("final01 original public Decision unavailable")
		}
		return found
	}
	before := read()
	encoded, err := v.Encode(before.Decision)
	if err != nil {
		t.Fatal(upgradeCause("final01 precontrol Decision encode", err))
	}
	if string(encoded) != string(old.DecisionBefore) || before.CurrentControl != nil {
		t.Fatal("upgrade rewrote original Decision or invented adopted control")
	}
	originalReceipt := func() {
		result, err := s.GetCommand(ctx, old.CommandGetRequest, &subject)
		if err != nil {
			t.Fatal(upgradeCause("final01 original command read", err))
		}
		found, ok := result.AsFound()
		if !ok {
			t.Fatal("final01 original command metadata was not readable")
		}
		receipt, err := v.Encode(found.Receipt)
		if err != nil || string(receipt) != string(old.AcceptedReceipt) {
			t.Fatal("final01 original receipt changed", upgradeCause("final01 receipt encode", err))
		}
	}
	originalReceipt()
	digest, err := v.DecisionInputDigest(request, subject)
	if err != nil {
		t.Fatal(upgradeCause("final01 original input digest", err))
	}
	until, err := time.Parse("2006-01-02T15:04:05.000000Z", string(request.AcceptBefore))
	if err != nil {
		t.Fatal(upgradeCause("final01 original command deadline", err))
	}
	ref := request.Target
	if err = source.SeedControlAccess(ctx, decision.ControlAccess{Subject: subject, DecisionOwner: v.OwnerRef{TenantID: ref.TenantID, OwnerID: ref.OwnerID}, DecisionRef: &ref, Purposes: []string{"cancel", "get"}, ValidUntil: until}); err != nil {
		t.Fatal(upgradeCause("final01 current control access", err))
	}
	basis, err := source.IssueControl(ctx, ControlClaim{Subject: subject, DecisionRef: ref, TaskRef: request.Payload.TaskRef, InputDigest: v.SchemaDigest(digest), ControlRevision: "2", ValidUntil: request.AcceptBefore})
	if err != nil {
		t.Fatal(upgradeCause("final01 real control issue", err))
	}
	control := v.DecisionCancelRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: v.ID("final01-cancel-" + old.Name), Target: ref, Method: "decision_engine.cancel", AcceptBefore: request.AcceptBefore, Payload: v.DecisionCancelPayload{DecisionRef: ref, TaskRef: request.Payload.TaskRef, DecisionInputDigest: v.SchemaDigest(digest), ControlBasis: basis, Reason: "new owner adopts stop after drained final01 writer"}}
	controlBytes, err := v.Encode(control)
	if err != nil {
		t.Fatal(upgradeCause("final01 control encode", err))
	}
	result, err := s.Cancel(ctx, controlBytes, &subject)
	if err != nil {
		t.Fatal(upgradeCause("final01 legal cancel", err))
	}
	received, ok := result.AsReceived()
	if !ok {
		t.Fatal("final01 control receipt unavailable")
	}
	if _, ok = received.Receipt.AsApplied(); !ok {
		t.Fatal("final01 legal stop was not applied")
	}
	after := read()
	stopped, err := v.Encode(after.Decision)
	if err != nil {
		t.Fatal(upgradeCause("final01 stopped Decision encode", err))
	}
	if old.Name == "completed" || old.Name == "failed" {
		if string(stopped) != string(old.DecisionBefore) {
			t.Fatal("stop rewrote final01 terminal bytes")
		}
	} else {
		closed, ok := after.Decision.AsCancelled()
		if !ok || closed.Input == nil {
			t.Fatal("final01 active work did not retain its original cancelled input")
		}
		prior, err := v.Decode[v.Decision](old.DecisionBefore)
		if err != nil {
			t.Fatal(upgradeCause("final01 original Decision decode", err))
		}
		var usage v.DecisionUsage
		if x, ok := prior.AsAccepted(); ok {
			usage = x.Usage
		} else if x, ok := prior.AsRunning(); ok {
			usage = x.Usage
		} else if x, ok := prior.AsWaiting(); ok {
			usage = x.Usage
		} else {
			t.Fatal("unknown original active public state")
		}
		if closed.Usage != usage {
			t.Fatal("stop changed final01 measured usage or start fee")
		}
	}
	if after.CurrentControl == nil || after.CurrentControl.ControlBasis.ControlRevision != "2" {
		t.Fatal("upgraded Source proof was not adopted by actual stop owner")
	}
	permit, err := source.Authorize(ctx, subject, ref, "get", nil)
	if err != nil {
		t.Fatal(upgradeCause("final01 original publication permission", err))
	}
	for i, published := range old.PublishedRefs {
		if i >= len(old.PublishedBodies) {
			t.Fatal("final01 publication frame incomplete")
		}
		body, err := source.ReadPublished(ctx, published, permit)
		if err != nil || string(body) != old.PublishedBodies[i] {
			t.Fatal("stop erased final01 independent publication", upgradeCause("final01 publication read", err))
		}
	}
	for _, planned := range old.Planned {
		exists, err := source.PublicationExists(ctx, planned.Key, planned.Body, planned.Sources, permit)
		expected := old.Name == "publication_running" || old.Name == "completed"
		if err != nil || exists != expected {
			t.Fatal("stop changed exact final01 prepared publication boundary", upgradeCause("final01 exact publication observation", err))
		}
	}
	originalReceipt()
	if err = closeOwners(); err != nil {
		t.Fatal(err)
	}
	store = nil
	source = nil
	openOwners()
	s = build()
	again := read()
	reopened, err := v.Encode(again.Decision)
	if err != nil || string(reopened) != string(stopped) || again.CurrentControl == nil || !reflect.DeepEqual(again.CurrentControl, after.CurrentControl) {
		t.Fatal("upgraded stop changed across actual two-owner reopen", upgradeCause("final01 reopened Decision encode", err))
	}
	replay, err := s.Cancel(ctx, controlBytes, &subject)
	if err != nil {
		t.Fatal(upgradeCause("final01 control replay", err))
	}
	fixed, ok := replay.AsReceived()
	if !ok {
		t.Fatal("new metadata control receipt lost after reopen")
	}
	first, err := v.Encode(received.Receipt)
	if err != nil {
		t.Fatal(upgradeCause("final01 first control receipt encode", err))
	}
	second, err := v.Encode(fixed.Receipt)
	if err != nil || string(first) != string(second) {
		t.Fatal("final01 applied control receipt changed after reopen", upgradeCause("final01 replay receipt encode", err))
	}
	originalReceipt()
	claim, err := s.Claim(ctx)
	if err != nil || claim != nil {
		t.Fatal("final01 cancelled/terminal Job revived", upgradeCause("final01 claim", err))
	}
	if err = closeOwners(); err != nil {
		t.Fatal(err)
	}
	store = nil
	source = nil
}
