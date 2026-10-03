package development

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 公开开发Config、实际HTTP模型、Dispatcher、Task/Gov/Execution与独立手机目标。
// 模型只提供闭合草稿；没有植入Proposal、Operation、Use或已成功Fact。
func TestConfiguredGUIActionTraversesRealTaskAndOriginalGrant(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredGUIAction(t, driver, "applied")
		})
	}
}

func TestConfiguredGUIRejectsCapabilityBindingCrossingBeforeTargetEntry(t *testing.T) {
	runConfiguredGUIAction(t, "sqlite", "cross-binding")
}

func TestConfiguredGUIOnceGrantIsNotConsumedByModelContext(t *testing.T) {
	runConfiguredGUIAction(t, "sqlite", "once")
}

func TestConfiguredGUIClickGrantDoesNotAuthorizeInput(t *testing.T) {
	runConfiguredGUIAction(t, "sqlite", "input-denied")
}

func TestConfiguredGUIGrantRevokeAfterOriginalSnapshotBlocksRealTarget(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredGUIAction(t, driver, "revoked")
		})
	}
}

func TestPreparedGUIActionKeepsOriginalLeafLockAfterAssemblyChanges(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredGUIAction(t, driver, "configuration-change")
		})
	}
}

func runConfiguredGUIAction(t *testing.T, driver, scenario string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), reportFixtureTimeout)
	defer cancel()
	root := t.TempDir()
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	cfg.UserRoles = append(cfg.UserRoles, "device_controller")
	resourceID := platform.StableDevelopmentID("resource", "phone-one")
	binding := api.ObjectRef{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, ObjectID: api.NewID("binding"), Revision: 1}
	grant := api.Grant{GrantID: api.NewID("grant"), OwnerID: cfg.OwnerID, Revision: 1, SubjectRef: api.ObjectRef{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, ObjectID: cfg.OwnerID, Revision: 1}, Resources: []string{resourceID}, Actions: []string{"gui.click"}, Purposes: []string{"goal_action"}, Recipients: []string{cfg.OwnerID}, Locations: []string{"cloud"}, Mode: "continuous", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: cfg.PolicyExpiresAt, Limits: []api.Amount{{Unit: "USD", Value: "1"}}}
	if scenario == "once" {
		grant.Mode = "once"
	}
	var configured map[string]any
	if err = json.Unmarshal(api.Raw(cfg), &configured); err != nil {
		t.Fatal(err)
	}
	configured["action_bindings"] = []any{map[string]any{"capability_ref": target.PhoneGUICapability().Ref, "binding_ref": binding, "install_lock_ref": component("builtin-install-lock"), "grant": grant}}
	if err = api.Decode(api.Raw(configured), &cfg); err != nil {
		t.Fatalf("explicit GUI configuration unavailable: %v", err)
	}

	var active atomic.Pointer[App]
	var sends atomic.Int32
	var lease execution.ResourceLease
	var observed execution.Observation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		body, e := io.ReadAll(io.LimitReader(r.Body, api.MaxJSONBytes+1))
		if e != nil {
			t.Error(e)
			return
		}
		var wire struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		var input struct {
			Snapshot api.Snapshot `json:"snapshot"`
		}
		if json.Unmarshal(body, &wire) != nil || len(wire.Messages) != 2 || json.Unmarshal([]byte(wire.Messages[1].Content), &input) != nil {
			t.Error("invalid actual model body")
			return
		}
		a := active.Load()
		found := false
		for n, cap := range input.Snapshot.CapabilityRefs {
			if api.Equal(cap, target.PhoneGUICapability().Ref) && n < len(input.Snapshot.BindingRefs) && api.Equal(input.Snapshot.BindingRefs[n], binding) {
				found = true
			}
		}
		if !found && !(scenario == "once" && sends.Load() == 3) {
			t.Error("configured GUI pair absent from actual model Snapshot")
			return
		}
		if scenario == "once" {
			raw, e := a.query(r.Context(), "grant.read", grant.GrantID, governance.IDInput{ID: grant.GrantID})
			var current governance.GrantRecord
			if e != nil || api.Decode(raw, &current) != nil {
				t.Errorf("current original once Grant unavailable %v", e)
				return
			}
			if sends.Load() <= 2 && current.OnceConsumed || sends.Load() == 3 && (!current.OnceConsumed || found) {
				t.Error("Context query consumed once or advertised consumed Grant")
				return
			}
		}
		generated := brain.Generated{Contents: []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "该模型合同仅建议准确GUI行动，不自报工具效果。", DisclosedSources: []api.ContentRef{}}}}
		if input.Snapshot.Purpose == "interpret_requirements" {
			generated.Contents = append(generated.Contents, brain.GeneratedContent{LocalID: "statement", MediaType: "text/plain", Body: "保留原目标的准确答案要求。", DisclosedSources: []api.ContentRef{}}, brain.GeneratedContent{LocalID: "parameters", MediaType: "application/json", Body: string(api.Raw(brain.RuleParameters{Kind: "answer", ExpectedHash: api.Hash([]byte("GUI contract")), ExpectedLength: 12})), DisclosedSources: []api.ContentRef{}})
			generated.Draft = brain.Draft{Kind: "refine_requirements", ReasonLocalID: "reason", Requirements: []brain.DraftRequirement{{CandidateKey: "artifact_exact", Kind: "quality", StatementLocalID: "statement", ParametersLocalID: "parameters", RuleRef: a.ArtifactRule, Required: true}}}
		} else {
			facts, e := a.Task.ContextFacts(r.Context(), a.Store, a.Scope, a.ServiceAuth, input.Snapshot.TaskRef.ObjectID)
			if e != nil {
				t.Error(e)
				return
			}
			if len(facts.Operations) > 0 {
				generated.Draft = brain.Draft{Kind: "fail", ReasonLocalID: "reason", ReasonCode: "contract_stopped_after_action"}
			} else {
				id := api.NewID("observation")
				commandGUI(t, r.Context(), a, a.UserAuth, "resource.observe", resourceID, execution.ObserveInput{ResourceID: resourceID, ObservationID: id, HolderID: lease.HolderID, InstanceID: lease.InstanceID, ControlEpoch: lease.ControlEpoch})
				works, status, e := a.Store.Claim(r.Context(), a.Scope, api.NewID("holder"), []string{execution.ResourceObserveJob}, 1, time.Minute)
				if e != nil || status != runtime.Committed || len(works) != 1 {
					t.Errorf("actual observation claim %v %s", e, status)
					return
				}
				handler, ok := a.Registry.Job(execution.ResourceObserveJob)
				if !ok {
					t.Error("actual observation handler missing")
					return
				}
				if e = handler(r.Context(), a.Store, a.Scope, works[0]); e != nil {
					t.Error(e)
					return
				}
				raw, e := a.queryAs(r.Context(), a.UserAuth, "resource.observation.get", id, execution.ObservationIDInput{ObservationID: id})
				var out execution.ObserveOutput
				if e != nil || api.Decode(raw, &out) != nil || !out.Ready || out.Observation == nil {
					t.Errorf("actual observation unavailable %v", e)
					return
				}
				observed = *out.Observation
				args := target.PhoneGUIArguments{ResourceID: resourceID, InstanceID: observed.InstanceID, ControlEpoch: observed.ControlEpoch, ObservationID: id, TargetVersion: observed.TargetVersion, ActionBefore: observed.ActionBefore, Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}}
				if scenario == "input-denied" {
					text := "unauthorized input"
					args.Action = "input"
					args.Point = nil
					args.Text = &text
				}
				generated.Contents = append(generated.Contents, brain.GeneratedContent{LocalID: "arguments", MediaType: "application/json", Body: string(api.Raw(args)), DisclosedSources: []api.ContentRef{}})
				generated.Draft = brain.Draft{Kind: "act", ReasonLocalID: "reason", Actions: []brain.DraftAction{{LocalKey: "open_notes", CapabilityRef: target.PhoneGUICapability().Ref, BindingRef: binding, ArgumentsLocalID: "arguments"}}}
				if scenario == "cross-binding" {
					generated.Draft.Actions[0].BindingRef = a.WriteBinding
				}
				if scenario == "revoked" {
					revokeActionGrant(t, r.Context(), a, grant.GrantID, input.Snapshot.GoalRef)
				}
			}
		}
		response := api.Raw(map[string]any{"id": "gui-original-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(map[string]any{"draft": generated.Draft, "contents": generated.Contents}))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
	defer server.Close()
	t.Setenv("HARNESS_CONTRACT_MODEL_CREDENTIAL", "synthetic-contract-token")
	cfg.Model = contractModelConfig(server.URL)
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
	if _, err := a.query(ctx, "resource.get", resourceID, execution.ResourceInput{ResourceID: resourceID}); !api.IsCode(err, "forbidden") {
		t.Fatalf("GUI configuration implicitly widened default ServiceAuth: %v", err)
	}
	commandGUI(t, ctx, a, a.UserAuth, "resource.acquire", resourceID, execution.AcquireInput{ResourceID: resourceID, HolderID: api.NewID("holder"), InstanceID: api.NewID("instance"), LeaseUntil: api.Time(time.Now().Add(4 * time.Minute))})
	if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 100); err != nil {
		t.Fatal(err)
	}
	raw, err := a.queryAs(ctx, a.UserAuth, "resource.get", resourceID, execution.ResourceInput{ResourceID: resourceID})
	if err != nil || api.Decode(raw, &lease) != nil || lease.State != "held" || !lease.ActuallyStopped {
		t.Fatalf("real device lease absent %v %+v", err, lease)
	}
	goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", api.Raw(brain.GoalSpec{Kind: "answer", Body: "GUI contract"}), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	taskID := api.NewID("task")
	commandGUI(t, ctx, a, a.UserAuth, "task.submit", taskID, task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}, RequirementCandidates: []api.RequirementCandidate{}})
	var facts task.ContextFacts
	changedAssembly := false
	var originalOperation task.OperationIntent
	for {
		limit := 200
		if scenario == "configuration-change" && !changedAssembly {
			limit = 1
		}
		err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, limit)
		if limit == 1 && err != nil && err.Error() == "overloaded: drain_limit_reached" {
			err = nil
		} // 显式只执行一项真实责任后观察公开状态。
		if err != nil {
			if (scenario == "cross-binding" || scenario == "input-denied" || scenario == "revoked") && api.IsCode(err, "forbidden") {
				facts, e := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
				if e != nil || len(facts.Operations) != 0 {
					t.Fatalf("crossed binding admitted an operation %+v %v", facts, e)
				}
				raw, e := a.query(ctx, "grant.read", grant.GrantID, governance.IDInput{ID: grant.GrantID})
				var current governance.GrantRecord
				if e != nil || api.Decode(raw, &current) != nil || current.OnceConsumed || len(current.Reserved) != 0 {
					t.Fatalf("crossed binding consumed Grant %+v %v", current, e)
				}
				if scenario == "revoked" && (current.Grant.State != "revoked" || current.Grant.Revision != 2) {
					t.Fatalf("original snapshot bypassed current revoked head %+v", current)
				}
				bytes, e := os.ReadFile(filepath.Join(root, "phones", resourceID+".json"))
				var physical struct {
					State    target.PhoneState `json:"state"`
					Attempts []json.RawMessage `json:"attempts"`
				}
				if e != nil || json.Unmarshal(bytes, &physical) != nil || physical.State.Screen != "home" || physical.State.Version != 1 || len(physical.Attempts) != 0 {
					t.Fatalf("crossed capability/binding reached target %+v %v", physical, e)
				}
				if scenario == "revoked" {
					if e = a.Close(); e != nil {
						t.Fatal(e)
					}
					a = nil
					reopened, e := OpenApp(ctx, cfg, true)
					if e != nil {
						t.Fatal(e)
					}
					defer reopened.Close()
					raw, e = reopened.query(ctx, "grant.read", grant.GrantID, governance.IDInput{ID: grant.GrantID})
					if e != nil || api.Decode(raw, &current) != nil || current.Grant.State != "revoked" || current.Grant.Revision != 2 {
						t.Fatalf("initialize recreated revoked Grant %+v %v", current, e)
					}
				}
				return
			}
			t.Fatal(err)
		}
		facts, err = a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if scenario == "configuration-change" && !changedAssembly && len(facts.Operations) == 1 && !facts.Operations[0].Fact.Closed {
			originalOperation = facts.Operations[0].Intent
			oldLock := a.InstallLock
			if err = a.Close(); err != nil {
				t.Fatal(err)
			}
			a = nil
			additional := cfg.ActionBindings[0]
			additional.BindingRef = binding
			additional.BindingRef.ObjectID = api.NewID("binding")
			cfg.ActionBindings = append(cfg.ActionBindings, additional)
			a, err = OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			active.Store(a)
			if api.Equal(a.InstallLock, oldLock) {
				t.Fatal("new explicit binding did not freeze a new assembly digest")
			}
			changedAssembly = true
		}
		if len(facts.Operations) == 1 && facts.Operations[0].Fact.Closed && facts.Task.Status == "failed" && !facts.Task.AccountingOpen {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("GUI Task did not close: %+v %v", facts, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	op := facts.Operations[0]
	if scenario == "configuration-change" && (!changedAssembly || !api.Equal(op.Intent, originalOperation) || api.Equal(op.Intent.InstallLockRef, a.InstallLock)) {
		t.Fatalf("configuration rebound original prepared operation: %+v", op.Intent)
	}
	if op.Fact.Effect != "applied" || op.Fact.MayApplyLater || !api.Equal(op.Intent.CapabilityRef, target.PhoneGUICapability().Ref) || !api.Equal(op.Intent.BindingRef, binding) || len(op.Intent.ResourceKeys) != 1 || op.Intent.ResourceKeys[0] != "device:"+resourceID {
		t.Fatalf("original public action fact differs: %+v", op)
	}
	raw, err = a.query(ctx, "grant.use.get", op.Intent.UseIntentRefs[0].ObjectID, governance.IDInput{ID: op.Intent.UseIntentRefs[0].ObjectID})
	var use governance.UseReceipt
	if err != nil || api.Decode(raw, &use) != nil || use.Decision != "allowed" || len(use.GrantRefs) != 1 || use.GrantRefs[0].ObjectID != grant.GrantID || use.IntentHash != op.Intent.IntentHash {
		t.Fatalf("exact configured original Grant not used: %+v %v", use, err)
	}
	bytes, err := os.ReadFile(filepath.Join(root, "phones", resourceID+".json"))
	var physical struct {
		State    target.PhoneState `json:"state"`
		Attempts []json.RawMessage `json:"attempts"`
	}
	if err != nil || json.Unmarshal(bytes, &physical) != nil || physical.State.Screen != "notes" || physical.State.Version != 2 || len(physical.Attempts) != 1 {
		t.Fatalf("independent GUI target did not apply exactly once: %+v %v", physical, err)
	}
	count := sends.Load()
	if count != 3 {
		t.Fatalf("actual model calls=%d", count)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	a = nil
	reopened, err := OpenApp(ctx, cfg, scenario == "once")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = runtime.Drain(ctx, reopened.Store, reopened.Scope, reopened.Registry, 100); err != nil {
		t.Fatal(err)
	}
	after, err := reopened.Task.ContextFacts(ctx, reopened.Store, reopened.Scope, reopened.UserAuth, taskID)
	if err != nil || len(after.Operations) != 1 || after.Operations[0].Fact.Effect != "applied" || sends.Load() != count {
		t.Fatalf("reopen rebuilt original action: %+v %v", after, err)
	}
}

func revokeActionGrant(t *testing.T, ctx context.Context, a *App, id string, preview api.ContentRef) {
	t.Helper()
	one := uint64(1)
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: "grant.revoke", ExpectedRevision: &one, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(governance.GrantRevoke{GrantRef: a.Scope.Ref(id, 1), PreviewRefs: []api.ContentRef{preview}, ConfirmationExpiresAt: api.Time(time.Now().Add(time.Minute))})}
	r, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
	var pending governance.ConfirmedOutput
	if err != nil || r.Error != nil || api.Decode(r.Output, &pending) != nil || pending.ConfirmationRef == nil {
		t.Fatalf("public revoke pending %+v %v", r, err)
	}
	raw, err := a.queryAs(ctx, a.UserAuth, "confirmation.read", pending.ConfirmationRef.ObjectID, governance.IDInput{ID: pending.ConfirmationRef.ObjectID})
	var confirmation governance.ConfirmationView
	if err != nil || api.Decode(raw, &confirmation) != nil {
		t.Fatalf("original confirmation %v", err)
	}
	commandGUI(t, ctx, a, a.UserAuth, "confirmation.decide", confirmation.RequestID, governance.ConfirmationDecision{RequestID: confirmation.RequestID, RequestRevision: confirmation.Revision, Decision: "approved", Challenge: confirmation.Challenge, PreviewRefs: confirmation.PreviewRefs})
	works, status, err := a.Store.Claim(ctx, a.Scope, api.NewID("holder"), []string{"governance.confirmation"}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("actual confirmation claim %s %v", status, err)
	}
	handler, ok := a.Registry.Job("governance.confirmation")
	if !ok {
		t.Fatal("actual confirmation handler absent")
	}
	if err = handler(ctx, a.Store, a.Scope, works[0]); err != nil {
		t.Fatal(err)
	}
	stored, err := a.Store.LookupCommand(ctx, a.Scope, command.CommandID)
	if err != nil || stored.Receipt.Stage != "applied" {
		t.Fatalf("public revoke did not apply %+v %v", stored.Receipt, err)
	}
}

func commandGUI(t *testing.T, ctx context.Context, a *App, auth runtime.Auth, method, target string, payload any) api.Receipt {
	t.Helper()
	r, e := a.Dispatcher.Command(ctx, auth, api.Raw(api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: target, Method: method, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(payload)}))
	if e != nil || r.Error != nil {
		t.Fatalf("%s: %+v %v", method, r, e)
	}
	return r
}
