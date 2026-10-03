// 两独立App/HTTPS权威通过原许可执行子报告；子结果不能直接裁决父目标。
// 条件检查、目标文件与两方Result均由原负责方实际读取并单独核验。
package development

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// The parent user really approves the bounded delegation Grant. There is no
// inserted Use, reserve, model Proposal, target Effect or trusted final bill.
func approveRemoteDelegationGrant(ctx context.Context, t *testing.T, a *configuredAgentEndpoint, receiver string) api.ObjectRef {
	t.Helper()
	grant := api.Grant{GrantID: api.NewID("grant"), OwnerID: a.app.Scope.OwnerID, Revision: 1, SubjectRef: a.app.UserAuth.Ref(a.app.Scope.OwnerID), Resources: []string{"managed-files"}, Actions: []string{"file.read", "file.write"}, Purposes: []string{"delegate"}, Recipients: []string{receiver}, Locations: []string{"cloud"}, Mode: "once", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: a.config.PolicyExpiresAt, Limits: []api.Amount{{Unit: "USD", Value: "2"}}}
	preview, err := a.app.Publish(ctx, a.app.Scope, a.app.UserAuth, api.NewID("content"), "application/json", api.Raw(grant), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "grant.issue", TargetID: grant.GrantID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(governance.GrantIssue{Grant: grant, PreviewRefs: []api.ContentRef{preview}, ConfirmationExpiresAt: grant.ExpiresAt, ParentGrantRefs: []api.ObjectRef{}})}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
	var pending governance.ConfirmedOutput
	if err != nil || receipt.Stage != "accepted" || receipt.Error != nil || api.Decode(receipt.Output, &pending) != nil || pending.ConfirmationRef == nil {
		t.Fatalf("original bounded permission confirmation: %+v %v", receipt, err)
	}
	raw, err := clarificationQuery(ctx, a.app, "confirmation.read", pending.ConfirmationRef.ObjectID, governance.IDInput{ID: pending.ConfirmationRef.ObjectID})
	var view governance.ConfirmationView
	if err != nil || api.Decode(raw, &view) != nil {
		t.Fatalf("original permission challenge: %v", err)
	}
	decision := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "confirmation.decide", TargetID: view.RequestID, ExpiresAt: original.ExpiresAt, Payload: api.Raw(governance.ConfirmationDecision{RequestID: view.RequestID, RequestRevision: view.Revision, Decision: "approved", Challenge: view.Challenge, PreviewRefs: view.PreviewRefs})}
	approved, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(decision))
	if err != nil || approved.Stage != "applied" || approved.Error != nil {
		t.Fatalf("actual permission approval: %+v %v", approved, err)
	}
	if !configuredAgentStep(ctx, t, a, "governance.confirmation") {
		t.Fatal("original permission confirmation Job missing")
	}
	done, err := a.app.Dispatcher.Lookup(ctx, a.app.UserAuth, original.CommandID)
	if err != nil || done.Stage != "applied" || done.Error != nil {
		t.Fatalf("original permission creation did not apply: %+v %v", done, err)
	}
	return a.app.Scope.Ref(grant.GrantID, 1)
}

func configureRemoteFileScope(ctx context.Context, t *testing.T, a, b *configuredAgentEndpoint, original collaboration.RemoteAgentProfile, permission api.ObjectRef) collaboration.RemoteAgentProfile {
	t.Helper()
	v := original.Values
	// 只为这个新文件行动profile冻结实际消费者用途，不改变已经登记的
	// 原profile、已出版DataPolicy或既有委派。execution_result 是原 read-only
	// Attempt 对账读取准确已出版 Result 的第17个实际消费者用途。
	v.MaterialPurposes = []string{"task.goal", "task.submit", "content.write", "brain.input", "task.context", "task.dispatch", "task.snapshot", "task.action", "execution.arguments", "brain.output", "task.attach_evidence", "task.complete", "execution_intent", "execution_arguments", "managed_file_read", "managed_file_write", "execution_result"}
	v.PermissionRefs = []api.ObjectRef{permission}
	v.CapabilityRefs = []api.ComponentRef{execution.FileReadCapability().Ref, execution.FileWriteCapability().Ref}
	v.BindingRefs = []api.ObjectRef{b.app.ReadBinding, b.app.WriteBinding}
	resource := component("managed-files")
	v.ResourceRefs = []api.ComponentRef{resource}
	v.ActionScopes = []collaboration.RemoteActionScope{
		{CapabilityRef: v.CapabilityRefs[0], BindingRef: b.app.ReadBinding, Resources: []string{"managed-files"}, ResourceRefs: []api.ComponentRef{resource}, Actions: []string{"file.read"}, Recipient: b.app.Scope.OwnerID, Location: "cloud"},
		{CapabilityRef: v.CapabilityRefs[1], BindingRef: b.app.WriteBinding, Resources: []string{"managed-files"}, ResourceRefs: []api.ComponentRef{resource}, Actions: []string{"file.write"}, Recipient: b.app.Scope.OwnerID, Location: "cloud"},
	}
	profile, err := collaboration.NewRemoteAgentProfile(original.ProfileRef.ComponentID, original.ProfileRef.Version, v)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []*configuredAgentEndpoint{a, b} {
		e.stopRun(t)
		if err = e.app.Close(); err != nil {
			t.Fatal(err)
		}
		e.config.RemoteAgent.Profiles = []collaboration.RemoteAgentProfile{profile}
		e.app, err = OpenApp(ctx, e.config, false)
		if err != nil {
			t.Fatal(err)
		}
		e.persistPrivateConfiguration(t)
		e.startRun(t)
	}
	return profile
}

func TestConfiguredRemoteChildReportDoesNotCompleteParentWithoutOwnChecks(t *testing.T) {
	for _, parentDriver := range []string{"sqlite", "postgres"} {
		t.Run(parentDriver+"_parent_sqlite_child", func(t *testing.T) {
			runConfiguredRemoteChildReport(t, parentDriver)
		})
	}
}

func runConfiguredRemoteChildReport(t *testing.T, parentDriver string) {
	t.Helper()
	// 仅放宽观察等待；Task的原5min/子委派3min期限不续写。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	a, b, profile := configuredAgentPairWithParentDriver(t, parentDriver)
	if a.config.Driver != parentDriver || b.config.Driver != "sqlite" || a.app.Scope.OwnerID == b.app.Scope.OwnerID || a.app.Scope.DatabaseID == b.app.Scope.DatabaseID {
		t.Fatal("remote child fixture did not retain independent owners and actual databases")
	}
	t.Logf("actual parent driver=%s owner=%s database=%s child driver=%s owner=%s database=%s", a.config.Driver, a.app.Scope.OwnerID, a.app.Scope.DatabaseID, b.config.Driver, b.app.Scope.OwnerID, b.app.Scope.DatabaseID)
	permission := approveRemoteDelegationGrant(ctx, t, a, b.app.Scope.OwnerID)
	profile = configureRemoteFileScope(ctx, t, a, b, profile, permission)
	goal, parent := configuredAgentOriginalParent(ctx, t, a)
	id := api.NewID("delegation")
	in := task.DelegateInput{DelegationID: id, ParentTaskRef: a.app.Scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: goal, InputRefs: []api.ContentRef{}, AgentBindingRef: profile.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{permission}, Budget: []api.Amount{{Unit: "USD", Value: "2"}}, Deadline: api.Time(time.Now().Add(3 * time.Minute)), PolicyRef: b.app.TaskPolicy.PolicyRef, ReceiverID: b.app.Scope.OwnerID}
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.app.Scope.OwnerID, CommandID: api.NewID("command"), Method: "collaboration.delegate", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(in)}
	receipt, err := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
	if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
		t.Fatalf("original actual-scope delegation: %+v %v", receipt, err)
	}
	if !configuredAgentStep(ctx, t, a, task.JobDelegation) || !configuredAgentStep(ctx, t, b, collaboration.JobRemoteCreate) {
		t.Fatal("original remote creation missing")
	}
	d, err := a.app.Task.DelegationRead(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, id)
	if err != nil {
		t.Fatal(err)
	}
	state, err := a.app.RemoteAgent.State(ctx, a.app.Scope, d)
	if err != nil || state.Task == nil {
		t.Fatalf("actual remote child mapping: %+v %v", state, err)
	}
	childID := state.Task.TaskID
	var childPublished task.ResultOutput
	childBatch := 0
	t.Logf("original parent_task=%s delegation=%s allocation=%s command=%s child_task=%s grant=%s profile=%s", parent.TaskID, id, d.AllocationRef.ObjectID, original.CommandID, childID, permission.ObjectID, profile.ProfileRef.Digest)
	for {
		// Process only the child owner. Parent goals cannot progress in this loop.
		childBatch++
		if err = configuredReportDrainBatch(ctx, t, b, childID, "child", childBatch); err != nil {
			t.Fatalf("child original work: %v", err)
		}
		actual, readErr := b.app.Task.Read(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, childID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if actual.Status == "failed" || actual.Status == "cancelled" {
			t.Fatalf("original child terminated without success: %+v", actual)
		}
		if actual.Status == "succeeded" {
			result, resultErr := b.app.Task.Result(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, childID, task.ResultInput{})
			if resultErr != nil {
				t.Fatal(resultErr)
			}
			if result.Publication == "published" {
				if result.Result.CompletionBasis != "verified" || len(result.Result.ConditionResults) != 2 {
					t.Fatalf("child claimed unverified success: %+v", result)
				}
				facts, factsErr := b.app.Task.ContextFacts(ctx, b.app.Store, b.app.Scope, b.app.UserAuth, childID)
				if factsErr != nil || len(facts.Operations) != 3 {
					t.Fatalf("child lacks inspect/save/independent-read: %+v %v", facts, factsErr)
				}
				for _, op := range facts.Operations {
					if !op.Fact.Closed || op.Fact.Effect == "unknown" || op.Fact.MayApplyLater {
						t.Fatalf("child target action not physically closed: %+v", op)
					}
				}
				t.Logf("actual child Result=%s revision=%d checks=%d operations=%d publication=%s accounting_open=%v", result.Result.ResultID, result.Result.Revision, len(result.Result.ConditionResults), len(facts.Operations), result.Publication, actual.AccountingOpen)
				saved, saveErr := os.ReadFile(filepath.Join(b.config.DataRoot, "files", "reports/parent.md"))
				if saveErr != nil || string(saved) != "# Parent independently verifies\n\nA child reply alone cannot complete this parent.\n" {
					t.Fatalf("independently observed child file differs: %q %v", saved, saveErr)
				}
				childPublished = result
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
	unchanged, err := a.app.Task.Read(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, parent.TaskID)
	if err != nil || unchanged.Status != "active" || unchanged.ResultRef != nil {
		t.Fatalf("child result became parent success: %+v %v", unchanged, err)
	}
	// Parent consumes only signed original child facts; its conditions still need
	// real operations and independently checked artifacts before final Result.
	if !configuredAgentStep(ctx, t, a, task.JobDelegation) {
		t.Fatal("original parent progress Job missing")
	}
	parentBatch := 0
	for {
		parentBatch++
		if err = configuredReportDrainBatch(ctx, t, a, parent.TaskID, "parent", parentBatch); err != nil {
			t.Fatalf("parent original work: %v", err)
		}
		actual, readErr := a.app.Task.Read(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, parent.TaskID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if actual.Status == "failed" || actual.Status == "cancelled" {
			t.Fatalf("parent did not verify its own goal: %+v", actual)
		}
		if actual.Status == "succeeded" {
			result, resultErr := a.app.Task.Result(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, parent.TaskID, task.ResultInput{})
			if resultErr != nil {
				t.Fatal(resultErr)
			}
			if result.Publication == "published" {
				if result.Result.CompletionBasis != "verified" || len(result.Result.ConditionResults) != 2 {
					t.Fatalf("parent final Result did not use own condition checks: %+v", result)
				}
				t.Logf("actual parent Result=%s revision=%d checks=%d publication=%s accounting_open=%v", result.Result.ResultID, result.Result.Revision, len(result.Result.ConditionResults), result.Publication, actual.AccountingOpen)
				facts, factsErr := a.app.Task.ContextFacts(ctx, a.app.Store, a.app.Scope, a.app.UserAuth, parent.TaskID)
				if factsErr != nil || len(facts.Operations) != 3 {
					t.Fatalf("parent lacks its own inspect/save/independent-read: %+v %v", facts, factsErr)
				}
				for _, op := range facts.Operations {
					if !op.Fact.Closed || op.Fact.Effect == "unknown" || op.Fact.MayApplyLater || op.Fact.Ref.OwnerID != a.app.Scope.OwnerID {
						t.Fatalf("parent own target action not physically closed: %+v", op)
					}
				}
				saved, saveErr := os.ReadFile(filepath.Join(a.config.DataRoot, "files", "reports/parent.md"))
				if saveErr != nil || string(saved) != "# Parent independently verifies\n\nA child reply alone cannot complete this parent.\n" {
					t.Fatalf("independently observed parent file differs: %q %v", saved, saveErr)
				}
				again, replayErr := a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
				if replayErr != nil || !api.Equal(again, receipt) {
					t.Fatalf("original delegation replay changed parent responsibility: %+v %v", again, replayErr)
				}
				configuredAgentCloseReportAccounting(ctx, t, a, b, parent.TaskID, childID, permission)
				a.reopenOriginal(t)
				b.reopenOriginal(t)
				for _, expected := range []struct {
					endpoint *configuredAgentEndpoint
					taskID   string
					result   task.ResultOutput
				}{{a, parent.TaskID, result}, {b, childID, childPublished}} {
					current, readErr := expected.endpoint.app.Task.Read(ctx, expected.endpoint.app.Store, expected.endpoint.app.Scope, expected.endpoint.app.UserAuth, expected.taskID)
					if readErr != nil || current.Status != "succeeded" || current.AccountingOpen || current.ResultRef == nil || current.ResultRef.OwnerID != expected.endpoint.app.Scope.OwnerID || current.ResultRef.ObjectID != expected.result.Result.ResultID || current.ResultRef.Revision != expected.result.Result.Revision {
						t.Fatalf("joined original report Task/fee identity changed: %+v %v", current, readErr)
					}
					published, resultErr := expected.endpoint.app.Task.Result(ctx, expected.endpoint.app.Store, expected.endpoint.app.Scope, expected.endpoint.app.UserAuth, expected.taskID, task.ResultInput{ResultRef: current.ResultRef})
					if resultErr != nil || published.Publication != "published" || !api.Equal(published.Result, expected.result.Result) || !api.Equal(published.ContentRef, expected.result.ContentRef) {
						t.Fatalf("joined original Result bytes/ref/revision changed: %+v %v", published, resultErr)
					}
					saved, saveErr := os.ReadFile(filepath.Join(expected.endpoint.config.DataRoot, "files", "reports/parent.md"))
					if saveErr != nil || string(saved) != "# Parent independently verifies\n\nA child reply alone cannot complete this parent.\n" {
						t.Fatalf("original physical report changed after join/reopen: %q %v", saved, saveErr)
					}
				}
				again, replayErr = a.app.Dispatcher.Command(ctx, a.app.UserAuth, api.Raw(original))
				if replayErr != nil || !api.Equal(again, receipt) {
					t.Fatalf("joined original delegation command changed: %+v %v", again, replayErr)
				}
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// 这里只记录真实Claim身份，不改变原Store的事务、续租或领域处理。
// 单批仍最多64项；完整观察沿原ctx和Task绝对期限，未新造工作身份。
type configuredReportClaimStore struct {
	runtime.Store
	claims            []configuredReportClaim
	missingParentGate error
}

type configuredReportClaim struct {
	JobID      string `json:"job_id"`
	Kind       string `json:"kind"`
	LeaseEpoch uint64 `json:"lease_epoch"`
}

func (s *configuredReportClaimStore) Claim(ctx context.Context, scope runtime.Scope, holder string, kinds []string, max int, lease time.Duration) ([]runtime.Work, runtime.CommitStatus, error) {
	works, status, err := s.Store.Claim(ctx, scope, holder, kinds, max, lease)
	if status == runtime.Committed && err == nil {
		for _, work := range works {
			s.claims = append(s.claims, configuredReportClaim{JobID: work.Job.JobID, Kind: work.Job.Kind, LeaseEpoch: work.Claim.LeaseEpoch})
		}
	}
	return works, status, err
}

// 观察原Savepoint返回的准确门禁错误；原Tx仍正常提交/回滚，未改领域处理。
func (s *configuredReportClaimStore) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, participants, func(tx runtime.Tx) error {
		return fn(configuredReportObservationTx{Tx: tx, observed: s})
	})
}

type configuredReportObservationTx struct {
	runtime.Tx
	observed *configuredReportClaimStore
}

func (tx configuredReportObservationTx) Peek(ctx context.Context, namespace, id string, out any) (uint64, error) {
	reader, ok := tx.Tx.(runtime.TxSnapshotReader)
	if !ok {
		return 0, api.E("unsupported", "route_snapshot_unconfigured")
	}
	return reader.Peek(ctx, namespace, id, out)
}

func (tx configuredReportObservationTx) Savepoint(ctx context.Context, fn func(runtime.Tx) error) error {
	return tx.Tx.Savepoint(ctx, func(inner runtime.Tx) error {
		err := fn(configuredReportObservationTx{Tx: inner, observed: tx.observed})
		var gate *api.Error
		if errors.As(err, &gate) && gate.Code == "dependency_unavailable" && gate.Reason == "remote_parent_scope_required" && tx.observed.missingParentGate == nil {
			tx.observed.missingParentGate = err
		}
		return err
	})
}

func configuredReportDrainBatch(ctx context.Context, t *testing.T, e *configuredAgentEndpoint, taskID, phase string, batch int) error {
	t.Helper()
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := e.app.Task.Read(ctx, e.app.Store, e.app.Scope, e.app.UserAuth, taskID)
	if err != nil {
		return err
	}
	if _, err = api.ParseTime(current.Deadline); err != nil {
		return err
	}
	observed := &configuredReportClaimStore{Store: e.app.Store, claims: make([]configuredReportClaim, 0, 64)}
	err = runtime.Drain(ctx, observed, e.app.Scope, e.app.Registry, 64)
	var boundary *api.Error
	batchExhausted := errors.As(err, &boundary) && boundary.Code == "overloaded" && boundary.Reason == "drain_limit_reached"
	distinct := make(map[string]struct{}, len(observed.claims))
	for _, claim := range observed.claims {
		distinct[claim.JobID] = struct{}{}
	}
	t.Logf("original report batch phase=%s batch=%d owner=%s task=%s status_before=%s original_deadline=%s claimed=%d distinct_jobs=%d batch_limit_reached=%v claims=%s", phase, batch, e.app.Scope.OwnerID, taskID, current.Status, current.Deadline, len(observed.claims), len(distinct), batchExhausted, api.Raw(observed.claims))
	if err != nil && !batchExhausted {
		return err
	}
	if observed.missingParentGate != nil {
		// 完整公开正例要求本次完成门禁有真实当前父证明。即时报告观察到
		// 的错误，不等原绝对期限再把它误归因于运行成本。
		return observed.missingParentGate
	}
	if batchExhausted {
		// 只识别扫描批次边界；其它领域、存储、未知提交或context错误仍返回。
		return nil
	}
	return err
}

// 费用仍由原两方Job归并；出版结果不是立即结清的替代断言。
func configuredAgentCloseReportAccounting(ctx context.Context, t *testing.T, a, b *configuredAgentEndpoint, parentID, childID string, permission api.ObjectRef) {
	t.Helper()
	batch := 0
	for {
		closed := true
		for _, endpoint := range []struct {
			e  *configuredAgentEndpoint
			id string
		}{{b, childID}, {a, parentID}} {
			actual, err := endpoint.e.app.Task.Read(ctx, endpoint.e.app.Store, endpoint.e.app.Scope, endpoint.e.app.UserAuth, endpoint.id)
			if err != nil || actual.Status != "succeeded" || actual.ResultRef == nil {
				t.Fatalf("original successful report disappeared during accounting: %+v %v", actual, err)
			}
			closed = closed && !actual.AccountingOpen
			for _, unit := range actual.Budget {
				closed = closed && unit.Reserved == "0"
			}
		}
		grant := configuredAgentGrant(ctx, t, a, permission)
		closed = closed && grant.OnceConsumed && api.Equal(grant.Reserved, []api.Amount{{Unit: "USD", Value: "0"}})
		if closed {
			t.Logf("actual original report accounting closed: parent=%s child=%s Grant=%s once=%v reserved=%v spent=%v", parentID, childID, permission.ObjectID, grant.OnceConsumed, grant.Reserved, grant.Spent)
			return
		}
		batch++
		for _, endpoint := range []struct {
			e  *configuredAgentEndpoint
			id string
		}{{b, childID}, {a, parentID}} {
			if err := configuredReportDrainBatch(ctx, t, endpoint.e, endpoint.id, "accounting", batch); err != nil {
				t.Fatalf("original report accounting work: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}
