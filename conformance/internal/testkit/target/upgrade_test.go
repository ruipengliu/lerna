package target_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/conformance/internal/testkit/target"
)

func TestPublishedV1WriterUpgradesWithOriginalFactsAndMigration(t *testing.T) {
	f := newFixture(t)
	base := filepath.Join("..", "..", "..", "fixtures", "deterministic-target", "v1")
	manifest, err := os.ReadFile(filepath.Join(base, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	frozen := map[string][]byte{}
	for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatal("invalid frozen writer manifest")
		}
		raw, err := os.ReadFile(filepath.Join(base, fields[1]))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != fields[0] {
			t.Fatalf("frozen source mismatch: %s", fields[1])
		}
		frozen[fields[1]] = raw
	}
	source := strings.Replace(string(frozen["target.go.txt"]), "package target", "package main", 1)
	source = strings.Replace(source, "//go:embed migrations/0001_target.sql\nvar migration string", "var migration = "+strconv.Quote(string(frozen["0001_target.sql"])), 1)
	files := map[string]string{"target_v1.go": source, "writer_v1.go": strings.Replace(string(frozen["writer_linux.go.txt"]), "package target", "package main", 1), "main_v1.go": v1Main}
	for name, source := range files {
		if err = os.WriteFile(filepath.Join(f.directory, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(f.directory, "v1-writer")
	buildCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", binary, filepath.Join(f.directory, "target_v1.go"), filepath.Join(f.directory, "writer_v1.go"), filepath.Join(f.directory, "main_v1.go"))
	build.WaitDelay = time.Second
	buildConfirmed := false
	f.closers = append(f.closers, func() error {
		if build.Process == nil {
			return nil
		}
		if !buildConfirmed {
			return errors.New("historical build descendants unconfirmed; retain exact scope")
		}
		return nil
	})
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("actual frozen writer build: %v %s", err, output)
	}
	buildConfirmed = true
	runCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	child := exec.CommandContext(runCtx, binary, f.cfg.Path, f.cfg.Identity, strconv.FormatInt(f.now.UnixNano(), 10))
	child.WaitDelay = time.Second
	registerChild(f, child)
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("actual frozen writer: %v %s", err, output)
	}
	var old struct {
		Settings target.Settings
		Fact     target.Fact
	}
	if err = json.Unmarshal(output, &old); err != nil {
		t.Fatalf("v1 observation: %v %s", err, output)
	}
	if len(old.Settings.Migrations) != 1 || old.Settings.Migrations[0].Version != 1 || old.Fact.Value.Version != 1 || !bytes.Equal(old.Fact.Value.Data, []byte{0, 255, 10}) || len(old.Fact.Receives) != 2 {
		t.Fatalf("actual v1 baseline: %+v", old)
	}
	writer := f.open()
	settings, err := writer.Settings(f.ctx)
	if err != nil || len(settings.Migrations) != 2 || settings.Migrations[1].Version != 2 || settings.Migrations[0] != old.Settings.Migrations[0] || settings.DatabaseID != old.Settings.DatabaseID {
		t.Fatalf("v1→v2 migration: %+v %v", settings, err)
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := observer.Observe(f.ctx, "v1-original")
	if err != nil || restored.Pending || !reflectReceiptEqual(restored.Receipt, old.Fact.Receipt) || !reflect.DeepEqual(restored.Receives, old.Fact.Receives) {
		t.Fatalf("v1 original facts changed: %+v %v", restored, err)
	}
	input := target.Request{Key: "v2-original", Resource: "fake-document", Data: []byte("upgraded")}
	plan := target.Plan{ID: "after-upgrade", Seed: 7, Deadline: f.now.Add(time.Hour), Steps: []target.Step{{ID: "receive", Kind: target.ReceiveOnly, Input: input}, {ID: "apply", Kind: target.ApplyReceived, Input: input}}}
	if _, err = writer.InstallPlan(f.ctx, plan); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.RunEvent(f.ctx, plan.ID, "receive"); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.RunEvent(f.ctx, plan.ID, "apply"); err != nil {
		t.Fatal(err)
	}
	value, err := writer.Read(f.ctx, input.Resource)
	if err != nil || value.Version != 2 || string(value.Data) != "upgraded" {
		t.Fatalf("normal new stage after actual upgrade: %+v %v", value, err)
	}
	t.Logf("actual v1→v2: old=%+v new=%+v", old.Settings, settings)
}
func registerChild(f *fixture, cmd *exec.Cmd) {
	f.closers = append(f.closers, func() error {
		if cmd.Process == nil {
			return nil
		}
		if cmd.ProcessState == nil {
			return errors.New("child exit unconfirmed; retain exact scope")
		}
		return nil
	})
}
func reflectReceiptEqual(a, b target.Receipt) bool {
	return a.Key == b.Key && a.Digest == b.Digest && a.Start.Equal(b.Start) && a.Deadline.Equal(b.Deadline) && a.Value.Resource == b.Value.Resource && a.Value.Version == b.Value.Version && bytes.Equal(a.Value.Data, b.Value.Data)
}

const v1Main = `package main
import("context";"encoding/json";"fmt";"os";"strconv";"time")
func main(){
 nanos,err:=strconv.ParseInt(os.Args[3],10,64);if err!=nil{panic(err)}
 ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
 cfg:=Config{Path:os.Args[1],Identity:os.Args[2],Window:time.Minute,QueryMode:QueryEnabled,IOTimeout:time.Second,BusyTimeout:10*time.Millisecond,Now:func()time.Time{return time.Unix(0,nanos).UTC()}}
 writer,err:=Open(ctx,cfg);if err!=nil{panic(err)}
 input:=Request{Key:"v1-original",Resource:"fake-document",Data:[]byte{0,255,10}}
 if _,err=writer.Write(ctx,input);err!=nil{panic(err)};if _,err=writer.Write(ctx,input);err!=nil{panic(err)}
 settings,err:=writer.Settings(ctx);if err!=nil{panic(err)}
 observer,err:=OpenObserver(ctx,ObserverConfig{Path:cfg.Path,Identity:cfg.Identity,IOTimeout:time.Second});if err!=nil{panic(err)}
 fact,err:=observer.Observe(ctx,input.Key);if err!=nil{panic(err)}
 if err=observer.Close();err!=nil{panic(err)};if err=writer.Close();err!=nil{panic(err)}
 raw,err:=json.Marshal(struct{Settings Settings;Fact Fact}{settings,fact});if err!=nil{panic(err)};fmt.Println(string(raw))
}
`
