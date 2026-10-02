package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
	o "github.com/ruipengliu/lerna/internal/orchestrator"
)

func TestOrchestratorStrictBatchAndLateDebt(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver+"/batch", func(t *testing.T) {
			s := newSuite(t, driver)
			f := newTaskFixture()
			f.mode = "act"
			f.policy.DecisionUpperBound[0].Amount = "0.2"
			service := newTaskService(t, s, f)
			task := submitTask(t, service, f, "0.3")
			runTaskService(t, service)
			task = awaitTask(t, service, f, task.TaskID, func(v api.Task) bool {
				for _, w := range v.WaitReasons {
					if w.Kind == "budget" {
						return true
					}
				}
				return false
			})
			f.mu.Lock()
			calls := f.physicalOperations
			f.mu.Unlock()
			if calls != 0 {
				t.Fatal("unfunded batch invoked", calls)
			}
			var count int
			if e := s.raw.QueryRowContext(ctx(), "SELECT count(*) FROM orchestrator_records WHERE kind='intent'").Scan(&count); e != nil || count != 0 {
				t.Fatal("partial batch", count, e)
			}
			cmd := api.Command{CommandID: durable.NewID("command"), Method: "task.adjust_budget", TargetID: task.TaskID, ExpectedRevision: &task.Revision, ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), Payload: o.Raw(api.TaskAdjustBudgetInput{Limits: []api.BudgetLimit{{Unit: "usd", Limit: "2"}}})}
			executeTask(t, service, cmd)
			awaitTask(t, service, f, task.TaskID, func(v api.Task) bool { return v.Status == "succeeded" })
		})
		t.Run(driver+"/debt", func(t *testing.T) {
			s := newSuite(t, driver)
			f := newTaskFixture()
			service := newTaskService(t, s, f)
			task := submitTask(t, service, f, "1")
			decisionID := o.ID("decision", task.TaskID, "1", "1")
			// The original identity is taken from its persisted snapshot, never guessed
			// by the accounting source. Prepare first with a synchronous domain unit.
			p := newTaskProbe(t, s, f)
			p.handle(t, "decide")
			rows := readDomainRecords(t, s, "snapshot_preparation")
			var prep o.SnapshotPreparation
			_ = json.Unmarshal(rows[0], &prep)
			decisionID = prep.Snapshot.Request.DecisionID
			f.mu.Lock()
			proof := f.put("pendingbill", []byte("pending"))
			bill := api.Billing{Source: api.SourceKey{OwnerID: f.policy.BrainID, Kind: "model_call", ID: o.ID("model_call", decisionID)}, Revision: 1, Digest: o.Hash("pending"), Usage: []api.Amount{{Unit: "usd", Amount: "0.1"}}, Final: false, ProofRef: proof}
			f.usage[decisionID] = bill
			f.mu.Unlock()
			runTaskService(t, service)
			task = awaitTask(t, service, f, task.TaskID, func(v api.Task) bool { return v.Status == "succeeded" && v.Budget[0].Spent.Amount == "0.1" })
			if !task.AccountingOpen || task.Budget[0].Reserved.Amount != "0.9" {
				t.Fatal("unknown remainder released", task)
			}
			original := queryResult(t, service, task.TaskID)
			f.mu.Lock()
			bill.Revision = 2
			bill.Usage[0].Amount = "1.5"
			bill.Final = true
			bill.Digest = o.Hash("late-debt")
			bill.ProofRef = f.put("latebill", []byte("final"))
			f.usage[decisionID] = bill
			f.mu.Unlock()
			source := api.Caller{TenantID: taskScope.TenantID, ActorID: taskScope.OwnerID, SourceOwnerID: f.policy.BrainID}
			q := api.BillingQuery{TaskRef: api.TaskRef{OrchestratorID: taskScope.OwnerID, TaskID: task.TaskID}, ObjectOwnerID: f.policy.BrainID, ObjectKind: "decision", ObjectID: decisionID}
			if e := service.ReconcileBilling(ctx(), source, q); e != nil {
				t.Fatal(e)
			}
			if e := service.ReconcileBilling(ctx(), source, q); e != nil {
				t.Fatal(e)
			}
			task = readTask(t, service, task.TaskID)
			if task.Status != "succeeded" || task.AccountingOpen || task.Budget[0].Spent.Amount != "1.5" || task.Budget[0].Reserved.Amount != "0" {
				t.Fatal("debt not fixed", task)
			}
			if original != queryResult(t, service, task.TaskID) {
				t.Fatal("late fee reopened result")
			}
			f.mu.Lock()
			bill.Revision = 3
			bill.Usage[0].Amount = "0.9"
			bill.Digest = o.Hash("refund")
			f.usage[decisionID] = bill
			f.mu.Unlock()
			if e := service.ReconcileBilling(ctx(), source, q); e == nil {
				t.Fatal("uncontracted refund accepted")
			}
			if readTask(t, service, task.TaskID).Budget[0].Spent.Amount != "1.5" {
				t.Fatal("rejected refund changed ledger")
			}
		})
	}
}
func TestOrchestratorLocalChildrenAndExtraction(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			s := newSuite(t, driver)
			f := newTaskFixture()
			f.policy.ExtractionOwnerID = o.ID("memory", "tests")
			f.policy.ExtractionAuthorizationRefs = f.policy.UsageAuthorizationRefs
			service := newTaskService(t, s, f)
			task := submitTask(t, service, f, "10")
			f.rootTask = task.TaskID
			f.propose = func(in api.DecisionRequest) *api.Proposal {
				if in.TaskID != f.rootTask || in.SnapshotRevision != 1 {
					return nil
				}
				d := api.DelegationRequest{DelegationID: o.ID("delegation", "proposed"), ParentTaskID: in.TaskID, AgentBinding: api.AgentBinding{AgentRef: f.policy.Ref, AdapterRef: f.policy.Ref, EndpointID: o.ID("endpoint", "child")}, GoalRef: f.goal, InputRefs: []api.ContentRef{}, Constraints: []string{}, Deadline: task.Deadline, AllocationID: o.ID("allocation", "proposed"), PermissionRefs: f.policy.UsageAuthorizationRefs, AncestorIDs: []string{}}
				actions := []json.RawMessage{o.Raw(api.ActionDelegate{ActionKey: "child", Type: "delegate", Purpose: "bounded local child", RequirementRefs: []string{}, EvidenceRefs: []api.ContentRef{}, Delegation: d})}
				return &api.Proposal{Kind: "act", Rationale: "delegate original", EvidenceRefs: []api.ContentRef{}, Assumptions: []string{}, Actions: &actions}
			}
			runTaskService(t, service)
			task = awaitTask(t, service, f, task.TaskID, func(v api.Task) bool { return v.Status == "succeeded" && !v.AccountingOpen })
			if task.Budget[0].Spent.Amount != "0.6" || task.Budget[0].Reserved.Amount != "0" {
				t.Fatal("child settlement", task.Budget)
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				f.mu.Lock()
				calls := len(f.extractionCalls)
				f.mu.Unlock()
				if calls >= 2 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			f.mu.Lock()
			calls := append([]api.OriginalCall{}, f.extractionCalls...)
			f.mu.Unlock()
			if len(calls) != 2 {
				t.Fatal("independent extraction missing", len(calls))
			}
			for _, call := range calls {
				if !strings.Contains(string(call.Command.Payload), "source_refs") {
					t.Fatal("unfixed extraction source")
				}
			}
			var receivers int
			if e := s.raw.QueryRowContext(ctx(), "SELECT count(*) FROM orchestrator_receivers").Scan(&receivers); e != nil || receivers != 1 {
				t.Fatal("atomic child receiver", receivers, e)
			}
		})
	}
}

func readDomainRecords(t *testing.T, s *suite, kind string) []json.RawMessage {
	t.Helper()
	rows, e := s.raw.QueryContext(ctx(), s.placeholders("SELECT data FROM orchestrator_records WHERE kind=$1 ORDER BY id"), kind)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	values := []json.RawMessage{}
	for rows.Next() {
		var b string
		if e = rows.Scan(&b); e != nil {
			t.Fatal(e)
		}
		values = append(values, json.RawMessage(b))
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	return values
}
func queryResult(t *testing.T, s api.Orchestrator, id string) string {
	t.Helper()
	r, e := s.Query(ctx(), taskCaller, api.Query{Method: "task.result", TargetID: id, Payload: o.Raw(api.TaskResultInput{})})
	if e != nil {
		t.Fatal(e)
	}
	return string(r.Value)
}
