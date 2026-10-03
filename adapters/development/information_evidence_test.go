package development

import (
	"context"
	"encoding/json"
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
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 原问题在Goal中固定；模型从实际Snapshot资料提出答案，不自报检查结果。
func TestInformationReferenceAnswerCompletesOnlyAfterOriginalSourceVerification(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runInformationReferenceTask(t, driver, "valid")
		})
	}
}

func TestInformationReferenceRejectsWrongAnswerBeforeCompletion(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runInformationReferenceTask(t, driver, "wrong-answer")
		})
	}
}

func runInformationReferenceTask(t *testing.T, driver, scenario string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), reportFixtureTimeout)
	defer cancel()
	var fetches, posts atomic.Int32
	observed := api.Time(time.Now().Add(-time.Second))
	body := api.Raw(map[string]any{"version": "7", "observed_at": observed})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		if r.Method != "GET" || r.URL.Path != "/facts/version" || r.URL.RawQuery != "" {
			t.Error("reference question changed original HTTP target")
		}
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
	discovery, err := OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	sourceRef := discovery.Information[0].Descriptor().SourceRef
	capability := discovery.Information[0].BodyDriver().Capability().Ref
	lock, err := InformationInstallLock(discovery.Information[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = discovery.Close(); err != nil {
		t.Fatal(err)
	}
	scope := runtime.Scope{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, DatabaseID: cfg.DatabaseID}
	binding := scope.Ref(api.NewID("binding"), 1)
	cfg.ActionBindings = []ActionBindingConfig{{CapabilityRef: capability, BindingRef: binding, InstallLockRef: lock, Grant: api.Grant{GrantID: api.NewID("grant"), OwnerID: cfg.OwnerID, Revision: 1, SubjectRef: scope.Ref(cfg.OwnerID, 1), Resources: []string{"source:" + sourceRef.ComponentID}, Actions: []string{providers.InformationBody}, Purposes: []string{"goal_action"}, Recipients: []string{cfg.Information[0].Source.Receiver}, Locations: []string{"cloud"}, Mode: "continuous", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: cfg.PolicyExpiresAt, Limits: []api.Amount{{Unit: "USD", Value: "1"}}}}}
	question := map[string]any{"profile": providers.ReferenceProfile, "sources": []any{map[string]any{"source_ref": sourceRef, "url": source.URL + "/facts/version"}}, "claims": []providers.ReferenceClaim{{Key: "version", JSONPointer: "/version"}}, "max_age_seconds": 600, "observed_at_pointer": "/observed_at"}
	// 新规则只能由显式闭合配置开放；旧配置不默认扩大参考问答能力。
	var configured map[string]any
	if err = json.Unmarshal(api.Raw(cfg), &configured); err != nil {
		t.Fatal(err)
	}
	configured["information_reference_answer"] = true
	if err = api.Decode(api.Raw(configured), &cfg); err != nil {
		t.Fatalf("explicit reference-answer profile unavailable: %v", err)
	}
	var active atomic.Pointer[App]
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		var wire struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		var input struct {
			Snapshot  api.Snapshot `json:"snapshot"`
			GoalUTF8  string       `json:"goal_utf8"`
			Materials []struct {
				Ref  api.ContentRef `json:"ref"`
				Body string         `json:"body_utf8"`
			} `json:"materials"`
		}
		if json.NewDecoder(r.Body).Decode(&wire) != nil || len(wire.Messages) != 2 || api.Decode([]byte(wire.Messages[1].Content), &input) != nil {
			t.Error("reference answer lacked actual model Snapshot wire")
			return
		}
		a := active.Load()
		generated := brain.Generated{Contents: []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "仅提出原问题的资料和答案，检查由Task独立负责。", DisclosedSources: []api.ContentRef{}}}}
		if input.Snapshot.Purpose == "interpret_requirements" {
			generated.Contents = append(generated.Contents, brain.GeneratedContent{LocalID: "statement", MediaType: "text/plain", Body: "版本答案与原源当前字符串事实及引用一致。", DisclosedSources: []api.ContentRef{}}, brain.GeneratedContent{LocalID: "parameters", MediaType: "application/json", Body: string(api.Raw(question)), DisclosedSources: []api.ContentRef{}})
			generated.Draft = brain.Draft{Kind: "refine_requirements", ReasonLocalID: "reason", Requirements: []brain.DraftRequirement{{CandidateKey: "original_source_answer", Kind: "quality", StatementLocalID: "statement", ParametersLocalID: "parameters", RuleRef: component("source-reference-answer"), Required: true}}}
		} else {
			observations := []providers.InformationObservation{}
			for _, material := range input.Materials {
				var packet struct {
					Observations []providers.InformationObservation `json:"information_observations"`
				}
				if json.Unmarshal([]byte(material.Body), &packet) == nil {
					observations = append(observations, packet.Observations...)
				}
			}
			if len(observations) == 0 {
				generated.Contents = append(generated.Contents, brain.GeneratedContent{LocalID: "arguments", MediaType: "application/json", Body: string(api.Raw(providers.BodyArguments{URL: source.URL + "/facts/version"})), DisclosedSources: []api.ContentRef{}})
				generated.Draft = brain.Draft{Kind: "act", ReasonLocalID: "reason", Actions: []brain.DraftAction{{LocalKey: "read_original_fact", CapabilityRef: capability, BindingRef: binding, ArgumentsLocalID: "arguments", DisclosedLocalIDs: []string{"arguments"}}}}
			} else {
				if len(observations) != 1 || observations[0].BodyRef == nil {
					t.Error("model received incomplete original source observations")
					return
				}
				o := observations[0]
				actualMaterial := false
				for _, material := range input.Materials {
					actualMaterial = actualMaterial || api.Equal(material.Ref, *o.BodyRef) && material.Body == string(body)
				}
				if !actualMaterial || o.SourceRef != sourceRef || o.URL != source.URL+"/facts/version" || o.BodyRef.Hash != api.Hash(body) {
					t.Error("model lacked exact original body bytes or received caller-invented metadata")
					return
				}
				claimedValue := "7"
				if scenario == "wrong-answer" {
					claimedValue = "9"
				}
				answer := map[string]any{"profile": providers.ReferenceProfile, "answers": []providers.ReferencedAnswer{{Key: "version", Value: claimedValue, Citations: []providers.ReferenceCitation{{SourceRef: sourceRef, BodyRef: *o.BodyRef, URL: o.URL, ObtainedAt: o.ObtainedAt, ObservedAt: observed, JSONPointer: "/version", Quote: claimedValue}}}}}
				generated.Contents = append(generated.Contents, brain.GeneratedContent{LocalID: "answer", MediaType: "application/json", Body: string(api.Raw(answer)), DisclosedSources: []api.ContentRef{*o.BodyRef}})
				generated.Draft = brain.Draft{Kind: "complete", ReasonLocalID: "reason", ArtifactLocalIDs: []string{"answer"}}
			}
		}
		if a == nil {
			t.Error("model request before app ready")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(api.Raw(map[string]any{"id": "reference-answer-original-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(map[string]any{"draft": generated.Draft, "contents": generated.Contents}))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}}))
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
	goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", api.Raw(brain.GoalSpec{Kind: "answer", Body: string(api.Raw(question))}), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	taskID := api.NewID("task")
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: taskID, Method: "task.submit", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}, RequirementCandidates: []api.RequirementCandidate{}})}
	receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(original))
	if err != nil || receipt.Error != nil {
		t.Fatalf("reference Task submit: %v %+v", err, receipt)
	}
	for {
		limit := 500
		if scenario != "valid" {
			limit = 1
		}
		err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, limit)
		if limit == 1 && api.IsCode(err, "overloaded") {
			err = nil
		}
		if err != nil {
			t.Fatal(err)
		}
		current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if scenario == "wrong-answer" {
			facts, readErr := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(facts.Checks) > 0 {
				if len(facts.Checks) != 1 || facts.Checks[0].Verdict != "fail" || current.Status == "succeeded" || fetches.Load() != 1 || posts.Load() != 3 {
					t.Fatalf("unsupported answer became completion: GET=%d POST=%d checks=%+v", fetches.Load(), posts.Load(), facts.Checks)
				}
				if _, err := a.Task.Result(ctx, a.Store, a.Scope, a.UserAuth, taskID, task.ResultInput{}); err == nil {
					t.Fatal("wrong answer created a Result")
				}
				cancel := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: taskID, Method: "task.cancel", ExpectedRevision: &current.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ControlInput{TaskID: taskID, Reason: "original answer disagrees with independently read fact"})}
				r, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(cancel))
				if err != nil || r.Error != nil {
					t.Fatalf("wrong answer cancellation: %v %+v", err, r)
				}
				if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 500); err != nil {
					t.Fatal(err)
				}
				closed, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
				if err != nil || closed.Status != "cancelled" || closed.AccountingOpen || fetches.Load() != 1 || posts.Load() != 3 {
					t.Fatalf("wrong answer lost original accounting: %+v %v", closed, err)
				}
				return
			}
		}
		if current.Status == "failed" || current.Status == "cancelled" {
			t.Fatalf("reference answer did not complete: %+v", current)
		}
		if current.Status == "succeeded" && !current.AccountingOpen {
			result, err := a.Task.Result(ctx, a.Store, a.Scope, a.UserAuth, taskID, task.ResultInput{})
			if err != nil {
				t.Fatal(err)
			}
			if result.Publication != "published" {
				continue
			}
			if result.ContentRef == nil || result.Result.CompletionBasis != "verified" || len(result.Result.ConditionResults) != 1 || result.Result.ConditionResults[0].Verdict != "pass" || !api.Equal(result.Result.ConditionResults[0].RuleRef, component("source-reference-answer")) || fetches.Load() != 1 || posts.Load() != 3 {
				t.Fatalf("source receipt replaced independently verified Result: GET=%d POST=%d %+v", fetches.Load(), posts.Load(), result)
			}
			published, err := a.Memory.Read(ctx, a.Scope, a.UserAuth, *result.ContentRef, "task.result")
			var exported api.Result
			if err != nil || api.Decode(published, &exported) != nil || !api.Equal(exported, result.Result) {
				t.Fatalf("original published answer Result differs: %v", err)
			}
			duplicate, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(original))
			if err != nil || !api.Equal(duplicate, receipt) {
				t.Fatalf("original answer submit receipt changed: %v", err)
			}
			if err = a.Close(); err != nil {
				t.Fatal(err)
			}
			a = nil
			reopened, err := OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			a = reopened
			active.Store(a)
			if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 500); err != nil {
				t.Fatal(err)
			}
			after, err := a.Task.Result(ctx, a.Store, a.Scope, a.UserAuth, taskID, task.ResultInput{})
			if err != nil || !api.Equal(after, result) || fetches.Load() != 1 || posts.Load() != 3 {
				t.Fatalf("restart changed original answer or resent a source/model request: %v", err)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("reference answer remains unfinished: %+v %v", current, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
