package executioncheck

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/gob"
	"encoding/json"
	"errors"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/10/authority.gob
var old10Authority []byte

//go:embed testdata/10/target.db
var old10Target []byte

//go:embed testdata/10/checkpoint.json
var old10Checkpoint []byte

//go:embed testdata/10/legacy-content.json
var old10ContentManifest []byte

func TestActualV10ExecutionUpgrade(t *testing.T) {
	testActualV10ExecutionUpgrade(t, "migrate")
}
func TestLegacyExecutionMigrationBoundaries(t *testing.T) {
	for _, mode := range []string{"repeat", "crash-before", "crash-after", "wrong-digest", "wrong-operation", "wrong-subject", "revoked-source", "already-retired"} {
		t.Run(mode, func(t *testing.T) { testActualV10ExecutionUpgrade(t, mode) })
	}
}
func testActualV10ExecutionUpgrade(t *testing.T, mode string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h, e := fresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	var state authorization.State
	if e = gob.NewDecoder(bytes.NewReader(old10Authority)).Decode(&state); e != nil {
		t.Fatal(e)
	}
	snapshot, e := h.db.Load(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.db.Commit(ctx, snapshot.Version, state); e != nil {
		t.Fatal(e)
	}
	var cp checkpoint
	if e = json.Unmarshal(old10Checkpoint, &cp); e != nil {
		t.Fatal(e)
	}
	h.close()
	if e = os.WriteFile(filepath.Join(h.root, "target.db"), old10Target, 0600); e != nil {
		t.Fatal(e)
	}
	reopened, e := open(ctx, h.root, cp.Token)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.close()
	before, e := reopened.client.GetInvocation(ctx, cp.Request.OperationID)
	if e != nil || !before.Started || before.Effect != "UNKNOWN" || before.Async != nil {
		t.Fatal(before, e)
	}
	var migration artifacts.LegacyUse
	if e = json.Unmarshal(old10ContentManifest, &migration); e != nil {
		t.Fatal(e)
	}
	if answers.Reference(&wire.ContentRef{Namespace: migration.Namespace, Key: migration.Key, Revision: migration.Revision}) != cp.Request.InputRef {
		t.Fatal("migration does not match original execution input")
	}
	binding := artifacts.Binding{Token: cp.Token, Namespace: "local", Location: "local", Recipient: "local"}
	allowed := mode == "migrate" || mode == "repeat" || strings.HasPrefix(mode, "crash-")
	switch mode {
	case "wrong-digest":
		migration.RecordSHA256 = strings.Repeat("0", 64)
	case "wrong-operation":
		migration.OperationID = "unrelated"
	case "wrong-subject":
		migration.Subject = "someone-else"
	case "revoked-source":
		if e = reopened.policy.Replace(nil); e != nil {
			t.Fatal(e)
		}
	case "already-retired":
		if _, e = reopened.access.Read(ctx, cp.Token, cp.Request.InputRef, reopened.cap); e == nil {
			t.Fatal("legacy input read without migration")
		}
	}
	if strings.HasPrefix(mode, "crash-") {
		reopened.close()
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		child := exec.CommandContext(ctx, executable, "-test.run=^TestLegacyMigrationCrashProbe$")
		child.Env = append(os.Environ(), "HARNESS_LEGACY_MIGRATION_ROOT="+h.root, "HARNESS_LEGACY_MIGRATION_POINT="+mode)
		out, err := child.CombinedOutput()
		var exit *exec.ExitError
		want := 73
		if mode == "crash-after" {
			want = 74
		}
		if !errors.As(err, &exit) || exit.ExitCode() != want {
			t.Fatalf("migration process: %v %s", err, out)
		}
		reopened, e = open(ctx, h.root, cp.Token)
		if e != nil {
			t.Fatal(e)
		}
		defer reopened.close()
	}
	e = reopened.content.MigrateLegacyUse(ctx, binding, migration)
	if allowed && e != nil {
		t.Fatal(e)
	}
	if !allowed && e == nil {
		t.Fatal("unproven legacy migration accepted")
	}
	if mode == "repeat" {
		reopened.close()
		reopened, e = open(ctx, h.root, cp.Token)
		if e != nil {
			t.Fatal(e)
		}
		defer reopened.close()
		if e = reopened.content.MigrateLegacyUse(ctx, binding, migration); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = reopened.client.Invoke(ctx, cp.Request, ""); e != nil {
		t.Fatal(e)
	}
	after, e := reopened.client.Reconcile(ctx, cp.Request.OperationID)
	if e != nil || after.Effect != "CONFIRMED" {
		t.Fatal(after, e)
	}
	if e = reopened.exec.Drain(ctx, 16); e != nil {
		t.Fatal(e)
	}
	target, e := reopened.target.Snapshot(ctx)
	if e != nil || target.Changes != 1 || target.Value != 3 {
		t.Fatal(target, e)
	}
	final, e := reopened.exec.GetInvocation(ctx, cp.Request.OperationID)
	if e != nil || final.Permit != state.Signed.Uses[cp.Request.OperationID] {
		t.Fatal("old permit changed", e)
	}

	task, e := reopened.core.Get(ctx, cp.Token, cp.Request.Qualification.Ref)
	if e != nil {
		t.Fatal(e)
	}
	if allowed {
		if task.State != "COMPLETED" || after.Result != "SUCCESS" || after.Reference == "" {
			t.Fatal("authorized upgrade incomplete", task.State, after.Result)
		}
	} else {
		if task.State == "COMPLETED" || after.Result != "FAILURE" || after.Reference != "" {
			t.Fatal("unproven input released", task.State, after.Result)
		}
	}
}

func TestLegacyMigrationCrashProbe(t *testing.T) {
	root := os.Getenv("HARNESS_LEGACY_MIGRATION_ROOT")
	if root == "" {
		t.Skip("actual process probe")
	}
	if os.Getenv("HARNESS_LEGACY_MIGRATION_POINT") == "crash-before" {
		os.Exit(73)
	}
	var cp checkpoint
	if err := json.Unmarshal(old10Checkpoint, &cp); err != nil {
		t.Fatal(err)
	}
	var migration artifacts.LegacyUse
	if err := json.Unmarshal(old10ContentManifest, &migration); err != nil {
		t.Fatal(err)
	}
	h, err := open(context.Background(), root, cp.Token)
	if err != nil {
		t.Fatal(err)
	}
	binding := artifacts.Binding{Token: cp.Token, Namespace: "local", Location: "local", Recipient: "local"}
	if err = h.content.MigrateLegacyUse(context.Background(), binding, migration); err != nil {
		t.Fatal(err)
	}
	os.Exit(74)
}
