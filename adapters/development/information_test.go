package development

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestExplicitInformationConfigurationDoesNotFetchAtConstruction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var requests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"7"}`))
	}))
	defer source.Close()
	root := t.TempDir()
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	var configured map[string]any
	if err = json.Unmarshal(api.Raw(cfg), &configured); err != nil {
		t.Fatal(err)
	}
	configured["information"] = []any{map[string]any{
		"source":                  providers.InformationSourceDescriptor{SourceRef: api.ComponentRef{ComponentID: api.NewID("source"), Version: "1"}, Origin: source.URL, FetchPathPrefixes: []string{"/facts/"}, AllowedCIDRs: []string{"127.0.0.1/32"}, Receiver: api.NewID("source"), Location: "cloud", PublicUnbilled: true, MaxQueryBytes: 4096, MaxItems: 20, MaxResponseBytes: 65536, TimeoutMillis: 2000, MaxConcurrent: 1},
		"allow_http_for_loopback": true, "retain_until": cfg.PolicyExpiresAt,
	}}
	if err = api.Decode(api.Raw(configured), &cfg); err != nil {
		t.Fatalf("explicit information configuration unavailable: %v", err)
	}
	a, err := OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("configuration or construction performed information HTTP")
	}
}

func TestConfiguredInformationBodyTraversesTaskWithOriginalDisclosureAndGrant(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredInformationTask(t, driver, "body", "applied")
		})
	}
}

func TestConfiguredInformationBodyRejectsMissingOriginalDisclosureBeforeUse(t *testing.T) {
	runConfiguredInformationTask(t, "sqlite", "body", "missing-disclosure")
}

func TestConfiguredInformationGrantRevokeBlocksDirectAndDerivedCachedBytes(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredInformationTask(t, driver, "body", "revoked-cache")
		})
	}
}

func TestConfiguredInformationCurrentDataLicenseCutoffBlocksOriginalCache(t *testing.T) {
	runConfiguredInformationTask(t, "sqlite", "body", "source-expired")
}

func TestConfiguredInformationSearchUsesOriginalDeclaredQueryAndFiniteCoverage(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredInformationTask(t, driver, "search", "applied")
		})
	}
}

func TestConfiguredInformationSearchRejectsMissingQueryDisclosureBeforeUse(t *testing.T) {
	runConfiguredInformationTask(t, "sqlite", "search", "missing-disclosure")
}

func TestConfiguredInformationOriginalFeesSettleAfterDataGrantRevokeBeforeBilling(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredInformationTask(t, driver, "body", "revoke-before-billing")
		})
	}
}

type originalPublicationReplyFault struct {
	runtime.Store
	armed atomic.Bool
}

func (s *originalPublicationReplyFault) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	status, err := s.Store.Within(ctx, scope, participants, fn)
	if status == runtime.Committed && err == nil && s.armed.Swap(false) {
		return runtime.CommitUnknown, runtime.ErrCommitUnknown
	}
	return status, err
}

// 实际DB已提交原publicationPlan后丢回执；不伪造DB失败，也不改私有记录。
func TestInformationConfigurationKeepsUnknownOriginalFilePublicationPolicy(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), reportFixtureTimeout)
			defer cancel()
			root := t.TempDir()
			cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			a, err := OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			fault := &originalPublicationReplyFault{Store: a.Store}
			fault.armed.Store(true)
			a.Store = fault
			id := api.NewID("content")
			body := []byte("original file material before optional source configuration")
			ref, err := a.Publish(ctx, a.Scope, a.UserAuth, id, "text/plain", body, []api.ContentRef{}, []api.ContentRef{})
			if !errors.Is(err, runtime.ErrCommitUnknown) {
				t.Fatalf("original publication commit was not unknown: %v", err)
			}
			if err = a.Close(); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"version":"7"}`))
			}))
			defer source.Close()
			cfg.Information = []InformationSourceConfig{{Source: providers.InformationSourceDescriptor{SourceRef: api.ComponentRef{ComponentID: api.NewID("source"), Version: "1"}, Origin: source.URL, FetchPathPrefixes: []string{"/facts/"}, AllowedCIDRs: []string{"127.0.0.1/32"}, Receiver: api.NewID("source"), Location: "cloud", PublicUnbilled: true, MaxQueryBytes: 4096, MaxItems: 20, MaxResponseBytes: 65536, TimeoutMillis: 2000, MaxConcurrent: 1}, AllowHTTPForLoopback: true, RetainUntil: cfg.PolicyExpiresAt}}
			a, err = OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := a.Close(); err != nil {
					t.Error(err)
				}
			}()
			recovered, err := a.Publish(ctx, a.Scope, a.UserAuth, id, "text/plain", body, []api.ContentRef{}, []api.ContentRef{})
			if err != nil || !api.Equal(recovered, ref) {
				t.Fatalf("optional source replaced original pending file publication policy: %+v %v", recovered, err)
			}
			actual, err := a.Memory.Read(ctx, a.Scope, a.UserAuth, recovered, "task.goal")
			if err != nil || string(actual) != string(body) || requests.Load() != 0 {
				t.Fatalf("original file recovery changed bytes or fetched source: %v HTTP=%d", err, requests.Load())
			}
		})
	}
}

func runConfiguredInformationTask(t *testing.T, driver, action, scenario string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), reportFixtureTimeout)
	defer cancel()
	var fetches atomic.Int32
	var originalResponse atomic.Value
	originalResponse.Store([]byte(`{"version":"7"}`))
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		body := []byte(`{"version":"7"}`)
		if action == "body" {
			if r.Method != "GET" || r.URL.Path != "/facts/version" || r.URL.RawQuery != "" {
				t.Error("Body did not use original fixed source route")
			}
		} else {
			var request providers.SearchRequest
			if r.Method != "POST" || r.URL.Path != "/search" || r.URL.RawQuery != "" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Protocol != "harness.information/1" || request.Limit != 1 || request.Query == "" {
				t.Error("Search did not use original fixed source wire")
			}
			cursor := "source-index-page-2"
			body = api.Raw(providers.SearchResponse{Protocol: "harness.information/1", RequestID: request.RequestID, Items: []providers.SearchItem{{URL: "https://example.org/facts/version", Title: "version", Snippet: "7"}}, Exhausted: false, Cursor: &cursor, Coverage: "source_index"})
		}
		originalResponse.Store(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer source.Close()
	root := t.TempDir()
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	cfg.Information = []InformationSourceConfig{{Source: providers.InformationSourceDescriptor{SourceRef: api.ComponentRef{ComponentID: api.NewID("source"), Version: "1"}, Origin: source.URL, FetchPathPrefixes: []string{"/facts/"}, AllowedCIDRs: []string{"127.0.0.1/32"}, Receiver: api.NewID("source"), Location: "cloud", PublicUnbilled: true, MaxQueryBytes: 4096, MaxItems: 20, MaxResponseBytes: 65536, TimeoutMillis: 2000, MaxConcurrent: 1}, AllowHTTPForLoopback: true, RetainUntil: api.Time(time.Now().Add(time.Hour))}}
	if action == "search" {
		cfg.Information[0].Source.SearchPath = "/search"
	}
	initial, err := OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	sourceRef := initial.Information[0].Descriptor().SourceRef
	capability := initial.Information[0].BodyDriver().Capability().Ref
	actionName := providers.InformationBody
	if action == "search" {
		capability = initial.Information[0].SearchDriver().Capability().Ref
		actionName = providers.InformationSearch
	}
	lock, err := InformationInstallLock(initial.Information[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = initial.Close(); err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 0 {
		t.Fatal("capability discovery fetched source")
	}
	scope := runtime.Scope{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, DatabaseID: cfg.DatabaseID}
	grant := api.Grant{GrantID: api.NewID("grant"), OwnerID: cfg.OwnerID, Revision: 1, SubjectRef: scope.Ref(cfg.OwnerID, 1), Resources: []string{"source:" + sourceRef.ComponentID}, Actions: []string{actionName}, Purposes: []string{"goal_action"}, Recipients: []string{cfg.Information[0].Source.Receiver}, Locations: []string{"cloud"}, Mode: "continuous", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: cfg.PolicyExpiresAt, Limits: []api.Amount{{Unit: "USD", Value: "1"}}}
	binding := scope.Ref(api.NewID("binding"), 1)
	cfg.ActionBindings = []ActionBindingConfig{{CapabilityRef: capability, BindingRef: binding, InstallLockRef: lock, Grant: grant}}
	var active atomic.Pointer[App]
	var modelCalls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		var wire struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		var input struct {
			Snapshot api.Snapshot `json:"snapshot"`
		}
		if json.NewDecoder(r.Body).Decode(&wire) != nil || len(wire.Messages) != 2 || json.Unmarshal([]byte(wire.Messages[1].Content), &input) != nil {
			t.Error("invalid actual source model wire")
			return
		}
		a := active.Load()
		declared := false
		for n, ref := range input.Snapshot.CapabilityRefs {
			declared = declared || api.Equal(ref, capability) && n < len(input.Snapshot.BindingRefs) && api.Equal(input.Snapshot.BindingRefs[n], binding)
		}
		if !declared {
			t.Error("accurate configured Body pair absent from original model Snapshot")
			return
		}
		generated := brain.Generated{Contents: []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "仅建议原信息源取得，不以建议代替真实结果。", DisclosedSources: []api.ContentRef{}}}}
		if input.Snapshot.Purpose == "interpret_requirements" {
			generated.Contents = append(generated.Contents, brain.GeneratedContent{LocalID: "statement", MediaType: "text/plain", Body: "保留原答案要求。", DisclosedSources: []api.ContentRef{}}, brain.GeneratedContent{LocalID: "parameters", MediaType: "application/json", Body: string(api.Raw(brain.RuleParameters{Kind: "answer", ExpectedHash: api.Hash([]byte("source contract")), ExpectedLength: 15})), DisclosedSources: []api.ContentRef{}})
			generated.Draft = brain.Draft{Kind: "refine_requirements", ReasonLocalID: "reason", Requirements: []brain.DraftRequirement{{CandidateKey: "artifact_exact", Kind: "quality", StatementLocalID: "statement", ParametersLocalID: "parameters", RuleRef: a.ArtifactRule, Required: true}}}
		} else {
			facts, err := a.Task.ContextFacts(r.Context(), a.Store, a.Scope, a.ServiceAuth, input.Snapshot.TaskRef.ObjectID)
			if err != nil {
				t.Error(err)
				return
			}
			if len(facts.Operations) > 0 {
				generated.Draft = brain.Draft{Kind: "fail", ReasonLocalID: "reason", ReasonCode: "contract_stopped_after_real_source_action"}
			} else {
				disclosed := []string{"arguments"}
				arguments := brain.GeneratedContent{LocalID: "arguments", MediaType: "application/json", Body: string(api.Raw(providers.BodyArguments{URL: source.URL + "/facts/version"})), DisclosedSources: []api.ContentRef{}}
				if action == "search" {
					arguments.Body = string(api.Raw(providers.SearchArguments{QueryRef: input.Snapshot.GoalRef, Limit: 1}))
					arguments.DisclosedSources = []api.ContentRef{input.Snapshot.GoalRef}
					disclosed = nil // Search只披露原QueryRef；不会强制公开整份Arguments JSON。
				}
				if scenario == "missing-disclosure" {
					disclosed = nil
					arguments.DisclosedSources = []api.ContentRef{}
				}
				generated.Contents = append(generated.Contents, arguments)
				generated.Draft = brain.Draft{Kind: "act", ReasonLocalID: "reason", Actions: []brain.DraftAction{{LocalKey: "retrieve_original_source", CapabilityRef: capability, BindingRef: binding, ArgumentsLocalID: "arguments", DisclosedLocalIDs: disclosed}}}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(api.Raw(map[string]any{"id": "source-original-model-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(map[string]any{"draft": generated.Draft, "contents": generated.Contents}))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}}))
	}))
	defer model.Close()
	t.Setenv("HARNESS_CONTRACT_MODEL_CREDENTIAL", "synthetic-contract-token")
	cfg.Model = contractModelConfig(model.URL)
	a, err := OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if a != nil {
			if err := a.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	active.Store(a)
	goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", api.Raw(brain.GoalSpec{Kind: "answer", Body: "source contract"}), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	taskID := api.NewID("task")
	commandGUI(t, ctx, a, a.UserAuth, "task.submit", taskID, task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}, RequirementCandidates: []api.RequirementCandidate{}})
	var facts task.ContextFacts
	revokedBeforeBilling := false
	for {
		limit := 200
		if scenario == "revoke-before-billing" && !revokedBeforeBilling {
			limit = 1
		}
		err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, limit)
		if limit == 1 && api.IsCode(err, "overloaded") {
			err = nil
		}
		if err != nil {
			if scenario == "missing-disclosure" && api.IsCode(err, "forbidden") {
				facts, readErr := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
				if readErr != nil || len(facts.Operations) != 0 || fetches.Load() != 0 {
					t.Fatalf("missing disclosure admitted a source operation: %v GET=%d", readErr, fetches.Load())
				}
				raw, readErr := a.query(ctx, "grant.read", grant.GrantID, governance.IDInput{ID: grant.GrantID})
				var current governance.GrantRecord
				if readErr != nil || api.Decode(raw, &current) != nil || current.OnceConsumed || len(current.Reserved) != 0 {
					t.Fatalf("missing disclosure consumed source Grant: %+v %v", current, readErr)
				}
				return
			}
			t.Fatal(err)
		}
		facts, err = a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if scenario == "revoke-before-billing" && !revokedBeforeBilling && fetches.Load() == 1 && len(facts.Operations) == 1 {
			raw, err := a.query(ctx, "execution.get", facts.Operations[0].Intent.OperationID, execution.OperationIDInput{OperationID: facts.Operations[0].Intent.OperationID})
			var view execution.OperationView
			if err != nil || api.Decode(raw, &view) != nil {
				t.Fatalf("original applied source fact unavailable: %v", err)
			}
			if view.Operation.Effect == "applied" && view.Operation.UsageFinal {
				revokeActionGrant(t, ctx, a, grant.GrantID, goal)
				current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
				if err != nil {
					t.Fatal(err)
				}
				command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: taskID, Method: "task.cancel", ExpectedRevision: &current.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ControlInput{TaskID: taskID, Reason: "confirmed data revoke before original source billing"})}
				receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
				if err != nil || receipt.Error != nil {
					t.Fatalf("original source Task cancellation rejected: %+v %v", receipt, err)
				}
				revokedBeforeBilling = true
			}
		}
		if (facts.Task.Status == "failed" || scenario == "revoke-before-billing" && facts.Task.Status == "cancelled") && !facts.Task.AccountingOpen {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("source task remained open: %+v %v", facts, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if scenario == "revoke-before-billing" {
		if !revokedBeforeBilling || len(facts.Operations) != 1 || facts.Task.Status != "cancelled" || facts.Task.AccountingOpen || fetches.Load() != 1 || modelCalls.Load() != 2 {
			t.Fatalf("known original fees lost after revoke: status=%s accounting=%v GET=%d POST=%d", facts.Task.Status, facts.Task.AccountingOpen, fetches.Load(), modelCalls.Load())
		}
		op := facts.Operations[0]
		raw, err := a.query(ctx, "execution.usage.get", op.Intent.OperationID, execution.OperationIDInput{OperationID: op.Intent.OperationID})
		var usage api.UsageSnapshot
		if err != nil || api.Decode(raw, &usage) != nil || !usage.SpendingClosed || !usage.UsageFinal || len(usage.Cumulative) != 1 || usage.Cumulative[0].Unit != "USD" || usage.Cumulative[0].Value != "0" || len(usage.ProofRefs) != 1 {
			t.Fatalf("original known source expense not readable: %+v %v", usage, err)
		}
		proofBytes, err := a.Memory.Read(ctx, a.Scope, a.ServiceAuth, usage.ProofRefs[0], "execution_usage_proof")
		var proof execution.UsageProof
		if err != nil || api.Decode(proofBytes, &proof) != nil || !api.Equal(proof.OperationRef, usage.SourceRef) || proof.SendStartedCount != 1 || proof.PhysicalCountMin != 1 || proof.PhysicalCountMax != 1 || len(proof.Attempts) != 1 {
			t.Fatalf("original exact minimal accounting basis missing: %+v %v", proof, err)
		}
		for _, subject := range []runtime.Auth{a.ServiceAuth, a.UserAuth} {
			bytes, err := a.Memory.Read(ctx, a.Scope, subject, usage.ProofRefs[0], "task.context")
			if len(bytes) != 0 || !api.IsCode(err, "forbidden") {
				t.Fatalf("minimal accounting proof became ordinary context: %d %v", len(bytes), err)
			}
			if proof.Attempts[0].ResultRef == nil {
				t.Fatal("original source fact lost its result identity")
			}
			bytes, err = a.Memory.Read(ctx, a.Scope, subject, *proof.Attempts[0].ResultRef, "execution_result")
			if len(bytes) != 0 || !api.IsCode(err, "forbidden") {
				t.Fatalf("known accounting fee reopened source result permission: %d %v", len(bytes), err)
			}
		}
		if len(facts.Task.Budget) != 1 || facts.Task.Budget[0].Spent != "0.00048" || facts.Task.Budget[0].Reserved != "0" {
			t.Fatalf("original two known model fees changed during cancellation: %+v", facts.Task.Budget)
		}
		if err = a.Close(); err != nil {
			t.Fatal(err)
		}
		a = nil
		a, err = OpenApp(ctx, cfg, false)
		if err != nil {
			t.Fatal(err)
		}
		active.Store(a)
		raw, err = a.query(ctx, "execution.usage.get", op.Intent.OperationID, execution.OperationIDInput{OperationID: op.Intent.OperationID})
		var reopened api.UsageSnapshot
		if err != nil || api.Decode(raw, &reopened) != nil || !api.Equal(reopened, usage) || fetches.Load() != 1 || modelCalls.Load() != 2 {
			t.Fatalf("original accounting recovery changed identity or resent HTTP: %+v %v GET=%d POST=%d", reopened, err, fetches.Load(), modelCalls.Load())
		}
		proofBytes, err = a.Memory.Read(ctx, a.Scope, a.ServiceAuth, usage.ProofRefs[0], "execution_usage_proof")
		if err != nil || api.Decode(proofBytes, &proof) != nil || proof.PhysicalCountMin != 1 || proof.PhysicalCountMax != 1 {
			t.Fatalf("original accounting proof unavailable after database reopen: %v", err)
		}
		return
	}
	if len(facts.Operations) != 1 || facts.Operations[0].Fact.Effect != "applied" || fetches.Load() != 1 || modelCalls.Load() != 3 {
		for _, operation := range facts.Operations {
			view, readErr := a.query(ctx, "execution.get", operation.Intent.OperationID, execution.OperationIDInput{OperationID: operation.Intent.OperationID})
			t.Logf("original public execution fact: %s error=%v", view, readErr)
		}
		t.Fatalf("real source Task path failed: GET=%d POST=%d", fetches.Load(), modelCalls.Load())
	}
	op := facts.Operations[0]
	disclosedRef := op.Intent.ArgumentsRef
	if action == "search" {
		args, err := a.Memory.Read(ctx, a.Scope, a.ServiceAuth, op.Intent.ArgumentsRef, "execution.arguments")
		var search providers.SearchArguments
		if err != nil || api.Decode(args, &search) != nil {
			t.Fatalf("original search query not readable: %v", err)
		}
		disclosedRef = search.QueryRef
	}
	if !containsContentRef(op.Intent.DisclosedSourceRefs, disclosedRef) {
		t.Fatal("original explicit Body argument disclosure was lost")
	}
	raw, err := a.query(ctx, "grant.use.get", op.Intent.UseIntentRefs[0].ObjectID, governance.IDInput{ID: op.Intent.UseIntentRefs[0].ObjectID})
	var use governance.UseReceipt
	if err != nil || api.Decode(raw, &use) != nil || len(use.GrantRefs) != 1 || use.GrantRefs[0].ObjectID != grant.GrantID || use.IntentHash != op.Intent.IntentHash {
		t.Fatalf("source original configured Grant differs: %+v %v", use, err)
	}
	raw, err = a.query(ctx, "execution.get", op.Intent.OperationID, execution.OperationIDInput{OperationID: op.Intent.OperationID})
	var view execution.OperationView
	if err != nil || api.Decode(raw, &view) != nil || view.Operation.ResultRef == nil {
		t.Fatalf("real source result missing: %v", err)
	}
	result, err := a.Memory.Read(ctx, a.Scope, a.ServiceAuth, *view.Operation.ResultRef, "task.context")
	var observation providers.InformationObservation
	if err != nil || api.Decode(result, &observation) != nil || observation.BodyRef == nil || !api.Equal(observation.SourceRef, sourceRef) {
		t.Fatalf("original source metadata missing: %+v %v", observation, err)
	}
	if action == "search" && (observation.Coverage != "source_index" || observation.Exhausted || observation.Cursor == nil || *observation.Cursor != "source-index-page-2" || len(observation.Items) != 1 || observation.Items[0].Snippet != "7") {
		t.Fatalf("Search changed original finite source coverage: %+v", observation)
	}
	received := originalResponse.Load().([]byte)
	body, err := a.Memory.Read(ctx, a.Scope, a.ServiceAuth, *observation.BodyRef, providers.InformationPurpose)
	if err != nil || string(body) != string(received) {
		t.Fatalf("original acquired bytes unavailable: %q %v", body, err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	a = nil
	if scenario == "source-expired" {
		cfg.Information[0].RetainUntil = api.Time(time.Now().Add(-time.Minute))
	}
	reopened, err := OpenApp(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if scenario == "source-expired" {
		bytes, err := reopened.Memory.Read(ctx, reopened.Scope, reopened.ServiceAuth, *observation.BodyRef, providers.InformationPurpose)
		if !api.IsCode(err, "gone") || len(bytes) != 0 || fetches.Load() != 1 || modelCalls.Load() != 3 {
			t.Fatalf("current source data cutoff disclosed old cache: bytes=%d error=%v GET=%d POST=%d", len(bytes), err, fetches.Load(), modelCalls.Load())
		}
		return
	}
	if err = runtime.Drain(ctx, reopened.Store, reopened.Scope, reopened.Registry, 100); err != nil {
		t.Fatal(err)
	}
	body, err = reopened.Memory.Read(ctx, reopened.Scope, reopened.ServiceAuth, *observation.BodyRef, providers.InformationPurpose)
	if err != nil || string(body) != string(received) || fetches.Load() != 1 || modelCalls.Load() != 3 {
		t.Fatalf("reopen replaced original Source: %q %v GET=%d POST=%d", body, err, fetches.Load(), modelCalls.Load())
	}
	if scenario == "revoked-cache" {
		derived, err := reopened.Publish(ctx, reopened.Scope, reopened.ServiceAuth, api.NewID("content"), "text/plain", []byte("version:7"), []api.ContentRef{*observation.BodyRef}, []api.ContentRef{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = reopened.Memory.Read(ctx, reopened.Scope, reopened.UserAuth, derived, "task.context"); err != nil {
			t.Fatalf("current derived source data unavailable before revoke: %v", err)
		}
		revokeActionGrant(t, ctx, reopened, grant.GrantID, *observation.BodyRef)
		for _, ref := range []api.ContentRef{*observation.BodyRef, derived, *view.Operation.ResultRef} {
			for _, auth := range []runtime.Auth{reopened.ServiceAuth, reopened.UserAuth} {
				bytes, err := reopened.Memory.Read(ctx, reopened.Scope, auth, ref, "task.context")
				if !api.IsCode(err, "forbidden") || len(bytes) != 0 {
					t.Fatalf("confirmed source revoke disclosed cached original/derived bytes: bytes=%d err=%v", len(bytes), err)
				}
			}
		}
		if fetches.Load() != 1 || modelCalls.Load() != 3 {
			t.Fatal("cached data revoke issued a replacement HTTP request")
		}
	}
}

func containsContentRef(refs []api.ContentRef, wanted api.ContentRef) bool {
	for _, ref := range refs {
		if api.Equal(ref, wanted) {
			return true
		}
	}
	return false
}
