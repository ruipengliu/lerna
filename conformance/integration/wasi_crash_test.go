//go:build linux

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/adapters/wasi"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

type crashWASIConfig struct {
	WorkerRoot, WorkerPath, WorkerHash, DatabasePath, ContentRoot, ContentOwner string
	Scope                                                                       rt.Scope
	Auth                                                                        rt.Auth
	Postgres                                                                    bool
}

// 边界是真实宿主进程SIGKILL、内核进程树及公开账本；不注入执行结果或读私有SQL。
func TestRestrictedWASIHostCrashFencesOriginalProcessWithoutReplayingAttempt(t *testing.T) {
	f, host, cfg := newWASIFixtureConfig(t)
	limits := wasi.DefaultLimits()
	for i := range limits {
		if limits[i].Unit == "cpu_seconds" {
			limits[i].Value = "5"
		}
		if limits[i].Unit == "wall_millis" {
			limits[i].Value = "10000"
		}
	}
	env := wasiEnvironment(t, f, host, limits)
	originalNS := *env.NamespaceRef
	invoke := wasiInvoke(t, f, env, wasiLoopModule())
	f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	input := crashWASIConfig{WorkerRoot: cfg.Root, WorkerPath: cfg.WorkerPath, WorkerHash: cfg.WorkerHash, DatabasePath: f.databasePath, ContentRoot: f.content.root, ContentOwner: f.content.owner, Scope: f.sc, Auth: f.auth, Postgres: f.postgresDSN != ""}
	path := filepath.Join(t.TempDir(), "host-config.json")
	if err := os.WriteFile(path, api.Raw(input), 0600); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestRestrictedWASICrashHelper$")
	child.Env = append(os.Environ(), "HARNESS_WASI_CRASH_CONFIG="+path)
	logPath := filepath.Join(t.TempDir(), "helper.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	child.Stdout, child.Stderr = log, log
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child.ProcessState == nil {
			child.Process.Kill()
			child.Wait()
		}
	})
	// race runner 下独立宿主先编译登记全部闭合 Schema；这里只放宽有界观测等待，
	// 原 invoke/control/claim/CPU/wall 的业务期限全部保持原值。
	deadline := time.Now().Add(25 * time.Second)
	var family map[int]string
	for {
		f.query(t, "environment.get", env.EnvironmentID, domain.EnvironmentIDInput{EnvironmentID: env.EnvironmentID}, &env)
		if !env.ReadyForCell && len(env.ActiveOperationIDs) == 1 {
			family = kernelChildren(child.Process.Pid)
			foundGuest := false
			for pid := range family {
				b, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
				foundGuest = foundGuest || strings.TrimSpace(string(b)) == "worker"
			}
			if foundGuest {
				break
			}
		}
		if time.Now().After(deadline) {
			child.Process.Kill()
			child.Wait()
			b, _ := os.ReadFile(logPath)
			if len(b) > 4096 {
				b = b[:4096]
			}
			t.Fatalf("original host never started an actual isolated WASM process: %s family=%v %+v", b, family, env)
		}
		time.Sleep(5 * time.Millisecond)
	}
	var started domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &started)
	if len(started.Attempts.Items) != 1 || started.Operation.Effect != "unknown" {
		t.Fatalf("original barrier was not durable %+v", started)
	}
	originalAttempt := started.Attempts.Items[0].AttemptID
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("host was not actually killed")
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		alive := false
		for pid, start := range family {
			current, live := kernelIdentity(pid)
			alive = alive || live && current == start
		}
		if !alive {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old sandbox survived real host SIGKILL")
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.restartStore(t)
	cfg.Store = f.st
	reopened, err := wasi.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	mountWASI(t, f, reopened, f.content)
	f.command(t, "execution.reconcile", invoke.OperationID, domain.ReconcileInput{OperationID: invoke.OperationID}, nil)
	f.drain(t)
	var view domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
	f.query(t, "environment.get", env.EnvironmentID, domain.EnvironmentIDInput{EnvironmentID: env.EnvironmentID}, &env)
	if view.Operation.Effect != "unknown" || view.Operation.UsageFinal || len(view.Operation.Usage) != 0 || !view.ActuallyStopped || view.Operation.MayApplyLater != false || len(view.Attempts.Items) != 1 || view.Attempts.Items[0].AttemptID != originalAttempt || env.NamespaceRevision != 1 || !api.Equal(*env.NamespaceRef, originalNS) || !env.ReadyForCell || !env.ActuallyExited {
		t.Fatalf("recovery replayed the cell, invented a lost result/fee or failed actual fencing: %+v %+v", view, env)
	}
	var proof wasi.Receipt
	for _, ref := range view.Operation.EvidenceRefs {
		b, e := f.content.ReadBytes(context.Background(), f.sc, f.auth, ref, "verify", "device")
		if e != nil || api.Decode(b, &proof) != nil {
			t.Fatal("original exit proof missing")
		}
	}
	if proof.AttemptID != originalAttempt || proof.Phase != "lost" || proof.SpawnCount != 1 || !proof.SpawnCountKnown || proof.UsageFinal || !proof.ActuallyExited {
		t.Fatalf("original lost process proof %+v", proof)
	}
}

func TestRestrictedWASICrashHelper(t *testing.T) {
	path := os.Getenv("HARNESS_WASI_CRASH_CONFIG")
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var input crashWASIConfig
	if err = json.Unmarshal(b, &input); err != nil {
		t.Fatal(err)
	}
	var store rt.Store
	if input.Postgres {
		store, err = postgres.Open(context.Background(), os.Getenv("HARNESS_TEST_POSTGRES_DSN"))
	} else {
		store, err = sqlite.Open(input.DatabasePath)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	content := &executionContent{root: input.ContentRoot, owner: input.ContentOwner, tenant: input.Scope.TenantID, sources: map[string]api.ContentRef{}}
	host, err := wasi.New(wasi.Config{Root: input.WorkerRoot, WorkerPath: input.WorkerPath, WorkerHash: input.WorkerHash, Store: store, Content: content, Scope: input.Scope, Location: "device", MaxConcurrent: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	svc, err := domain.New(domain.Config{OwnerID: input.Scope.OwnerID, Content: content, Authority: &executionAuthority{}, Location: "device", Drivers: []domain.Driver{host}, EnvironmentAdmission: host.Admission()})
	if err != nil {
		t.Fatal(err)
	}
	registry := rt.NewRegistry()
	if err = svc.Register(registry); err != nil {
		t.Fatal(err)
	}
	works, status, err := store.Claim(context.Background(), input.Scope, api.NewID("holder"), []string{domain.RunJob}, 1, 30*time.Second)
	if err != nil || status != rt.Committed || len(works) != 1 {
		t.Fatalf("original helper claim %s %v", status, err)
	}
	run, _ := registry.Job(domain.RunJob)
	if err = run(context.Background(), store, input.Scope, works[0]); err != nil {
		t.Fatal(err)
	}
}

func kernelIdentity(pid int) (string, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", false
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return "", false
	}
	fields := strings.Fields(string(b)[end+1:])
	if len(fields) < 20 || fields[0] == "Z" {
		return "", false
	}
	return fields[19], true
}
func kernelChildren(parent int) map[int]string {
	result := map[int]string{}
	dir, err := os.Open("/proc")
	if err != nil {
		return result
	}
	entries, _ := dir.ReadDir(8193)
	dir.Close()
	if len(entries) > 8192 {
		return result
	}
	type child struct {
		pid   int
		start string
	}
	children := map[int][]child{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		end := strings.LastIndexByte(string(b), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(b)[end+1:])
		if len(fields) < 20 || fields[0] == "Z" {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		children[ppid] = append(children[ppid], child{pid, fields[19]})
	}
	queue := []int{parent}
	for len(queue) > 0 && len(result) < 16 {
		pid := queue[0]
		queue = queue[1:]
		for _, child := range children[pid] {
			if _, seen := result[child.pid]; !seen {
				result[child.pid] = child.start
				queue = append(queue, child.pid)
			}
		}
	}
	return result
}
