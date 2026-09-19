package extractioncheck

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	contextmemory "lerna/adapters/context/memory"
	contextpolicy "lerna/adapters/context/policy"
	sqlitecontext "lerna/adapters/context/sqlite"
	taskcontext "lerna/adapters/context/task"
	memoryauth "lerna/adapters/memory/auth"
	sqlitememory "lerna/adapters/memory/sqlite"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/brain"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/tasks"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runReportDecision(t *testing.T, h *harness, task tasks.Task, memoryStore *sqlitememory.Store, service *memory.Service, policy *memoryauth.Authority, adapter *contextmemory.Adapter, request contextassembly.Request, ref contextassembly.Reference, expectedAnswer string, memoryApplicable bool) func() {
	ctx := context.Background()
	generation, err := h.work.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 4096, OutputTokens: 256})
	if err != nil {
		t.Fatal(err)
	}
	generation = generation.WithOutputIdentities(h.operation)
	s, err := h.core.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"claim", "start"} {
		s, err = generation.Commit(ctx, tasks.WorkChange{ChangeID: "report-" + kind, Kind: kind, Qualification: tasks.QualificationOf(s)})
		if err != nil {
			t.Fatal(err)
		}
	}
	s, err = generation.ReserveDecision(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := contextmemory.NewSources(service, func(view artifacts.SourceAuthority) memory.Checker { return policy.ReadPolicy(view) }, h.contentPolicy, "local", []contextassembly.Reference{ref})
	if err != nil {
		t.Fatal(err)
	}
	content, err := artifacts.New(h.auth, h.blobs, resolver, artifacts.Config{Inline: 64, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 64, MaxChunk: 65536, MaxFiles: 128, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	b := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}
	access, err := answers.NewContentAccess(content, h.contentPolicy, b, &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}, "task", h.clock, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := taskcontext.NewFacts(generation, access, h.clock, s, taskcontext.Scope{Purpose: request.Purpose, Location: request.Location, Storage: request.Storage, PolicyVersion: request.PolicyVersion})
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := sqlitecontext.Open(filepath.Join(h.root, "report-context.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { snapshots.Close() })
	contextPolicy, err := contextpolicy.New(h.auth, snapshots, contextpolicy.Config{Namespace: "local", Consumer: "extracted-report", ConfigSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("extracted-report-context:v1"))), Batch: 16, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := contextPolicy.Bind(h.token, adapter)
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := contextassembly.NewWithPolicy(snapshots, facts, adapter, bound)
	if err != nil {
		t.Fatal(err)
	}
	session, err := taskcontext.NewSession(assembler, request, s.Task)
	if err != nil {
		t.Fatal(err)
	}
	input, err := session.Assemble(ctx, s.Task, "local", brain.MaxInputBytes)
	if err != nil {
		t.Fatal(err)
	}
	if memoryApplicable {
		checkCancelledMemoryValidation(t, snapshots, facts, adapter, bound, request)
	}
	memories, inapplicable := 0, 0
	for _, block := range input.Blocks {
		if block.Role == "memory" {
			memories++
		}
		if block.Role == "context-status" {
			var status contextassembly.Status
			if err := json.Unmarshal([]byte(block.Text), &status); err != nil {
				t.Fatal(err)
			}
			inapplicable += len(status.Inapplicable)
		}
	}
	if memoryApplicable && (memories != 1 || inapplicable != 0) || !memoryApplicable && (memories != 0 || inapplicable != 1) {
		t.Fatalf("context applicability: memory blocks=%d, inapplicable=%d", memories, inapplicable)
	}
	lineage, err := session.Lineage(adapter)
	if err != nil {
		t.Fatal(err)
	}
	access, err = access.WithLineage(lineage)
	if err != nil {
		t.Fatal(err)
	}
	port, err := answers.BindContextPort(generation, access, "local", session)
	if err != nil {
		t.Fatal(err)
	}
	decider, err := brain.NewAnswer(reportContractModel{}, session, access, generation, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: brain.MaxAnswerBytes, SettlementTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := decider.Decide(ctx, tasks.DecisionInput{Task: s.Task, Work: s.Work[0], Generation: s.Generations[0]})
	if err != nil {
		t.Fatal(err)
	}
	out, err := port.Commit(ctx, tasks.WorkChange{Kind: "complete", ChangeID: "report-publication", Qualification: tasks.QualificationOf(s), Finished: true, Proposal: proposal})
	if err != nil || out.Task.State != "COMPLETED" {
		t.Fatalf("report publication: %s %v", out.Task.State, err)
	}
	outputRef, err := answers.ParseReference(out.Task.Result)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := content.Call(ctx, b, &wire.ContentRequest{Method: "GET", Ref: outputRef, Purpose: "task"})
	if err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, source := range stored.Record.Spec.Sources {
		if source.Kind == "memory" && source.Revision == ref.Revision {
			linked = true
		}
	}
	if !linked {
		t.Fatal("report output lost Memory lineage")
	}
	if stored.Record.Spec.Size == 0 || stored.Record.Spec.Size > 8192 {
		t.Fatal("report output size outside envelope")
	}
	bytes, err := content.Call(ctx, b, &wire.ContentRequest{Method: "READ", Ref: outputRef, Purpose: "task", Limit: uint32(stored.Record.Spec.Size)})
	if err != nil {
		t.Fatal(err)
	}
	var answer brain.Answer
	if err = json.Unmarshal(bytes.Data, &answer); err != nil || answer.Text != expectedAnswer || len(answer.Sources) != 1 || strings.HasPrefix(answer.Sources[0], "memory:") != memoryApplicable {
		t.Fatalf("published report ignored inferred format: %v", err)
	}
	// ContentAccess resolves the actual controlled output, including file bytes.
	if err = access.ValidateResult(ctx, out.Task.Result); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := access.ValidateResult(ctx, out.Task.Result); err == nil {
			t.Fatal("source invalidation left derived report usable")
		}
		cleanPublishedReport(t, h, memoryStore, service, snapshots, content, request.Key, outputRef, stored.Record.Spec)
	}
}

func putReportFacts(t *testing.T, h *harness) string {
	t.Helper()
	ctx := context.Background()
	body := []byte("The report is ready.")
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.content.Call(ctx, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, &wire.ContentRequest{Method: "PUT", OperationId: op, Data: body, Spec: &wire.ContentSpec{Kind: "evidence", Resource: "root", Purpose: "task", Sources: []*wire.ContentSource{{Kind: "note", Key: "one", Revision: 1}}, AcquiredAt: h.now().Unix(), RetainUntil: h.now().Add(10 * time.Minute).Unix(), MediaType: "text/plain", Size: uint64(len(body)), Sha256: fmt.Sprintf("%x", sha256.Sum256(body))}})
	if err != nil {
		t.Fatal(err)
	}
	return answers.Reference(result.Record.Ref)
}

// Deterministic contract model: it selects a report format from the actual
// governed context. This validates integration, not remote model quality.
type reportContractModel struct{}

func (reportContractModel) Capabilities() brain.Capabilities {
	return brain.Capabilities{Model: "report-contract", Version: "1", Location: "local", Text: true, Structured: true, HardBounds: true, ContextTokens: 8192, InputUpper: 4096}
}
func (reportContractModel) Generate(_ context.Context, request brain.Request) (brain.Result, error) {
	answer := brain.Answer{Text: "The report is ready. No applicable format preference was supplied."}
	for _, block := range request.Input.Blocks {
		if block.Role != "memory" && block.Role != "context-status" {
			answer.Sources = []string{block.Ref}
			break
		}
	}
	for _, block := range request.Input.Blocks {
		var value struct{ Claim, Value string }
		if block.Role == "memory" && json.Unmarshal([]byte(block.Text), &value) == nil && (value.Claim == "tentative-report-format" || value.Claim == "explicit-reply-format") {
			if value.Value == "concise" {
				answer.Text = "Report ready."
			}
			if value.Value == "detailed" {
				if value.Claim == "tentative-report-format" {
					answer.Text = "The report is ready. Its detailed format follows a tentative inference from prior report choices."
				} else {
					answer.Text = "The report is ready. Its detailed format follows the user's explicit preference."
				}
			}
			answer.Sources = []string{block.Ref}
		}
	}
	data, err := json.Marshal(answer)
	return brain.Result{Content: data, Finish: "stop", Usage: brain.Usage{Known: true, Input: 100, Output: 30}}, err
}
