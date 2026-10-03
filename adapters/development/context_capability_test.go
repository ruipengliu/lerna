package development

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestCapabilityLookupRechecksCompleteOriginalGrantChainWithoutConsumingOnce(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("requires actual PostgreSQL")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			root := t.TempDir()
			cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			cfg.UserRoles = append(cfg.UserRoles, "device_controller")
			a, err := OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := a.Close(); err != nil {
					t.Error(err)
				}
			})
			publish := func(body []byte) api.ContentRef {
				t.Helper()
				ref, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", body, []api.ContentRef{}, []api.ContentRef{})
				if err != nil {
					t.Fatal(err)
				}
				return ref
			}
			preview := publish([]byte(`{"permission":"exact GUI descriptor only"}`))
			confirm := func(method, id string, revision *uint64, input any) api.Receipt {
				t.Helper()
				original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: method, TargetID: id, ExpectedRevision: revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(input)}
				r, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(original))
				if err != nil || r.Error != nil || r.Stage != "accepted" {
					t.Fatalf("public %s original confirmation %+v %v", method, r, err)
				}
				var accepted governance.ConfirmedOutput
				if err = api.Decode(r.Output, &accepted); err != nil || accepted.ConfirmationRef == nil {
					t.Fatal("exact pending confirmation missing")
				}
				raw, err := clarificationQuery(ctx, a, "confirmation.read", accepted.ConfirmationRef.ObjectID, governance.IDInput{ID: accepted.ConfirmationRef.ObjectID})
				var view governance.ConfirmationView
				if err != nil || api.Decode(raw, &view) != nil {
					t.Fatalf("public confirmation recovery %v", err)
				}
				decision := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: "confirmation.decide", TargetID: view.RequestID, ExpiresAt: original.ExpiresAt, Payload: api.Raw(governance.ConfirmationDecision{RequestID: view.RequestID, RequestRevision: view.Revision, Decision: "approved", Challenge: view.Challenge, PreviewRefs: view.PreviewRefs})}
				approved, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(decision))
				if err != nil || approved.Error != nil || approved.Stage != "applied" {
					t.Fatalf("actual confirmation approval %+v %v", approved, err)
				}
				works, status, err := a.Store.Claim(ctx, a.Scope, api.NewID("worker"), []string{"governance.confirmation"}, 10, time.Minute)
				if err != nil || status != runtime.Committed || len(works) < 1 {
					t.Fatalf("original confirmed responsibility not available %+v %v", works, err)
				}
				handler, ok := a.Registry.Job("governance.confirmation")
				if !ok {
					t.Fatal("confirmation handler missing")
				}
				for _, work := range works {
					if err = handler(ctx, a.Store, a.Scope, work); err != nil {
						t.Fatal(err)
					}
				}
				r, err = a.Dispatcher.Lookup(ctx, a.UserAuth, original.CommandID)
				if err != nil || r.Error != nil || r.Stage != "applied" {
					t.Fatalf("original confirmed command did not actually apply %+v %v", r, err)
				}
				return r
			}
			parent := api.Grant{GrantID: api.NewID("grant"), OwnerID: a.Scope.OwnerID, Revision: 1, SubjectRef: a.ServiceAuth.Ref(a.Scope.OwnerID), Resources: []string{developmentPhoneIDs()[0]}, Actions: []string{"gui.click"}, Purposes: []string{"goal_action"}, Recipients: []string{a.Scope.OwnerID}, Locations: []string{"cloud"}, Mode: "continuous", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: cfg.PolicyExpiresAt, Limits: []api.Amount{{Unit: "USD", Value: "1"}}}
			confirm("grant.issue", parent.GrantID, nil, governance.GrantIssue{Grant: parent, PreviewRefs: []api.ContentRef{preview}, ConfirmationExpiresAt: cfg.PolicyExpiresAt, ParentGrantRefs: []api.ObjectRef{}})
			child := parent
			child.GrantID, child.Mode = api.NewID("grant"), "once"
			confirm("grant.issue", child.GrantID, nil, governance.GrantIssue{Grant: child, PreviewRefs: []api.ContentRef{preview}, ConfirmationExpiresAt: cfg.PolicyExpiresAt, ParentGrantRefs: []api.ObjectRef{a.Scope.Ref(parent.GrantID, 1)}})
			cfg.ActionBindings = []ActionBindingConfig{{CapabilityRef: target.PhoneGUICapability().Ref, BindingRef: a.Scope.Ref(api.NewID("binding"), 1), InstallLockRef: component("builtin-install-lock"), Grant: child}}
			if err = a.Close(); err != nil {
				t.Fatal(err)
			}
			a, err = OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			goal := publish(api.Raw(brain.GoalSpec{Kind: "answer", Body: "Keep original goal; descriptor lookup is not a GUI action."}))
			id := api.NewID("task")
			command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: "task.submit", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})}
			r, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
			if err != nil || r.Error != nil {
				t.Fatalf("original submit %+v %v", r, err)
			}
			current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, id)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := (contextCompiler{a}).Prepare(ctx, a.Scope, a.UserAuth, current)
			if err != nil {
				t.Fatal(err)
			}
			declared := false
			for _, ref := range prepared.Snapshot.CapabilityRefs {
				declared = declared || api.Equal(ref, target.PhoneGUICapability().Ref)
			}
			if !declared {
				t.Fatal("actual original Snapshot omitted the still-authorized child capability")
			}
			if _, err = a.Task.PrepareDecision(ctx, a.Store, a.Scope, a.ServiceAuth, prepared); err != nil {
				t.Fatal(err)
			}
			one := uint64(1)
			confirm("grant.revoke", parent.GrantID, &one, governance.GrantRevoke{GrantRef: a.Scope.Ref(parent.GrantID, 1), PreviewRefs: []api.ContentRef{preview}, ConfirmationExpiresAt: cfg.PolicyExpiresAt})
			cap := target.PhoneGUICapability().Ref
			query := publish(api.Raw(capabilityLookup{cap}))
			proposal := task.Proposal{DecisionID: prepared.DecisionID, Kind: "need_context", ReasonRef: goal, Lookups: []task.ContextLookup{{Kind: "capability_describe", TargetRef: a.Scope.Ref(cap.ComponentID, 1), QueryRef: query}}}
			out, err := a.Task.ConsumeProposal(ctx, a.Store, a.Scope, a.ServiceAuth, proposal, nil)
			if err != nil || out.Outcome != "adopted" {
				t.Fatalf("original descriptor lookup responsibility %+v %v", out, err)
			}
			works, status, err := a.Store.Claim(ctx, a.Scope, api.NewID("worker"), []string{task.JobContextLookup}, 1, time.Minute)
			if err != nil || status != runtime.Committed || len(works) != 1 {
				t.Fatalf("original lookup job missing %+v %v", works, err)
			}
			handler, _ := a.Registry.Job(task.JobContextLookup)
			if err = handler(ctx, a.Store, a.Scope, works[0]); err != nil {
				t.Fatal(err)
			}
			facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, id)
			if err != nil || !facts.ContextPending || len(facts.ContextMaterials) != 0 || len(facts.Operations) != 0 || facts.ContextBudget.Calls != 1 {
				t.Fatalf("current revoked parent disclosed a new descriptor or admitted a target action: %+v %v", facts, err)
			}
			raw, err := a.query(ctx, "grant.read", child.GrantID, governance.IDInput{ID: child.GrantID})
			var originalChild governance.GrantRecord
			if err != nil || api.Decode(raw, &originalChild) != nil || originalChild.Grant.State != "active" || originalChild.Grant.Revision != 1 || originalChild.OnceConsumed || len(originalChild.Reserved) != 0 {
				t.Fatalf("descriptor query consumed or rewrote the original child Grant: %+v %v", originalChild, err)
			}
		})
	}
}
