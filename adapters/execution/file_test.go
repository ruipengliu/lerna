package execution_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
)

func TestManagedFileWritesVersionCASAndIndependentReadback(t *testing.T) {
	root := t.TempDir()
	driver, err := target.NewManagedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	request := target.FileWrite{OperationID: api.NewID("operation"), AttemptID: api.NewID("attempt"), Path: "report.md", ExpectedVersion: "absent", Data: []byte("verified report\n")}
	receipt, err := driver.Write(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Effect != "applied" || receipt.MayApplyLater {
		t.Fatalf("unexpected effect: %+v", receipt)
	}
	actual, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil || string(actual) != "verified report\n" {
		t.Fatalf("target truth %q, %v", actual, err)
	}
	observed, err := driver.Read(context.Background(), "report.md")
	if err != nil || observed.Version != receipt.Version || string(observed.Data) != "verified report\n" {
		t.Fatalf("readback: %+v %v", observed, err)
	}
	request.OperationID, request.AttemptID = api.NewID("operation"), api.NewID("attempt")
	request.Data = []byte("second version\n")
	if _, err = driver.Write(context.Background(), request); !api.IsCode(err, "revision_conflict") {
		t.Fatalf("stale CAS allowed: %v", err)
	}
}

func TestManagedFileRejectsLinksAndTraversal(t *testing.T) {
	root := t.TempDir()
	driver, err := target.NewManagedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	outside := filepath.Join(t.TempDir(), "private")
	if err = os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err = os.Link(outside, filepath.Join(root, "hard")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../private", "linked", "hard", ".harness/journals/x"} {
		_, err = driver.Write(context.Background(), target.FileWrite{OperationID: api.NewID("operation"), AttemptID: api.NewID("attempt"), Path: p, ExpectedVersion: "absent", Data: []byte("leak")})
		if !api.IsCode(err, "forbidden") {
			t.Errorf("path %q was not rejected: %v", p, err)
		}
	}
	actual, _ := os.ReadFile(outside)
	if string(actual) != "private" {
		t.Fatal("external file changed")
	}
}

func TestManagedFileRecoversRenameLostReceiptByOriginalIdentity(t *testing.T) {
	root := t.TempDir()
	driver, err := target.NewManagedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	q := target.FileWrite{OperationID: api.NewID("operation"), AttemptID: api.NewID("attempt"), Path: "report", ExpectedVersion: "absent", Data: []byte("committed")}
	driver.Fault = func(stage string) error {
		if stage == "renamed" {
			return context.Canceled
		}
		return nil
	}
	if _, err = driver.Write(context.Background(), q); err == nil {
		t.Fatal("expected lost reply")
	}
	driver.Close()
	driver, err = target.NewManagedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	r, err := driver.Recover(context.Background(), q.AttemptID)
	if err != nil || r.Effect != "applied" || !r.DirectorySynced {
		t.Fatalf("recovery %+v %v", r, err)
	}
	if err = os.WriteFile(filepath.Join(root, "report"), []byte("later writer"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err = driver.Recover(context.Background(), q.AttemptID)
	if err != nil || r.Effect != "applied" {
		t.Fatalf("historical effect rewritten %+v %v", r, err)
	}
}

func TestManagedFileRefusesSecondHostOwner(t *testing.T) {
	root := t.TempDir()
	one, err := target.NewManagedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	two, err := target.NewManagedFiles(root)
	if err == nil {
		two.Close()
		t.Fatal("a second host owner acquired the same root")
	}
}

func TestManagedFileRetainsPathIsolationWhileOriginalEffectIsUnknown(t *testing.T) {
	root := t.TempDir()
	driver, err := target.NewManagedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	q := target.FileWrite{OperationID: api.NewID("operation"), AttemptID: api.NewID("attempt"), Path: "report", ExpectedVersion: "absent", Data: []byte("first")}
	driver.Fault = func(stage string) error {
		if stage == "renamed" {
			return context.Canceled
		}
		return nil
	}
	if _, err = driver.Write(context.Background(), q); err == nil {
		t.Fatal("expected lost reply")
	}
	// 模拟目标外部改变；当前摘要不能证明原写没有发生。
	if err = os.Remove(filepath.Join(root, "report")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "report"), []byte("another version"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := driver.Recover(context.Background(), q.AttemptID)
	if err != nil || recovered.Effect != "unknown" {
		t.Fatalf("unknown target association %+v %v", recovered, err)
	}
	driver.Fault = nil
	_, err = driver.Write(context.Background(), target.FileWrite{OperationID: api.NewID("operation"), AttemptID: api.NewID("attempt"), Path: "report", ExpectedVersion: api.Hash([]byte("another version")), Data: []byte("new action")})
	if !api.IsCode(err, "invalid_state") {
		t.Fatalf("unknown path isolation was released: %v", err)
	}
	actual, _ := os.ReadFile(filepath.Join(root, "report"))
	if string(actual) != "another version" {
		t.Fatalf("unknown path changed %q", actual)
	}
}
