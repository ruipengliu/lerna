// Package storecontract exercises the shared MemoryStore behavioral contract.
// Factories MUST provide disposable empty stores; this is not a live-data probe.
// Passing these checks does not by itself prove process-crash durability.
package storecontract

import (
	"context"
	"fmt"
	"lerna/memory"
	"time"
)

type Factory func(context.Context) (memory.QueryStore, func() error, error)

func Cases() []string {
	return []string{"atomic-history", "live-revisions", "original-identity", "concurrent-correction", "fixed-disclosure", "cancelled-commit"}
}
func seed() memory.Change {
	return memory.Change{OperationID: "original", Subject: "alice", SemanticSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Record: memory.Revision{Ref: memory.Ref{Namespace: "contract", Collection: "personal", Key: "format"}, Revision: 1, Document: []byte(`{"text":"concise"}`)}}
}
func Check(ctx context.Context, open Factory, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if open == nil {
		return fmt.Errorf("missing store factory")
	}
	store, cleanup, e := open(ctx)
	if e != nil {
		return e
	}
	if store == nil || cleanup == nil {
		return fmt.Errorf("incomplete store factory")
	}
	defer cleanup()
	first := seed()
	if name == "cancelled-commit" {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, e = store.Commit(cancelled, first); e == nil {
			return fmt.Errorf("cancelled commit succeeded")
		}
		if _, e = store.LookupOperation(ctx, first.Record.Ref.Namespace, first.OperationID); e != memory.Missing {
			return fmt.Errorf("cancelled commit retained an operation")
		}
		return nil
	}
	receipt, e := store.Commit(ctx, first)
	if e != nil {
		return e
	}
	if receipt.Ref != first.Record.Ref || receipt.Revision != 1 || receipt.Position != 1 || receipt.Subject != first.Subject || receipt.SemanticSHA256 != first.SemanticSHA256 {
		return fmt.Errorf("initial receipt differs from the committed intent")
	}
	second := first
	second.OperationID = "correction"
	second.Expected = 1
	second.Record.Revision = 2
	second.Record.Document = []byte(`{"text":"detailed"}`)
	switch name {
	case "live-revisions":
		if _, e = store.Commit(ctx, second); e != nil {
			return e
		}
		refs := []memory.VersionRef{{Ref: first.Record.Ref, Revision: 1}, {Ref: first.Record.Ref, Revision: 2}}
		if e = store.ValidateVersions(ctx, refs); e != nil {
			return fmt.Errorf("retained history failed liveness: %w", e)
		}
		refs[1].Revision = 3
		if e = store.ValidateVersions(ctx, refs); e != memory.Missing {
			return fmt.Errorf("missing batch member treated as live: %v", e)
		}
		refs[1].Revision = 2
		refs[1].Ref.Namespace = "other"
		if e = store.ValidateVersions(ctx, refs); e != memory.Missing {
			return fmt.Errorf("cross namespace revision substituted: %v", e)
		}
	case "atomic-history":
		next, e := store.Commit(ctx, second)
		if e != nil {
			return e
		}
		if next.Revision != 2 || next.Position != 2 {
			return fmt.Errorf("correction position is not atomic")
		}
		old, e := store.Read(ctx, first.Record.Ref, 1)
		if e != nil || string(old.Document) != `{"text":"concise"}` {
			return fmt.Errorf("original revision was not preserved")
		}
		current, e := store.Scan(ctx, "contract", "personal")
		if e != nil || len(current) != 1 || current[0].Revision != 2 || string(current[0].Document) != `{"text":"detailed"}` {
			return fmt.Errorf("scan did not return the current revision")
		}
		if _, e = store.Read(ctx, first.Record.Ref, 3); e != memory.Missing {
			return fmt.Errorf("exact missing revision was substituted")
		}
		changes, e := store.ReadChanges(ctx, "contract", "personal", 0, 10)
		if e != nil || len(changes) != 2 || changes[0] != receipt || changes[1] != next {
			return fmt.Errorf("changes differ from atomic receipts")
		}
	case "original-identity":
		if _, e = store.Commit(ctx, second); e != nil {
			return e
		}
		retry, e := store.Commit(ctx, first)
		if e != nil || retry != receipt {
			return fmt.Errorf("replay did not precede a new version check")
		}
		old, e := store.LookupOperation(ctx, "contract", first.OperationID)
		if e != nil || old != receipt {
			return fmt.Errorf("original operation was lost")
		}
		first.Record.Document = []byte(`{"text":"changed under same identity"}`)
		if _, e = store.Commit(ctx, first); e != memory.IdentityConflict {
			return fmt.Errorf("identity accepted a changed payload")
		}
		first.OperationID = "stale"
		if _, e = store.Commit(ctx, first); e != memory.Conflict {
			return fmt.Errorf("stale update was accepted")
		}
		if _, e = store.LookupOperation(ctx, "contract", "stale"); e != memory.Missing {
			return fmt.Errorf("failed update left a receipt")
		}
	case "concurrent-correction":
		start := make(chan struct{})
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			in := second
			in.OperationID = fmt.Sprintf("race-%d", i)
			go func() { <-start; _, e := store.Commit(ctx, in); results <- e }()
		}
		close(start)
		success, conflict := 0, 0
		var unexpected error
		for i := 0; i < 2; i++ {
			switch e := <-results; e {
			case nil:
				success++
			case memory.Conflict:
				conflict++
			default:
				unexpected = e
			}
		}
		if unexpected != nil {
			return fmt.Errorf("unexpected competing correction result: %v", unexpected)
		}
		if success != 1 || conflict != 1 {
			return fmt.Errorf("competing corrections did not produce one winner")
		}
		changes, e := store.ReadChanges(ctx, "contract", "personal", 0, 10)
		if e != nil || len(changes) != 2 {
			return fmt.Errorf("competing correction produced extra state")
		}
	case "fixed-disclosure":
		selection := memory.ReadBinding{Namespace: "contract", ID: "read", Subject: "alice", SemanticSHA256: first.SemanticSHA256, PermitID: "permit", Coverage: "complete", Results: []memory.VersionRef{{Ref: first.Record.Ref, Revision: 1}}}
		if _, e = store.BindRead(ctx, selection); e != nil {
			return e
		}
		if _, e = store.Commit(ctx, second); e != nil {
			return e
		}
		selection.Results[0].Revision = 2
		retry, e := store.BindRead(ctx, selection)
		if e != nil || len(retry.Results) != 1 || retry.Results[0].Revision != 1 {
			return fmt.Errorf("read retry expanded its selection")
		}
		selection.ID = "empty"
		selection.Results = nil
		if _, e = store.BindRead(ctx, selection); e != nil {
			return e
		}
		selection.Results = []memory.VersionRef{{Ref: first.Record.Ref, Revision: 2}}
		empty, e := store.BindRead(ctx, selection)
		if e != nil || len(empty.Results) != 0 {
			return fmt.Errorf("empty disclosure changed on retry")
		}
		selection.PermitID = "another-permit"
		if _, e = store.BindRead(ctx, selection); e != memory.IdentityConflict {
			return fmt.Errorf("read identity accepted a different permit")
		}
	default:
		return fmt.Errorf("unknown store contract case")
	}
	return nil
}
