package credentials_test

import (
	"context"
	"errors"
	"lerna/adapters/filekeys"
	"lerna/adapters/sqlitecredentials"
	"lerna/credentials"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type renewalProvider struct {
	target  credentials.Binding
	sends   int
	unknown bool
}

func (p *renewalProvider) ID() string                  { return "fixture-provider" }
func (p *renewalProvider) Target() credentials.Binding { return p.target }
func (p *renewalProvider) ReplaySafe() bool            { return true }
func (p *renewalProvider) Renew(context.Context, string, []byte) error {
	p.sends++
	return errors.New("lost response with fixture secret")
}
func (p *renewalProvider) Inspect(context.Context, string, []byte) (credentials.RenewalObservation, error) {
	if p.unknown {
		return credentials.RenewalObservation{State: "unknown"}, nil
	}
	return credentials.RenewalObservation{State: "applied", Secret: []byte("new-token"), ExpiresUnix: 1800007200}, nil
}
func TestRenewalReopensAndChecksOriginalOperationWithoutResending(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dir := filepath.Join(root, "keys")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	keys, e := filekeys.Open(dir, 64)
	if e != nil {
		t.Fatal(e)
	}
	defer keys.Close()
	if _, e = keys.Generate(ctx); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "credentials.db")
	db, e := sqlitecredentials.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	c := &clock{time.Unix(1800000000, 0)}
	cfg := credentials.Config{Timeout: time.Second, MaxConcurrent: 4}
	broker, e := credentials.New(db, keys, auth{}, c, cfg)
	if e != nil {
		t.Fatal(e)
	}
	target := credentials.Binding{Namespace: "local", Subject: "alice", Driver: "orders", Service: "orders", Account: "alice", Purpose: "task", Location: "local"}
	if _, e = broker.Put(ctx, "trusted", "token", target, 0, 1800003600, []byte("old-token")); e != nil {
		t.Fatal(e)
	}
	provider := &renewalProvider{target: target}
	manager, e := credentials.NewLifecycle(db, keys, auth{}, c, cfg)
	if e != nil {
		t.Fatal(e)
	}
	manager, e = manager.WithProvider(provider)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = manager.StartRenewal(ctx, "trusted", "renew-1", "token", target); e != nil {
		t.Fatal(e)
	}
	pending, e := manager.StepRenewal(ctx, "trusted", "renew-1", target)
	if e != nil || pending.Phase != "checking" || pending.Attempts != 1 {
		t.Fatal("lost result did not remain checking", e)
	}
	db.Close()
	db, e = sqlitecredentials.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	manager, e = credentials.NewLifecycle(db, keys, auth{}, c, cfg)
	if e != nil {
		t.Fatal(e)
	}
	manager, e = manager.WithProvider(provider)
	if e != nil {
		t.Fatal(e)
	}
	completed, e := manager.StepRenewal(ctx, "trusted", "renew-1", target)
	if e != nil || completed.Phase != "completed" || provider.sends != 1 {
		t.Fatal("renewal was replayed or not reconciled", e)
	}
	broker, e = credentials.New(db, keys, auth{}, c, cfg)
	if e != nil {
		t.Fatal(e)
	}
	sink := &exit{target: target}
	driver, e := broker.Bind(target, sink)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = driver.Use(ctx, "trusted", "token", credentials.Call{OperationID: "use-new"}); e != nil || sink.received != "new-token" {
		t.Fatal("new credential not usable", e)
	}

	provider.unknown = true
	if _, e = manager.StartRenewal(ctx, "trusted", "renew-2", "token", target); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 12; i++ {
		result, e := manager.StepRenewal(ctx, "trusted", "renew-2", target)
		if e != nil {
			t.Fatal(e)
		}
		if i == 11 && (result.Phase != "needs_reconciliation" || result.Checks != 8 || result.Attempts != 1) {
			t.Fatal("unknown lookup budget not enforced")
		}
	}
	if provider.sends != 2 {
		t.Fatal("unknown outcome caused repeat sends")
	}
	if _, e = manager.StartRenewal(ctx, "trusted", "escape-budget", "token", target); e != credentials.Conflict {
		t.Fatal("new identity bypassed unknown outcome", e)
	}
	t.Run("late send retains original reference", func(t *testing.T) {
		if _, e = broker.Put(ctx, "trusted", "parallel-token", target, 0, 1800003600, []byte("old-token")); e != nil {
			t.Fatal(e)
		}
		paused := &pausedRenewalProvider{target: target, entered: make(chan struct{}), release: make(chan struct{})}
		manager, e = manager.WithProvider(paused)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = manager.StartRenewal(ctx, "trusted", "parallel", "parallel-token", target); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() { _, err := manager.StepRenewal(ctx, "trusted", "parallel", target); done <- err }()
		defer func() {
			close(paused.release)
			if err := <-done; err != nil {
				t.Errorf("original send failed: %v", err)
			}
		}()
		select {
		case <-paused.entered:
		case <-time.After(time.Second):
			t.Fatal("send not paused")
		}
		var result credentials.Renewal
		for j := 0; j < 3; j++ {
			result, e = manager.StepRenewal(ctx, "trusted", "parallel", target)
			if e != nil {
				t.Fatal(e)
			}
		}
		if result.Phase != "needs_reconciliation" {
			t.Fatal("in-flight operation was released", result.Phase)
		}
		if _, e = manager.StartRenewal(ctx, "trusted", "new-parallel", "parallel-token", target); e != credentials.Conflict {
			t.Fatal("late send escaped original identity", e)
		}
	})

}

type pausedRenewalProvider struct {
	target           credentials.Binding
	entered, release chan struct{}
	sends            atomic.Int32
}

func (p *pausedRenewalProvider) ID() string                  { return "paused" }
func (p *pausedRenewalProvider) Target() credentials.Binding { return p.target }
func (p *pausedRenewalProvider) ReplaySafe() bool            { return true }
func (p *pausedRenewalProvider) Renew(ctx context.Context, _ string, _ []byte) error {
	if p.sends.Add(1) == 1 {
		close(p.entered)
		select {
		case <-p.release:
		case <-ctx.Done():
			return credentials.Unavailable
		}
	}
	return nil
}
func (p *pausedRenewalProvider) Inspect(context.Context, string, []byte) (credentials.RenewalObservation, error) {
	return credentials.RenewalObservation{State: "not_occurred"}, nil
}
