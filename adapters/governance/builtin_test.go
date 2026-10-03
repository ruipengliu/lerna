package governance_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	adapter "github.com/ruipengliu/lerna/adapters/governance"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

// 文件内容端口保存真实不可变字节，不充当 Grant 权威。
type fileContent struct {
	root  string
	scope runtime.Scope
	mu    sync.Mutex
}

func TestBuiltinProcessRecoveryHelper(t *testing.T) {
	if os.Getenv("HARNESS_BUILTIN_RECOVERY_HELPER") != "1" {
		return
	}
	var scope runtime.Scope
	var install domain.Installation
	var req domain.InstanceRequest
	for _, part := range []struct {
		key   string
		value any
	}{{"HARNESS_BUILTIN_SCOPE", &scope}, {"HARNESS_BUILTIN_INSTALL", &install}, {"HARNESS_BUILTIN_INSTANCE", &req}} {
		if e := json.Unmarshal([]byte(os.Getenv(part.key)), part.value); e != nil {
			t.Fatal(e)
		}
	}
	content := &fileContent{root: os.Getenv("HARNESS_BUILTIN_CONTENT"), scope: scope}
	host, e := adapter.NewBuiltinHost(adapter.BuiltinHostConfig{Root: os.Getenv("HARNESS_BUILTIN_ROOT"), Scope: scope, Content: content, Clock: time.Now, Installations: []domain.Installation{install}, ReadinessTTL: time.Minute})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = host.Prepare(context.Background(), install); e != nil {
		t.Fatal(e)
	}
	if ready, e := host.Initialize(context.Background(), req); e != nil || !ready.Ready {
		t.Fatalf("actual helper instance failed: %v", e)
	}
	fmt.Println("INSTANCE_READY")
	select {}
}

func TestBuiltinKilledProcessCannotReviveOriginalInstance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	scope, content, install := fixture(t)
	root := t.TempDir()
	req := domain.InstanceRequest{TargetID: api.NewID("target"), ActivationID: api.NewID("activation"), InstanceID: api.NewID("instance"), Generation: 1, Installation: install, ConfigRef: install.ConfigRef, Deadline: api.Time(time.Now().Add(time.Minute))}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBuiltinProcessRecoveryHelper$")
	cmd.Env = append(os.Environ(), "HARNESS_BUILTIN_RECOVERY_HELPER=1", "HARNESS_BUILTIN_SCOPE="+string(api.Raw(scope)), "HARNESS_BUILTIN_INSTALL="+string(api.Raw(install)), "HARNESS_BUILTIN_INSTANCE="+string(api.Raw(req)), "HARNESS_BUILTIN_ROOT="+root, "HARNESS_BUILTIN_CONTENT="+content.root)
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	line, e := bufio.NewReader(pipe).ReadString('\n')
	if e != nil || line != "INSTANCE_READY\n" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("actual running process unavailable: %q %v", line, e)
	}
	if e = cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	if e = cmd.Wait(); e == nil {
		t.Fatal("helper unexpectedly exited normally")
	}
	host, e := adapter.NewBuiltinHost(adapter.BuiltinHostConfig{Root: root, Scope: scope, Content: content, Clock: time.Now, Installations: []domain.Installation{install}, ReadinessTTL: time.Minute})
	if e != nil {
		t.Fatal(e)
	}
	defer host.Close()
	if _, e = host.Initialize(ctx, req); e == nil {
		t.Fatal("crash started a replacement under original instance ID")
	}
	fence, e := host.Fence(ctx, req)
	if e != nil || !fence.Exited || fence.MayApplyLater {
		t.Fatalf("dead original process not independently fenced: %+v %v", fence, e)
	}
	disposed, e := host.Dispose(ctx, install)
	if e != nil || !disposed.Exited {
		t.Fatalf("actual dead process disposal: %+v %v", disposed, e)
	}
}

func (c *fileContent) Publish(_ context.Context, p adapter.Publication, b []byte) (api.ContentRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	name := filepath.Join(c.root, p.ID)
	if old, err := os.ReadFile(name); err == nil {
		if api.Hash(old) != api.Hash(b) {
			return api.ContentRef{}, fmt.Errorf("immutable conflict")
		}
	} else if err := os.WriteFile(name, b, 0600); err != nil {
		return api.ContentRef{}, err
	}
	return api.ContentRef{TenantID: c.scope.TenantID, OwnerID: c.scope.OwnerID, ContentID: p.ID, Version: 1, Hash: api.Hash(b), ByteLength: uint64(len(b)), MediaType: p.MediaType}, nil
}
func (c *fileContent) Read(_ context.Context, r api.ContentRef, _ string) ([]byte, error) {
	b, e := os.ReadFile(filepath.Join(c.root, r.ContentID))
	if e == nil && (api.Hash(b) != r.Hash || uint64(len(b)) != r.ByteLength) {
		e = fmt.Errorf("exact bytes changed")
	}
	return b, e
}
func fixture(t *testing.T) (runtime.Scope, *fileContent, domain.Installation) {
	t.Helper()
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner")}
	c := &fileContent{root: t.TempDir(), scope: scope}
	artifact, e := c.Publish(context.Background(), adapter.Publication{ID: api.NewID("artifact"), MediaType: "application/json"}, []byte(`{"builtin":"reference-rule-file","version":"1"}`))
	if e != nil {
		t.Fatal(e)
	}
	comp := func(name string) api.ComponentRef {
		return api.ComponentRef{ComponentID: api.NewID(name), Version: "1", Digest: api.Hash([]byte(name))}
	}
	return scope, c, domain.Installation{InstallLockRef: comp("lock"), ConfigRef: comp("config"), PlatformRef: comp("platform"), Artifacts: []api.ContentRef{artifact}, DependencyRefs: []api.ComponentRef{}, ABI: "go-static-v1", Profile: api.Profile, ReadFormats: []string{"v1"}, WriteFormats: []string{"v1"}, TrustedBuiltin: true, IsolationRefs: []api.ContentRef{}}
}

func TestBuiltinInstanceIsExactAndFencedBeforeDisposal(t *testing.T) {
	ctx := context.Background()
	scope, content, install := fixture(t)
	host, e := adapter.NewBuiltinHost(adapter.BuiltinHostConfig{Root: t.TempDir(), Scope: scope, Content: content, Clock: time.Now, Installations: []domain.Installation{install}, ReadinessTTL: time.Minute})
	if e != nil {
		t.Fatal(e)
	}
	defer host.Close()
	altered := install
	altered.ABI = "untrusted-native"
	if _, e = host.Prepare(ctx, altered); e == nil {
		t.Fatal("model self-described trust became allowlisted")
	}
	prepared, e := host.Prepare(ctx, install)
	if e != nil || !prepared.Compatible || !prepared.IsolationVerified {
		t.Fatalf("prepare: %+v %v", prepared, e)
	}
	proof, e := content.Read(ctx, prepared.SelfTestRef, "test")
	if e != nil || len(proof) == 0 {
		t.Fatalf("actual selftest evidence absent: %v", e)
	}
	req := domain.InstanceRequest{TargetID: api.NewID("target"), ActivationID: api.NewID("activation"), InstanceID: api.NewID("instance"), Generation: 1, Installation: install, ConfigRef: install.ConfigRef, Deadline: api.Time(time.Now().Add(time.Minute))}
	first, e := host.Initialize(ctx, req)
	if e != nil || !first.Ready {
		t.Fatalf("initialize: %+v %v", first, e)
	}
	again, e := host.Initialize(ctx, req)
	if e != nil || !api.Equal(first, again) {
		t.Fatalf("original instance restarted: %v", e)
	}
	changed := req
	changed.Generation++
	if _, e = host.Fence(ctx, changed); e == nil {
		t.Fatal("wrong generation fenced original instance")
	}
	residual, e := host.Dispose(ctx, install)
	if e != nil || residual.Exited || len(residual.ResidualRefs) != 1 {
		t.Fatalf("live instance falsely disposed: %+v %v", residual, e)
	}
	fenced, e := host.Fence(ctx, req)
	if e != nil || !fenced.Exited || fenced.MayApplyLater {
		t.Fatalf("actual wait/fence: %+v %v", fenced, e)
	}
	disposed, e := host.Dispose(ctx, install)
	if e != nil || !disposed.Exited || len(disposed.ResidualRefs) != 0 {
		t.Fatalf("dispose: %+v %v", disposed, e)
	}
	if _, e = host.Initialize(ctx, req); e == nil {
		t.Fatal("fenced original instance resurrected")
	}
}
