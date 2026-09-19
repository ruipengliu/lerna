package filekeys_test

import (
	"context"
	"lerna/adapters/credentials/filekeys"
	"lerna/credentials"
	"os"
	"testing"
)

func TestPreparedKeySurvivesReopenWithoutActivation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	source, e := filekeys.Open(dir, 16)
	if e != nil {
		t.Fatal(e)
	}
	old, e := source.Generate(ctx)
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := source.Prepare(ctx, "rotation-1")
	if e != nil || prepared == old {
		t.Fatal("new version not prepared", e)
	}
	active, raw, _, e := source.Reserve(ctx)
	clear(raw)
	if e != nil || active != old {
		t.Fatal("preparation activated key", e)
	}
	source.Close()
	source, e = filekeys.Open(dir, 16)
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	again, e := source.Prepare(ctx, "rotation-1")
	if e != nil || again != prepared {
		t.Fatal("restart generated another version", e)
	}
	key, e := source.Read(ctx, prepared)
	clear(key)
	if e != nil {
		t.Fatal("prepared material not durable", e)
	}
}

func TestActivationRequiresPreparedOperationAndRetirementProtectsActiveKey(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	source, e := filekeys.Open(dir, 16)
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	old, e := source.Generate(ctx)
	if e != nil {
		t.Fatal(e)
	}
	next, e := source.Prepare(ctx, "rotation-2")
	if e != nil {
		t.Fatal(e)
	}
	if e = source.Activate(ctx, "other", next); e != credentials.Denied {
		t.Fatal("wrong operation activated", e)
	}
	active, raw, _, e := source.Reserve(ctx)
	clear(raw)
	if e != nil || active != old {
		t.Fatal("failed activation changed key", e)
	}
	if e = source.Activate(ctx, "rotation-2", next); e != nil {
		t.Fatal(e)
	}
	if e = source.Activate(ctx, "rotation-2", next); e != nil {
		t.Fatal("repeat activation", e)
	}
	active, raw, _, e = source.Reserve(ctx)
	clear(raw)
	if e != nil || active != next {
		t.Fatal("activation not applied", e)
	}
	if e = source.Retire(ctx, next); e != credentials.Denied {
		t.Fatal("active key retired", e)
	}
	if e = source.Retire(ctx, old); e != nil {
		t.Fatal(e)
	}
	if _, e = source.Read(ctx, old); e != credentials.KeyUnavailable {
		t.Fatal("retired material available", e)
	}
	if e = source.Retire(ctx, old); e != nil {
		t.Fatal("retirement retry", e)
	}
}
