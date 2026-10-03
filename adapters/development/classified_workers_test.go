package development_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/development"
	"github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
)

func TestClassifiedWorkerUsesExplicitKindsWithoutTakingPhysicalTargetLocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	c, err := development.InitializeConfig(ctx, filepath.Join(root, "config.json"), root, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	c.TenantID, c.OwnerID, c.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	management, err := development.OpenAppForRole(ctx, c, true, "management")
	if err != nil {
		t.Fatal(err)
	}
	if err = management.Close(); err != nil {
		t.Fatal(err)
	}
	c.WorkerPool = &development.ClassifiedWorkerConfig{PoolID: api.NewID("pool"), JobKinds: []string{"task.control"}, Concurrency: 1}
	worker, err := development.OpenAppForRole(ctx, c, false, "worker")
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if worker.OwnsTargets || worker.Files != nil || worker.Phones != nil {
		t.Fatal("classified cloud worker took physical executor targets")
	}
	files, err := execution.NewManagedFiles(filepath.Join(root, "files"))
	if err != nil {
		t.Fatal("classified worker blocked an independent target owner")
	}
	defer files.Close()
	workCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- worker.Run(workCtx, false, true) }()
	stop()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal("configured classified worker did not run and exit", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("classified worker did not actually exit")
	}
}
