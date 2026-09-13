package extractioncheck

import (
	"context"
	"lerna/adapters/memoryauth"
	"lerna/adapters/sqlitememory"
	"lerna/execution"
	"lerna/extraction"
	"lerna/memory"
	"lerna/schema"
	"path/filepath"
	"testing"
	"time"
)

func triggerSaver(t *testing.T, h *harness, guard *extraction.TriggerGuard) (*sqlitememory.Store, *extraction.Saver) {
	t.Helper()
	d := extraction.SavedSchema()
	schemas, err := schema.New([]schema.Resource{d})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := memoryauth.New(h.auth, h.source, []memoryauth.Collection{{Namespace: "local", Name: "personal", Resource: "root", PolicyRef: "local-extracted", Purpose: "task", Storage: []string{"local"}, Processing: []string{"local"}, Recipients: []string{"local"}, Schemas: map[string]string{d.Type: schema.Digest(d.Document)}}})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlitememory.Open(filepath.Join(h.root, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	service, err := memory.New(store, policy, schemas, h.clock, memory.Config{Location: "local", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	saver, err := extraction.NewSaver(h.candidates, guard, h.source, service, h.clock, memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}, extraction.SaveTarget{Collection: "personal", PolicyRef: "local-extracted", Purpose: "task"}, h.operation)
	if err != nil {
		t.Fatal(err)
	}
	return store, saver
}

// Cancel at the actual boundary between candidate commit and automatic save.
// Both effects use real drivers/services; cancellation mutates the real store.
type cancelBeforeSaving struct {
	execution.Driver
	h       *harness
	trigger string
}

func (d cancelBeforeSaving) Start(ctx context.Context, c execution.Call) error {
	if err := d.Driver.Start(ctx, c); err != nil {
		return err
	}
	return d.h.candidates.CancelTrigger(ctx, "local", "operator", d.trigger)
}

// Apply an actual policy mutation after candidate commit, before Saver runs.
type beforeMemorySave struct {
	execution.Driver
	h      *harness
	change func(context.Context, *harness) error
}

func (d beforeMemorySave) Start(ctx context.Context, c execution.Call) error {
	if err := d.Driver.Start(ctx, c); err != nil {
		return err
	}
	return d.change(ctx, d.h)
}
