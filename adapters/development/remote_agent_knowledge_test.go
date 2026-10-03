package development

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	execadapter "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 普通知识经公开登记/验证/加载进入真实父Snapshot，再由两独立HTTPS owner
// 冻结委派并实际读取子目标文件；没有插入Snapshot、Use、Proposal或Effect。
func TestConfiguredRemoteParentKnowledgeFreezesOriginalSelectionAndExecutesChildRead(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver+"_parent_sqlite_child", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			a, b, profile := configuredAgentPairWithParentDriver(t, driver)
			permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
			skill, agent := configureRemoteParentKnowledge(ctx, t, a)
			profile = configureRemoteKnowledgeFileScope(ctx, t, a, b, profile, permission)
			goal, parent := configuredAgentOriginalParent(ctx, t, a)
			commit := remoteParentKnowledgeCommit(ctx, t, a, parent.TaskID)
			assertRemoteParentKnowledgePacket(ctx, t, a, commit, skill, agent)
			id := api.NewID("delegation")
			in := task.DelegateInput{DelegationID: id, ParentTaskRef: a.app.Scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: goal, InputRefs: []api.ContentRef{}, AgentBindingRef: profile.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{permission}, Budget: []api.Amount{{Unit: "USD", Value: "2"}}, Deadline: api.Time(time.Now().Add(3 * time.Minute)), PolicyRef: b.app.TaskPolicy.PolicyRef, ReceiverID: b.app.Scope.OwnerID}
			original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "collaboration.delegate", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(in)}
			receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
			if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
				t.Fatalf("selected original public delegation: %+v %v", receipt, err)
			}
			grant := configuredAgentGrant(ctx, t, a, permission)
			if !grant.OnceConsumed || !api.Equal(grant.Reserved, []api.Amount{{Unit: "USD", Value: "2"}}) {
				t.Fatalf("ordinary knowledge replaced original once Grant: %+v", grant)
			}
			const targetBytes = "Exact child bytes permitted by the original approved delegation."
			if err = os.MkdirAll(filepath.Join(b.config.DataRoot, "files", "reports"), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(b.config.DataRoot, "files", "reports", "parent.md"), []byte(targetBytes), 0600); err != nil {
				t.Fatal(err)
			}
			if !configuredAgentStep(ctx, t, a, task.JobDelegation) || !configuredAgentStep(ctx, t, b, collaboration.JobRemoteCreate) {
				t.Fatal("selected original child creation missing")
			}
			d, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
			if err != nil {
				t.Fatal(err)
			}
			state, err := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
			if err != nil || state.Task == nil || state.SourceDatabaseID != b.app.Scope.DatabaseID || state.Task.OrchestratorID != b.app.Scope.OwnerID {
				t.Fatalf("selected independent child mapping: %+v %v", state, err)
			}
			childID := state.Task.TaskID
			childCtx := b.app.foreignContextFactory(ctx, runtime.Flow{Kind: "query", Scope: b.app.Scope, Auth: b.app.ServiceAuth})
			childCtx, err = b.app.RemoteAgent.PrepareChildContext(childCtx, childID)
			if err != nil {
				t.Fatal(err)
			}
			var admission *collaboration.RemoteParentAdmission
			status, err := b.app.Store.Within(childCtx, b.app.Scope, []string{"task", "collaboration", "content", "memory", "governance", "platform"}, func(tx runtime.Tx) error {
				actual, e := b.app.Task.ReadTaskTreeTx(childCtx, tx, b.app.ServiceAuth, childID)
				if e != nil {
					return e
				}
				admission, e = b.app.RemoteAgent.ChildAdmissionTx(childCtx, tx, actual)
				return e
			})
			if err != nil || status != runtime.Committed || admission == nil {
				t.Fatalf("actual child admission: %v %v", status, err)
			}
			digest, err := api.Digest(commit.Selection)
			if err != nil {
				t.Fatal(err)
			}
			if admission.Knowledge == nil || admission.Knowledge.SelectionID != commit.Selection.ID || admission.Knowledge.SelectionDigest != digest || admission.Knowledge.PacketRef != commit.PacketRef || admission.DecisionID != commit.DecisionID || admission.SnapshotRef != commit.SnapshotRef || admission.SnapshotID != commit.Selection.Request.SnapshotID || admission.TaskRef != commit.Selection.Request.TaskRef {
				t.Fatalf("remote freezer substituted the original selection: %+v", admission)
			}
			if !api.Equal(admission.CapabilityRefs, []api.ComponentRef{execadapter.FileReadCapability().Ref}) || len(admission.ActionScopes) != 1 || admission.ActionScopes[0].BindingRef != b.app.ReadBinding || !api.Equal(admission.ActionScopes[0].ResourceRefs, []api.ComponentRef{component("managed-files")}) || !api.Equal(admission.ActionScopes[0].Actions, []string{"file.read"}) || admission.ActionScopes[0].Recipient != b.app.Scope.OwnerID || admission.Controls.MaxActionsPerDecision != 1 || admission.Controls.MaxDelegationsPerDecision != 1 || admission.Controls.MaxDepth != 2 || admission.Controls.MaxActionDurationSeconds != 30 || admission.Controls.MaxInputBytes != 65536 || admission.Controls.MaxOutputTokens != 128 || !api.Equal(admission.Controls.MaxCallCostBound, []api.Amount{{Unit: "USD", Value: "0.25"}}) || admission.ValidUntil != in.Deadline || admission.UseRef == nil {
				t.Fatalf("selected capability/binding/control/Grant intersection expanded: %+v", admission)
			}
			var use governance.UseReceipt
			raw, err := a.app.query(ctx, "grant.use.get", admission.UseRef.ObjectID, governance.IDInput{ID: admission.UseRef.ObjectID})
			if err != nil || api.Decode(raw, &use) != nil || use.TargetRef != a.app.Scope.Ref(id, 1) || use.TargetKind != "delegation" || use.Decision != "allowed" || use.IntentHash != admission.UseIntentHash || use.RequestDigest != admission.UseDigest || !api.Equal(use.GrantRefs, []api.ObjectRef{permission}) {
				t.Fatalf("selected scope did not retain its original authorized Use: %v %+v", err, use)
			}
			intent := remoteKnowledgeFirstIntent(ctx, t, b, childID)
			if intent.CapabilityRef != execadapter.FileReadCapability().Ref || intent.BindingRef != b.app.ReadBinding || intent.MaxDurationSeconds != 30 || intent.TaskRef.ObjectID != childID {
				t.Fatalf("actual original child action escaped selected controls: %+v", intent)
			}
			if !configuredAgentStep(ctx, t, b, task.JobDispatchOperation) {
				t.Fatal("original selected dispatch missing")
			}
			var actual execution.OperationView
			for round := 0; round < 64; round++ {
				raw, err = b.app.queryAs(ctx, b.app.ServiceAuth, "execution.get", intent.OperationID, execution.OperationIDInput{OperationID: intent.OperationID})
				if err != nil || api.Decode(raw, &actual) != nil {
					t.Fatal(err)
				}
				if actual.Operation.ExecutionState == "closed" && actual.Operation.ResultRef != nil {
					break
				}
				if !configuredAgentStep(ctx, t, b, execution.RunJob, execution.ReconcileJob) {
					t.Fatal("selected native read stopped before original result")
				}
			}
			if actual.Operation.ResultRef == nil || actual.Operation.Attempts.TotalCount != 1 || !actual.ActuallyStopped {
				t.Fatalf("selected original read lacks physical completion: %+v", actual)
			}
			body, err := b.app.ReadContentBytes(ctx, b.app.Scope, b.app.ServiceAuth, *actual.Operation.ResultRef, "task.context", "cloud")
			var observed execadapter.FileReadResult
			if err != nil || api.Decode(body, &observed) != nil || observed.Path != "reports/parent.md" || observed.DataBase64 != base64.StdEncoding.EncodeToString([]byte(targetBytes)) || api.Hash(body) != actual.Operation.ResultRef.Hash || uint64(len(body)) != actual.Operation.ResultRef.ByteLength {
				t.Fatalf("selected native read did not observe original real bytes: %v %+v", err, observed)
			}
			again, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
			if err != nil || !api.Equal(again, receipt) {
				t.Fatalf("original selected delegation replay changed identity: %v %+v", err, again)
			}
			a.reopenOriginal(t)
			b.reopenOriginal(t)
			after, err := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
			if err != nil || after.Task == nil || after.Task.TaskID != childID {
				t.Fatalf("reopen changed selected original child: %v %+v", err, after)
			}
			if got := remoteParentKnowledgeCommit(ctx, t, a, parent.TaskID); !api.Equal(got, commit) {
				t.Fatal("reopen refreshed the original selection/Snapshot identity")
			}
			t.Logf("actual selected parent=%s decision=%s selection=%s snapshot=%s packet=%s delegation=%s allocation=%s child=%s operation=%s original_command=%s", parent.TaskID, commit.DecisionID, commit.Selection.ID, commit.SnapshotRef.ContentID, commit.PacketRef.ContentID, id, d.AllocationRef.ObjectID, childID, intent.OperationID, original.CommandID)
		})
	}
}

func configureRemoteParentKnowledge(ctx context.Context, t *testing.T, a *configuredAgentEndpoint) (governance.SkillDefinition, governance.AgentConfigDefinition) {
	t.Helper()
	skill, _ := registerPublishedKnowledgeSkill(t, ctx, a.app)
	raw, err := a.app.query(ctx, "skill.load", skill.SkillRef.ComponentID, governance.SkillReference{SkillRef: skill.SkillRef})
	var loaded governance.LoadedSkill
	if err != nil || api.Decode(raw, &loaded) != nil || !api.Equal(loaded.Definition, skill) {
		t.Fatalf("actual public Skill load: %v", err)
	}
	controls := governance.KnowledgeControls{MaxInputBytes: 65536, MaxOutputTokens: 128, MaxActionsPerDecision: 1, MaxDelegationsPerDecision: 1, MaxDepth: 2, MaxActionDurationSeconds: 30, MaxCallCostBound: []api.Amount{{Unit: "USD", Value: "0.25"}}}
	limits, err := a.app.Publish(ctx, a.app.Scope, a.app.UserAuth, api.NewID("content"), "application/json", api.Raw(controls), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := governance.SealAgentConfig(governance.AgentConfigDefinition{AgentConfigRef: api.ComponentRef{ComponentID: api.NewID("agent"), Version: "1"}, BrainRef: a.app.Profile.Ref, CapabilityRefs: []api.ComponentRef{execadapter.FileReadCapability().Ref}, ControlLimitsRef: limits, SourceRefs: []api.ContentRef{}})
	if err != nil {
		t.Fatal(err)
	}
	knowledgePublicCommand(t, ctx, a.app, "agent_config.register", agent.AgentConfigRef.ComponentID, agent, nil)
	if err = runtime.Drain(ctx, a.app.Store, a.app.Scope, a.app.Registry, 200); err != nil {
		t.Fatal(err)
	}
	raw, err = a.app.query(ctx, "agent_config.get", agent.AgentConfigRef.ComponentID, governance.AgentConfigReference{AgentConfigRef: agent.AgentConfigRef})
	var validated governance.AgentConfigRecord
	if err != nil || api.Decode(raw, &validated) != nil || validated.State != "active" || !api.Equal(validated.Definition, agent) {
		t.Fatalf("actual AgentConfig validation: %v %+v", err, validated)
	}
	a.config.Knowledge = &KnowledgeConfig{SkillRefs: []api.ComponentRef{skill.SkillRef}, AgentConfigRef: &agent.AgentConfigRef, ControlLimits: governance.KnowledgeControls{MaxInputBytes: api.MaxJSONBytes, MaxOutputTokens: 512, MaxActionsPerDecision: 4, MaxDelegationsPerDecision: 2, MaxDepth: 4, MaxActionDurationSeconds: 60, MaxCallCostBound: []api.Amount{{Unit: "USD", Value: "1"}}}}
	return skill, agent
}

func configureRemoteKnowledgeFileScope(ctx context.Context, t *testing.T, a, b *configuredAgentEndpoint, original collaboration.RemoteAgentProfile, permission api.ObjectRef) collaboration.RemoteAgentProfile {
	t.Helper()
	// 新有界profile，不覆盖已登记的原配置版本。
	v := original.Values
	v.AgentBindingRef = a.app.Scope.Ref(api.NewID("binding"), 1)
	v.PermissionRefs = []api.ObjectRef{permission}
	v.CapabilityRefs = []api.ComponentRef{execadapter.FileReadCapability().Ref, execadapter.FileWriteCapability().Ref}
	v.BindingRefs = []api.ObjectRef{b.app.ReadBinding, b.app.WriteBinding}
	v.ResourceRefs = []api.ComponentRef{component("managed-files")}
	v.MaterialPurposes = []string{"task.goal", "task.submit", "content.write", "brain.input", "task.context", "task.dispatch", "task.snapshot", "task.action", "execution.arguments", "brain.output", "task.attach_evidence", "task.complete", "execution_intent", "execution_arguments", "managed_file_read", "managed_file_write"}
	v.ActionScopes = []collaboration.RemoteActionScope{
		{CapabilityRef: execadapter.FileReadCapability().Ref, BindingRef: b.app.ReadBinding, Resources: []string{"managed-files"}, ResourceRefs: v.ResourceRefs, Actions: []string{"file.read"}, Recipient: b.app.Scope.OwnerID, Location: "cloud"},
		{CapabilityRef: execadapter.FileWriteCapability().Ref, BindingRef: b.app.WriteBinding, Resources: []string{"managed-files"}, ResourceRefs: v.ResourceRefs, Actions: []string{"file.write"}, Recipient: b.app.Scope.OwnerID, Location: "cloud"},
	}
	profile, err := collaboration.NewRemoteAgentProfile(api.NewID("agent"), "1.0.0", v)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []*configuredAgentEndpoint{a, b} {
		e.stopRun(t)
		if err = e.app.Close(); err != nil {
			t.Fatal(err)
		}
		e.config.RemoteAgent.Profiles = append(e.config.RemoteAgent.Profiles, profile)
		e.app, err = OpenApp(ctx, e.config, false)
		if err != nil {
			t.Fatal(err)
		}
		e.persistPrivateConfiguration(t)
		e.startRun(t)
	}
	return profile
}

func remoteParentKnowledgeCommit(ctx context.Context, t *testing.T, a *configuredAgentEndpoint, parentID string) governance.KnowledgeCommit {
	t.Helper()
	var snapshot task.TaskSnapshotView
	var found bool
	status, err := a.app.Store.Within(ctx, a.app.Scope, []string{"task"}, func(tx runtime.Tx) error {
		var e error
		snapshot, found, e = a.app.Task.LatestTaskSnapshotTx(ctx, tx, a.app.ServiceAuth, parentID)
		return e
	})
	if err != nil || status != runtime.Committed || !found {
		t.Fatalf("actual original parent Snapshot: %v %v", status, err)
	}
	raw, err := a.app.query(ctx, "knowledge.selection.get", snapshot.Intent.DecisionID, governance.KnowledgeSelectionReference{SnapshotRef: snapshot.Intent.SnapshotRef, DecisionID: snapshot.Intent.DecisionID})
	var commit governance.KnowledgeCommit
	if err != nil || api.Decode(raw, &commit) != nil || commit.Selection.Request.TaskRef.ObjectID != parentID || commit.Selection.Request.SnapshotID != snapshot.Snapshot.SnapshotID || commit.SnapshotRef != snapshot.Intent.SnapshotRef {
		t.Fatalf("actual original parent Knowledge commit: %v %+v", err, commit)
	}
	return commit
}

func assertRemoteParentKnowledgePacket(ctx context.Context, t *testing.T, a *configuredAgentEndpoint, commit governance.KnowledgeCommit, skill governance.SkillDefinition, agent governance.AgentConfigDefinition) {
	t.Helper()
	body, err := a.app.ReadContentBytes(ctx, a.app.Scope, a.app.ServiceAuth, commit.PacketRef, "knowledge.consume", "cloud")
	var packet governance.KnowledgePacket
	if err != nil || api.Decode(body, &packet) != nil || packet.Kind != "ordinary_knowledge/1" || len(packet.Skills) != 1 || !api.Equal(packet.Skills[0].Definition, skill) || packet.AgentConfig == nil || !api.Equal(*packet.AgentConfig, agent) || api.Hash(body) != commit.PacketRef.Hash || uint64(len(body)) != commit.PacketRef.ByteLength {
		t.Fatalf("ordinary original Packet lost exact public Skill/AgentConfig: %v %+v", err, packet)
	}
}

func remoteKnowledgeFirstIntent(ctx context.Context, t *testing.T, b *configuredAgentEndpoint, childID string) task.OperationIntent {
	t.Helper()
	for round := 0; round < 96; round++ {
		facts, err := b.app.Task.ContextFacts(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, childID)
		if err != nil {
			t.Fatal(err)
		}
		if len(facts.Operations) != 0 {
			if len(facts.Operations) != 1 {
				t.Fatal("selected first action fixture progressed beyond its original read")
			}
			return facts.Operations[0].Intent
		}
		if !configuredAgentStep(ctx, t, b, task.JobAdvance, task.JobDispatchDecision, brain.JobAdvance, task.JobCoverage, task.JobBilling) {
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(25 * time.Millisecond):
			}
		}
	}
	t.Fatal("selected original child first action was not actually admitted")
	return task.OperationIntent{}
}

func TestConfiguredRemoteParentKnowledgeRejectsUnapprovedOrNoLongerCurrentDelegation(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		for _, boundary := range []string{"skill_withdrawn", "source_closed", "parent_paused", "deadline", "no_grant", "delegation_count"} {
			t.Run(driver+"_"+boundary, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				a, b, profile := configuredAgentPairWithParentDriver(t, driver)
				permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
				skill, agent := configureRemoteParentKnowledge(ctx, t, a)
				if boundary == "delegation_count" {
					controls := remoteKnowledgeOriginalControls()
					controls.MaxDelegationsPerDecision = 0
					agent = replaceRemoteKnowledgeAgent(ctx, t, a, controls, agent.CapabilityRefs)
				}
				profile = configureRemoteKnowledgeFileScope(ctx, t, a, b, profile, permission)
				goal, parent := configuredAgentOriginalParent(ctx, t, a)
				commit := remoteParentKnowledgeCommit(ctx, t, a, parent.TaskID)
				assertRemoteParentKnowledgePacket(ctx, t, a, commit, skill, agent)
				// 原内容身份或原消费Decision任一替换都不能借用准确selection。
				for _, wrong := range []governance.KnowledgeSelectionReference{
					{SnapshotRef: func() api.ContentRef {
						ref := commit.SnapshotRef
						ref.Hash = api.Hash([]byte("other original snapshot bytes"))
						return ref
					}(), DecisionID: commit.DecisionID},
					{SnapshotRef: commit.SnapshotRef, DecisionID: api.NewID("decision")},
				} {
					if _, err := a.app.query(ctx, "knowledge.selection.get", wrong.DecisionID, wrong); !api.IsCode(err, "forbidden") || err.Error() != "forbidden: knowledge_original_consumer_mismatch" {
						t.Fatalf("substituted source/consumer borrowed original selection: %v", err)
					}
				}
				code, reason := "", ""
				switch boundary {
				case "skill_withdrawn":
					withdrawRemoteKnowledgeSkill(ctx, t, a, skill)
					code, reason = "invalid_state", "selected_skill_closed"
				case "source_closed":
					one := uint64(1)
					knowledgePublicCommand(t, ctx, a.app, "content.close", skill.BodyRef.ContentID, memory.CloseInput{ContentRef: skill.BodyRef, Reason: "withdraw the original selected Skill source"}, &one)
					code, reason = "forbidden", "source_closed"
				case "parent_paused":
					parent = configuredAgentControl(ctx, t, a, parent.TaskID, "pause")
					code, reason = "invalid_state", "task_not_running"
				case "deadline":
					code, reason = "invalid_request", "delegation_deadline_exceeded"
				case "no_grant":
					code, reason = "invalid_request", "use_scope_invalid"
				case "delegation_count":
					code, reason = "forbidden", "remote_parent_delegation_control_exceeded"
				}
				id := api.NewID("delegation")
				in := remoteKnowledgeDelegateInput(a, b, profile, goal, parent, permission, id)
				if boundary == "deadline" {
					end, _ := api.ParseTime(parent.Deadline)
					in.Deadline = api.Time(end.Add(time.Minute))
				}
				if boundary == "no_grant" {
					in.PermissionRefs = []api.ObjectRef{}
				}
				requests := a.requests.Load() + b.requests.Load()
				original := remoteKnowledgeDelegateCommand(a, in)
				r, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
				if err != nil || r.Stage != "rejected" || r.Error == nil || r.Error.Code != code || r.Error.Reason != reason {
					t.Fatalf("selected %s boundary: %v %+v", boundary, err, r)
				}
				if _, err = a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id); !api.IsCode(err, "not_found") {
					t.Fatalf("refused selected scope created a Delegation: %v", err)
				}
				if _, err = a.app.query(ctx, "grant.use.get", stableID("use", "remote-agent/"+id), governance.IDInput{ID: stableID("use", "remote-agent/"+id)}); !api.IsCode(err, "not_found") {
					t.Fatalf("refused selected scope created a Use: %v", err)
				}
				grant := configuredAgentGrant(ctx, t, a, permission)
				if grant.OnceConsumed || !api.Equal(grant.Reserved, []api.Amount{}) || a.requests.Load()+b.requests.Load() != requests {
					t.Fatalf("refused knowledge scope consumed authorization or sent remote work: %+v", grant)
				}
				raw, err := b.app.query(ctx, "task.list", b.app.Scope.OwnerID, task.TaskListInput{Limit: 100})
				var children api.Page[api.Task]
				if err != nil || api.Decode(raw, &children) != nil || !children.Exhausted || children.Partial || children.NextCursor != "" || len(children.Items) != 0 {
					t.Fatalf("refused scope created a child: %v %+v", err, children)
				}
			})
		}
	}
}

func TestConfiguredRemoteParentKnowledgeBoundsActualChildActionAndEncoding(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		for _, boundary := range []string{"actions", "capability", "cost", "input"} {
			t.Run(driver+"_"+boundary, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				a, b, profile := configuredAgentPairWithParentDriver(t, driver)
				permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
				skill, agent := configureRemoteParentKnowledge(ctx, t, a)
				controls, caps := remoteKnowledgeOriginalControls(), agent.CapabilityRefs
				reason := "remote_parent_model_control_exceeded"
				switch boundary {
				case "actions":
					controls.MaxActionsPerDecision = 0
					reason = "remote_parent_action_count_exceeded"
				case "capability":
					caps = []api.ComponentRef{execadapter.FileWriteCapability().Ref}
					reason = "remote_parent_capability_exceeded"
				case "cost":
					controls.MaxCallCostBound = []api.Amount{{Unit: "USD", Value: "0"}}
				}
				if boundary != "input" {
					agent = replaceRemoteKnowledgeAgent(ctx, t, a, controls, caps)
				}
				var posts atomic.Int32
				if boundary == "cost" || boundary == "input" {
					// 实际供应商HTTP出口已装配；原父费用/输入上限必须在首次POST之前拒绝。
					model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						posts.Add(1)
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(knowledgeContractReply())
					}))
					defer model.Close()
					t.Setenv("HARNESS_CONTRACT_MODEL_CREDENTIAL", "synthetic-remote-knowledge-contract-token")
					b.config.Model = contractModelConfig(model.URL)
					if os.Getenv("HARNESS_TEST_REMOTE_AGENT_EVIDENCE_ROOT") != "" {
						f, err := os.OpenFile(filepath.Join(b.config.DataRoot, "model-token-HARNESS_CONTRACT_MODEL_CREDENTIAL"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
						if err != nil {
							t.Fatal(err)
						}
						_, err = f.WriteString(os.Getenv("HARNESS_CONTRACT_MODEL_CREDENTIAL"))
						err = errors.Join(err, f.Sync(), f.Close())
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				profile = configureRemoteKnowledgeFileScope(ctx, t, a, b, profile, permission)
				goal, parent := configuredAgentOriginalParent(ctx, t, a)
				commit := remoteParentKnowledgeCommit(ctx, t, a, parent.TaskID)
				assertRemoteParentKnowledgePacket(ctx, t, a, commit, skill, agent)
				if boundary == "input" {
					var err error
					goal, err = a.app.Publish(ctx, a.app.Scope, a.app.UserAuth, api.NewID("content"), "application/json", api.Raw(brain.GoalSpec{Kind: "report", Title: "Original bounded child input", Body: strings.Repeat("x", 65536), SavePath: "reports/parent.md"}), []api.ContentRef{goal}, []api.ContentRef{})
					if err != nil {
						t.Fatal(err)
					}
				}
				_, childID := createRemoteKnowledgeChild(ctx, t, a, b, profile, goal, parent, permission)
				var refusal error
				for round := 0; round < 128; round++ {
					if boundary == "input" {
						// 65个不同来源副本的reconcile均各等待一次；只领取本切片
						// 原TaskAdvance，使真实编码门禁先被观察，不扩大Drain轮次。
						refusal = remoteKnowledgeOriginalInputAdvance(ctx, t, b)
					} else {
						refusal = runtime.Drain(ctx, b.app.Store, b.app.Scope, b.app.Registry, 64)
					}
					if refusal != nil {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("actual selected child did not reach its bound", ctx.Err())
					case <-time.After(20 * time.Millisecond):
					}
				}
				var observed *api.Error
				if !errors.As(refusal, &observed) || observed.Code != "forbidden" || observed.Reason != reason {
					t.Fatalf("actual selected child %s refusal: %v posts=%d", boundary, refusal, posts.Load())
				}
				assertRemoteKnowledgeNoChildAction(ctx, t, b, childID)
				if posts.Load() != 0 {
					t.Fatal("original selected cost/input bound allowed a physical model POST")
				}
				grant := configuredAgentGrant(ctx, t, a, permission)
				if !grant.OnceConsumed || !api.Equal(grant.Reserved, []api.Amount{{Unit: "USD", Value: "2"}}) {
					t.Fatalf("child refusal erased original once delegation responsibility: %+v", grant)
				}
				if boundary == "input" || boundary == "cost" {
					raw, err := b.app.query(ctx, "budget.read", childID, task.BudgetReadInput{TaskID: childID})
					var budget task.BudgetReadResponse
					if err != nil || api.Decode(raw, &budget) != nil || budget.Task == nil || len(budget.Task.Reservations) != 0 {
						t.Fatalf("blocked child encoding reserved a Decision/model Use: %v %+v", err, budget)
					}
				}
			})
		}
	}
}

func TestConfiguredRemoteParentKnowledgeRechecksWithdrawalAndControlBeforeChildAdmission(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		for _, boundary := range []string{"skill_withdrawn", "source_closed", "parent_paused"} {
			t.Run(driver+"_"+boundary, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				a, b, profile := configuredAgentPairWithParentDriver(t, driver)
				permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
				skill, _ := configureRemoteParentKnowledge(ctx, t, a)
				profile = configureRemoteKnowledgeFileScope(ctx, t, a, b, profile, permission)
				goal, parent := configuredAgentOriginalParent(ctx, t, a)
				_, childID := createRemoteKnowledgeChild(ctx, t, a, b, profile, goal, parent, permission)
				switch boundary {
				case "skill_withdrawn":
					withdrawRemoteKnowledgeSkill(ctx, t, a, skill)
				case "source_closed":
					one := uint64(1)
					knowledgePublicCommand(t, ctx, a.app, "content.close", skill.BodyRef.ContentID, memory.CloseInput{ContentRef: skill.BodyRef, Reason: "withdraw original source after child creation"}, &one)
				case "parent_paused":
					configuredAgentControl(ctx, t, a, parent.TaskID, "pause")
				}
				flow := b.app.foreignContextFactory(ctx, runtime.Flow{Kind: "job", Scope: b.app.Scope, Auth: b.app.ServiceAuth})
				flow, err := b.app.RemoteAgent.PrepareChildContext(flow, childID)
				if err != nil {
					t.Fatalf("actual current signed parent scope: %v", err)
				}
				status, err := b.app.Store.Within(flow, b.app.Scope, []string{"task", "collaboration", "content", "memory", "governance", "platform"}, func(tx runtime.Tx) error {
					return b.app.Task.CheckTaskCurrentTx(flow, tx, b.app.ServiceAuth, childID, true)
				})
				if status == runtime.Committed || !api.IsCode(err, "forbidden") || err.Error() != "forbidden: remote_parent_scope_denied" {
					t.Fatalf("original child used stale selected %s scope: %v %v", boundary, status, err)
				}
				assertRemoteKnowledgeNoChildAction(ctx, t, b, childID)
				grant := configuredAgentGrant(ctx, t, a, permission)
				if !grant.OnceConsumed || !api.Equal(grant.Reserved, []api.Amount{{Unit: "USD", Value: "2"}}) {
					t.Fatalf("withdrawal/control refunded original once Use: %+v", grant)
				}
			})
		}
	}
}

func remoteKnowledgeOriginalControls() governance.KnowledgeControls {
	return governance.KnowledgeControls{MaxInputBytes: 65536, MaxOutputTokens: 128, MaxActionsPerDecision: 1, MaxDelegationsPerDecision: 1, MaxDepth: 2, MaxActionDurationSeconds: 30, MaxCallCostBound: []api.Amount{{Unit: "USD", Value: "0.25"}}}
}

func replaceRemoteKnowledgeAgent(ctx context.Context, t *testing.T, a *configuredAgentEndpoint, controls governance.KnowledgeControls, caps []api.ComponentRef) governance.AgentConfigDefinition {
	t.Helper()
	ref, err := a.app.Publish(ctx, a.app.Scope, a.app.UserAuth, api.NewID("content"), "application/json", api.Raw(controls), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := governance.SealAgentConfig(governance.AgentConfigDefinition{AgentConfigRef: api.ComponentRef{ComponentID: api.NewID("agent"), Version: "1"}, BrainRef: a.app.Profile.Ref, CapabilityRefs: caps, ControlLimitsRef: ref, SourceRefs: []api.ContentRef{}})
	if err != nil {
		t.Fatal(err)
	}
	knowledgePublicCommand(t, ctx, a.app, "agent_config.register", agent.AgentConfigRef.ComponentID, agent, nil)
	if err = runtime.Drain(ctx, a.app.Store, a.app.Scope, a.app.Registry, 200); err != nil {
		t.Fatal(err)
	}
	raw, err := a.app.query(ctx, "agent_config.get", agent.AgentConfigRef.ComponentID, governance.AgentConfigReference{AgentConfigRef: agent.AgentConfigRef})
	var active governance.AgentConfigRecord
	if err != nil || api.Decode(raw, &active) != nil || active.State != "active" || !api.Equal(active.Definition, agent) {
		t.Fatalf("actual narrowed AgentConfig validation: %v %+v", err, active)
	}
	a.config.Knowledge.AgentConfigRef = &agent.AgentConfigRef
	return agent
}

func withdrawRemoteKnowledgeSkill(ctx context.Context, t *testing.T, a *configuredAgentEndpoint, skill governance.SkillDefinition) {
	t.Helper()
	raw, err := a.app.query(ctx, "skill.get", skill.SkillRef.ComponentID, governance.SkillReference{SkillRef: skill.SkillRef})
	var current governance.SkillRecord
	if err != nil || api.Decode(raw, &current) != nil || current.State != "active" {
		t.Fatalf("original selected Skill head: %v %+v", err, current)
	}
	knowledgePublicCommand(t, ctx, a.app, "skill.withdraw", current.ID, governance.KnowledgeChange{Ref: a.app.Scope.Ref(current.ID, current.Revision), Reason: "stop the original selected knowledge before another child admission"}, &current.Revision)
}

func remoteKnowledgeDelegateInput(a, b *configuredAgentEndpoint, profile collaboration.RemoteAgentProfile, goal api.ContentRef, parent api.Task, permission api.ObjectRef, id string) task.DelegateInput {
	return task.DelegateInput{DelegationID: id, ParentTaskRef: a.app.Scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: goal, InputRefs: []api.ContentRef{}, AgentBindingRef: profile.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{permission}, Budget: []api.Amount{{Unit: "USD", Value: "2"}}, Deadline: api.Time(time.Now().Add(3 * time.Minute)), PolicyRef: b.app.TaskPolicy.PolicyRef, ReceiverID: b.app.Scope.OwnerID}
}

func remoteKnowledgeDelegateCommand(a *configuredAgentEndpoint, in task.DelegateInput) api.Command {
	return api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "collaboration.delegate", TargetID: in.DelegationID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(in)}
}

func createRemoteKnowledgeChild(ctx context.Context, t *testing.T, a, b *configuredAgentEndpoint, profile collaboration.RemoteAgentProfile, goal api.ContentRef, parent api.Task, permission api.ObjectRef) (task.Delegation, string) {
	t.Helper()
	id := api.NewID("delegation")
	original := remoteKnowledgeDelegateCommand(a, remoteKnowledgeDelegateInput(a, b, profile, goal, parent, permission, id))
	r, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
	if err != nil || r.Stage != "applied" || r.Error != nil {
		t.Fatalf("actual selected delegation: %v %+v", err, r)
	}
	if !configuredAgentStep(ctx, t, a, task.JobDelegation) || !configuredAgentStep(ctx, t, b, collaboration.JobRemoteCreate) {
		t.Fatal("actual selected child creation missing")
	}
	d, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
	if err != nil {
		t.Fatal(err)
	}
	state, err := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
	if err != nil || state.Task == nil || state.Task.OrchestratorID != b.app.Scope.OwnerID || state.SourceDatabaseID != b.app.Scope.DatabaseID {
		t.Fatalf("actual selected child original mapping: %v %+v", err, state)
	}
	return d, state.Task.TaskID
}

func assertRemoteKnowledgeNoChildAction(ctx context.Context, t *testing.T, b *configuredAgentEndpoint, childID string) {
	t.Helper()
	facts, err := b.app.Task.ContextFacts(ctx, b.app.Store, b.app.Scope, b.app.ServiceAuth, childID)
	if err != nil || len(facts.Operations) != 0 || facts.Task.ResultRef != nil {
		t.Fatalf("refused selected child admitted an action or success: %v %+v", err, facts)
	}
	if _, err = os.ReadFile(filepath.Join(b.config.DataRoot, "files", "reports", "parent.md")); !os.IsNotExist(err) {
		t.Fatalf("refused selected scope changed the native target: %v", err)
	}
	raw, err := b.app.query(ctx, "budget.read", childID, task.BudgetReadInput{TaskID: childID})
	var budget task.BudgetReadResponse
	if err != nil || api.Decode(raw, &budget) != nil || budget.Task == nil {
		t.Fatalf("original refused child budget: %v %+v", err, budget)
	}
	for _, reservation := range budget.Task.Reservations {
		if reservation.SourceKind != "brain_decision" {
			t.Fatalf("refused selected child created an action reserve: %+v", reservation)
		}
		for _, key := range []string{"inspect_file", "save_file", "verify_file"} {
			id := stableID("use", reservation.SourceRef.ObjectID+"/"+key)
			if _, err = b.app.queryAs(ctx, b.app.ServiceAuth, "grant.use.get", id, governance.IDInput{ID: id}); !api.IsCode(err, "not_found") {
				t.Fatalf("refused selected child created an action Use: %v", err)
			}
		}
	}
}

// 使用与既有configuredAgentStep相同的实际Claim/注册handler入口；只保留
// 本切片预期的门禁错误用于断言，既不调用私有guard也不替代业务事实。
func remoteKnowledgeOriginalInputAdvance(ctx context.Context, t *testing.T, b *configuredAgentEndpoint) error {
	t.Helper()
	works, status, err := b.app.Store.Claim(ctx, b.app.Scope, api.NewID("boot"), []string{task.JobAdvance}, 1, time.Minute)
	if err != nil || status != runtime.Committed {
		t.Fatalf("original selected input TaskAdvance Claim: %v %v", status, err)
	}
	if len(works) == 0 {
		return nil
	}
	handler, ok := b.app.Registry.Job(works[0].Job.Kind)
	if !ok {
		t.Fatal("original selected input handler missing")
	}
	return handler(ctx, b.app.Store, b.app.Scope, works[0])
}
