package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
	o "github.com/ruipengliu/lerna/internal/orchestrator"
	pg "github.com/ruipengliu/lerna/internal/storage/postgres"
	filedb "github.com/ruipengliu/lerna/internal/storage/sqlite"
	"github.com/ruipengliu/lerna/internal/storage/taskstore"
)

// Trusted test assembly drives one real claimed domain job at a time. The
// engine/SQL handle remain here and never enter a public component port.
type taskProbe struct {
	e *durable.Engine
	p durable.Participant
	c *o.Coordinator
	s *suite
}
type probeUnit struct {
	p     *taskProbe
	claim durable.Claim
}

func (u probeUnit) Claim() durable.Claim { return u.claim }
func (u probeUnit) Step(ctx context.Context, fn func() error) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	return fn()
}
func (u probeUnit) Within(ctx context.Context, fn func(*durable.Tx) error) durable.Result {
	return u.p.e.Within(ctx, taskScope, []durable.Participant{u.p.p}, fn)
}
func newTaskProbe(t *testing.T, s *suite, f *taskFixture) *taskProbe {
	t.Helper()
	e, err := durable.New(s.store, durable.Options{Kinds: o.Kinds})
	if err != nil {
		t.Fatal(err)
	}
	participant, err := e.Register("orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	q := filedb.TaskQueries()
	if s.driver == "postgres" {
		q = pg.TaskQueries()
	}
	limits := o.DefaultLimits()
	limits.Backoff = time.Millisecond
	return &taskProbe{e, participant, &o.Coordinator{Repository: taskstore.New(participant, q), Ports: f.ports(), Config: o.Config{Scope: taskScope, ServiceID: o.ID("service", "tasks"), Limits: limits}}, s}
}
func (p *taskProbe) claim(t *testing.T, kind string, lease time.Duration) durable.Claim {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		claims, r := p.e.Claim(ctx(), taskScope, kind, durable.NewID("boot"), 1, lease)
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		if len(claims) > 0 {
			return claims[0]
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no domain claim", kind)
	return durable.Claim{}
}
func (p *taskProbe) handle(t *testing.T, kind string) {
	t.Helper()
	if e := p.c.Handle(ctx(), probeUnit{p, p.claim(t, kind, 3*time.Second)}); e != nil {
		t.Fatal(kind, e)
	}
}

func TestOrchestratorEightAccessPaths(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			s := newSuite(t, driver)
			f := newTaskFixture()
			f.mode = "act"
			service := newTaskService(t, s, f)
			p := newTaskProbe(t, s, f)
			counts := map[string]int64{}
			measure := func(name string, fn func()) {
				before, e := s.store.Stats(ctx(), taskScope)
				if e != nil {
					t.Fatal(e)
				}
				fn()
				after, e := s.store.Stats(ctx(), taskScope)
				if e != nil {
					t.Fatal(e)
				}
				counts[name] = after.SQL - before.SQL
			}
			var task api.Task
			measure("admission", func() { task = submitTask(t, service, f, "10") })
			p.handle(t, "decide")
			p.handle(t, "poll")
			measure("candidate_admission", func() { p.handle(t, "poll") })
			measure("fact_merge", func() { p.handle(t, "dispatch") })
			rev := readTask(t, service, task.TaskID).Revision
			command := api.Command{ExpectedRevision: &rev, CommandID: durable.NewID("command"), Method: "task.pause", TargetID: task.TaskID, Payload: o.Raw(api.TaskPauseInput{Reason: "measure bounded propagation"}), ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
			measure("control_propagation", func() { executeTask(t, service, command); p.handle(t, "control") })
			rev = readTask(t, service, task.TaskID).Revision
			command.ExpectedRevision = &rev
			command.CommandID = durable.NewID("command")
			command.Method = "task.resume"
			command.Payload = o.Raw(api.TaskResumeInput{Reason: "continue fixed original"})
			executeTask(t, service, command)
			p.handle(t, "control")
			measure("accounting", func() { p.handle(t, "settle") })
			p.handle(t, "decide")
			p.handle(t, "poll")
			p.handle(t, "poll")
			measure("completion", func() { p.handle(t, "verify") })
			measure("jobs_claim_recovery", func() {
				if e := service.Recover(ctx()); e != nil {
					t.Fatal(e)
				}
			})
			measure("list", func() {
				_, e := service.Query(ctx(), taskCaller, api.Query{Method: "task.list", TargetID: taskScope.OwnerID, Payload: o.Raw(api.TaskListInput{Limit: 1})})
				if e != nil {
					t.Fatal(e)
				}
			})
			// Closed history never participates in a current completion collection.
			for n := 0; n < 256; n++ {
				_, e := s.raw.ExecContext(ctx(), s.placeholders("INSERT INTO orchestrator_records(tenant_id,owner_id,kind,id,task_id,revision,state,current_key,immutable,data) VALUES($1,$2,'check',$3,$4,1,'closed','',true,'{}')"), taskScope.TenantID, taskScope.OwnerID, fmt.Sprintf("history-%04d", n), task.TaskID)
				if e != nil {
					t.Fatal(e)
				}
			}
			before, _ := s.store.Stats(ctx(), taskScope)
			r := p.e.Within(ctx(), taskScope, []durable.Participant{p.p}, func(tx *durable.Tx) error {
				if e := p.c.Repository.Read(tx); e != nil {
					return e
				}
				rows, e := p.c.Repository.Records(tx, o.Filter{TaskID: task.TaskID, Kind: "check", Current: "@current", Limit: 101})
				if e == nil && len(rows) != 1 {
					return fmt.Errorf("current set polluted by history: %d", len(rows))
				}
				return e
			})
			mustCommit(t, r)
			after, _ := s.store.Stats(ctx(), taskScope)
			counts["current_set_after_256_history"] = after.SQL - before.SQL
			if len(counts) != 9 {
				t.Fatal("missing access paths")
			}
			b, _ := json.Marshal(counts)
			t.Logf("actual SQL calls, including clock/claim/qualification transactions: %s", b)
		})
	}
}
func TestOrchestratorStaleClaimRollsBackDomainRecovery(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			s := newSuite(t, driver)
			f := newTaskFixture()
			service := newTaskService(t, s, f)
			task := submitTask(t, service, f, "10")
			p := newTaskProbe(t, s, f)
			old := p.claim(t, "decide", 100*time.Millisecond)
			time.Sleep(120 * time.Millisecond)
			newer := p.claim(t, "decide", time.Second)
			r := p.e.Within(ctx(), taskScope, []durable.Participant{p.p}, func(tx *durable.Tx) error {
				if e := p.c.Repository.Read(tx); e != nil {
					return e
				}
				t, e := p.c.Repository.Task(tx, task.TaskID)
				if e != nil {
					return e
				}
				if _, e = p.c.Repository.LockTasks(tx, []*o.TaskState{t}); e != nil {
					return e
				}
				t.Task.Revision++
				if e = p.c.Repository.SaveTask(tx, t); e != nil {
					return e
				}
				_, e = tx.Finish(old, durable.Done())
				return e
			})
			if r.Outcome != durable.RolledBack || !errors.Is(r.Err, durable.ErrClaim) {
				t.Fatal(r)
			}
			if readTask(t, service, task.TaskID).Revision != task.Revision || len(readDomainRecords(t, s, "work")) != 1 {
				t.Fatal("stale recovery created another responsibility")
			}
			if e := p.c.Handle(ctx(), probeUnit{p, newer}); e != nil {
				t.Fatal(e)
			}
			if len(readDomainRecords(t, s, "snapshot_preparation")) != 1 {
				t.Fatal("new holder lost original continuation")
			}
		})
	}
}
