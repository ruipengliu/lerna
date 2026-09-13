package contextassembly_test

import (
	"context"
	"lerna/adapters/sqlitecontext"
	"lerna/brain"
	"lerna/contextassembly"
	"path/filepath"
	"strings"
	"testing"
)

type facts struct{}

func (facts) Load(context.Context, contextassembly.Request) (brain.Input, error) {
	return brain.Input{Goal: "Compose a reply", Constraints: "Do not change user facts"}, nil
}
func (facts) Validate(context.Context, contextassembly.Request) error { return nil }

type sources struct {
	text   string
	denied bool
	retain bool
}

func (s *sources) Load(context.Context, contextassembly.Request, contextassembly.Reference) (contextassembly.Source, error) {
	if s.denied {
		return contextassembly.Source{}, contextassembly.Denied
	}
	return contextassembly.Source{Block: brain.Block{Ref: "style:1", Text: s.text, Role: "memory", Subject: "alice"}, Applicable: true, CanStore: s.retain}, nil
}
func (s *sources) Validate(context.Context, contextassembly.Request, contextassembly.Reference, bool) error {
	if s.denied {
		return contextassembly.Denied
	}
	return nil
}
func TestAssembleReplaysOnlyOriginalAuthorizedReference(t *testing.T) {
	store, e := sqlitecontext.Open(filepath.Join(t.TempDir(), "context.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	src := &sources{text: "Prefer concise replies"}
	a, e := contextassembly.New(store, facts{}, src)
	if e != nil {
		t.Fatal(e)
	}
	req := contextassembly.Request{Key: contextassembly.Key{Namespace: "local", TaskID: "task", Decision: 1}, Subject: "alice", Purpose: "assist", Location: "model", Storage: "device", FactsVersion: 1, PolicyVersion: "v1", MaxBytes: 8192, Candidates: []contextassembly.Candidate{{Reference: contextassembly.Reference{Namespace: "local", Collection: "personal", Key: "style", Revision: 1}}}}
	first, e := a.Assemble(context.Background(), req)
	if e != nil || len(first.Input.Blocks) != 1 {
		t.Fatalf("first %v %v", first, e)
	}
	saved, e := store.Read(context.Background(), req.Key)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(saved.Document), src.text) {
		t.Fatal("unretained body persisted")
	}
	deps, e := a.Dependencies(context.Background(), req)
	if e != nil || len(deps) != 1 || deps[0].Reference.Key != "style" || deps[0].State != "selected" {
		t.Fatalf("lineage %v %v", deps, e)
	}
	src.text = "Prefer long replies"
	if _, e = a.Assemble(context.Background(), req); e != contextassembly.Invalidated {
		t.Fatalf("changed body silently rebound %v", e)
	}
	src.text = "Prefer concise replies"
	src.denied = true
	if _, e = a.Assemble(context.Background(), req); e != contextassembly.Denied {
		t.Fatalf("revoked body %v", e)
	}
}

type catalogSources struct {
	entries map[string]contextassembly.Source
	revoked map[string]bool
}

func (s *catalogSources) Load(_ context.Context, _ contextassembly.Request, r contextassembly.Reference) (contextassembly.Source, error) {
	if s.revoked[r.Key] {
		return contextassembly.Source{}, contextassembly.Denied
	}
	v, ok := s.entries[r.Key]
	if !ok {
		return contextassembly.Source{}, contextassembly.Missing
	}
	return v, nil
}
func (s *catalogSources) Validate(_ context.Context, _ contextassembly.Request, r contextassembly.Reference, stored bool) error {
	if s.revoked[r.Key] {
		return contextassembly.Denied
	}
	v, ok := s.entries[r.Key]
	if !ok {
		return contextassembly.Missing
	}
	if stored && !v.CanStore {
		return contextassembly.Denied
	}
	return nil
}
func TestRequiredMemoryDisplacesOptionalBeforeBudgetFailure(t *testing.T) {
	store, e := sqlitecontext.Open(filepath.Join(t.TempDir(), "context.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	src := &catalogSources{entries: map[string]contextassembly.Source{}, revoked: map[string]bool{}}
	for _, key := range []string{"optional", "required"} {
		src.entries[key] = contextassembly.Source{Block: brain.Block{Ref: key, Text: strings.Repeat(key, 100), Subject: "alice", Role: "memory"}, Applicable: true, CanStore: true}
	}
	a, e := contextassembly.New(store, facts{}, src)
	if e != nil {
		t.Fatal(e)
	}
	req := contextassembly.Request{Key: contextassembly.Key{Namespace: "local", TaskID: "task", Decision: 1}, Subject: "alice", Purpose: "assist", Location: "model", Storage: "device", FactsVersion: 1, PolicyVersion: "v1", MaxBytes: 1100}
	for _, key := range []string{"optional", "required"} {
		req.Candidates = append(req.Candidates, contextassembly.Candidate{Reference: contextassembly.Reference{Namespace: "local", Collection: "personal", Key: key, Revision: 1}, Required: key == "required"})
	}
	result, e := a.Assemble(context.Background(), req)
	if e != nil || len(result.Input.Blocks) != 1 || result.Input.Blocks[0].Ref != "required" || len(result.Status.Trimmed) != 1 {
		t.Fatalf("required displaced %v %v", result, e)
	}
}

func TestConflictAndMissingSelectionStayFixedButRecheckPermissions(t *testing.T) {
	store, e := sqlitecontext.Open(filepath.Join(t.TempDir(), "context.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	src := &catalogSources{entries: map[string]contextassembly.Source{}, revoked: map[string]bool{}}
	for _, key := range []string{"concise", "detailed", "inapplicable"} {
		src.entries[key] = contextassembly.Source{Block: brain.Block{Ref: key, Text: key, Subject: "alice", Role: "memory"}, Applicable: key != "inapplicable", CanStore: true, Claim: "style", Value: key}
	}
	a, e := contextassembly.New(store, facts{}, src)
	if e != nil {
		t.Fatal(e)
	}
	req := contextassembly.Request{Key: contextassembly.Key{Namespace: "local", TaskID: "task", Decision: 1}, Subject: "alice", Purpose: "assist", Location: "model", Storage: "device", FactsVersion: 1, PolicyVersion: "v1", MaxBytes: 8192}
	for _, key := range []string{"concise", "detailed", "inapplicable", "missing"} {
		req.Candidates = append(req.Candidates, contextassembly.Candidate{Reference: contextassembly.Reference{Namespace: "local", Collection: "personal", Key: key, Revision: 1}})
	}
	result, e := a.Assemble(context.Background(), req)
	if e != nil || len(result.Input.Blocks) != 2 || len(result.Status.Missing) != 1 || len(result.Status.Inapplicable) != 1 || len(result.Status.Conflicts) != 1 {
		t.Fatalf("statuses %v %v", result, e)
	}
	saved, e := store.Read(context.Background(), req.Key)
	if e != nil || !strings.Contains(string(saved.Document), `"Text":"concise"`) {
		t.Fatalf("retained body %s %v", saved.Document, e)
	}
	src.entries["missing"] = src.entries["concise"]
	again, e := a.Assemble(context.Background(), req)
	if e != contextassembly.Invalidated {
		t.Fatalf("expanded original %v %v", again, e)
	}
	src.revoked["inapplicable"] = true
	if e = a.Validate(context.Background(), req); e != contextassembly.Denied {
		t.Fatalf("revoked coverage source %v", e)
	}
	delete(src.revoked, "inapplicable")
	item := src.entries["concise"]
	item.CanStore = false
	src.entries["concise"] = item
	if _, e = a.Assemble(context.Background(), req); e != contextassembly.Denied {
		t.Fatalf("retention revoked %v", e)
	}
}

func TestEssentialFactsOverBudgetDoNotCreateSnapshot(t *testing.T) {
	store, e := sqlitecontext.Open(filepath.Join(t.TempDir(), "context.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	a, e := contextassembly.New(store, facts{}, &sources{text: "style"})
	if e != nil {
		t.Fatal(e)
	}
	req := contextassembly.Request{Key: contextassembly.Key{Namespace: "local", TaskID: "task", Decision: 1}, Subject: "alice", Purpose: "assist", Location: "model", Storage: "device", FactsVersion: 1, PolicyVersion: "v1", MaxBytes: 1}
	if _, e = a.Assemble(context.Background(), req); e != contextassembly.BudgetExceeded {
		t.Fatalf("mandatory facts %v", e)
	}
	if _, e = store.Read(context.Background(), req.Key); e != contextassembly.Missing {
		t.Fatalf("failed assembly bound %v", e)
	}
}

func TestDeniedCandidateCannotPersistAsMissingReference(t *testing.T) {
	store, e := sqlitecontext.Open(filepath.Join(t.TempDir(), "context.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	a, e := contextassembly.New(store, facts{}, &sources{denied: true})
	if e != nil {
		t.Fatal(e)
	}
	req := contextassembly.Request{Key: contextassembly.Key{Namespace: "local", TaskID: "task", Decision: 1}, Subject: "alice", Purpose: "assist", Location: "model", Storage: "device", FactsVersion: 1, PolicyVersion: "v1", MaxBytes: 8192, Candidates: []contextassembly.Candidate{{Reference: contextassembly.Reference{Namespace: "local", Collection: "personal", Key: "private", Revision: 1}}}}
	if _, e = a.Assemble(context.Background(), req); e != contextassembly.Denied {
		t.Fatalf("denied metadata %v", e)
	}
	if _, e = store.Read(context.Background(), req.Key); e != contextassembly.Missing {
		t.Fatalf("private reference persisted %v", e)
	}
}

func TestMissingDependencyMetadataIsReauthorizedOnReplay(t *testing.T) {
	store, e := sqlitecontext.Open(filepath.Join(t.TempDir(), "context.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	src := &catalogSources{entries: map[string]contextassembly.Source{}, revoked: map[string]bool{}}
	a, e := contextassembly.New(store, facts{}, src)
	if e != nil {
		t.Fatal(e)
	}
	req := contextassembly.Request{Key: contextassembly.Key{Namespace: "local", TaskID: "task", Decision: 1}, Subject: "alice", Purpose: "assist", Location: "model", Storage: "device", FactsVersion: 1, PolicyVersion: "v1", MaxBytes: 8192, Candidates: []contextassembly.Candidate{{Reference: contextassembly.Reference{Namespace: "local", Collection: "personal", Key: "missing", Revision: 1}}}}
	if _, e = a.Assemble(context.Background(), req); e != nil {
		t.Fatal(e)
	}
	src.revoked["missing"] = true
	if e = a.Validate(context.Background(), req); e != contextassembly.Denied {
		t.Fatalf("missing metadata revoked %v", e)
	}
}
