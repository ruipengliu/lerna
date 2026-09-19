package control

import (
	"context"
	"encoding/json"
	"fmt"
	sqliteauth "lerna/adapters/authorization/sqlite"
	"lerna/authorization"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var crashPoints = []string{"after-start", "before-control", "after-control", "before-settle", "settle-lost-reply"}

type probePlan struct {
	Token       string
	Now         time.Time
	Started     tasks.RunSnapshot
	Request     tasks.ControlRequest
	Observation tasks.Observation
}
type crashStore struct {
	authorization.Store
	point string
	armed bool
}

func (s *crashStore) Commit(ctx context.Context, v uint64, state authorization.State) error {
	if !s.armed {
		return s.Store.Commit(ctx, v, state)
	}
	if s.point == "before-control" {
		os.Exit(73)
	}
	err := s.Store.Commit(ctx, v, state)
	if err == nil {
		os.Exit(73)
	}
	return err
}

// RunProbe executes only against a disposable fixture directory prepared by the
// conformance parent. It never connects to a real external execution target.
func RunProbe(ctx context.Context, dir, point string) error {
	valid := false
	for _, p := range crashPoints {
		valid = valid || p == point
	}
	if !valid {
		return fmt.Errorf("unknown probe")
	}
	data, err := os.ReadFile(filepath.Join(dir, "plan.json"))
	if err != nil {
		return err
	}
	var plan probePlan
	if err = json.Unmarshal(data, &plan); err != nil {
		return err
	}
	db, err := sqliteauth.Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	store := &crashStore{Store: db, point: point, armed: point == "before-control" || point == "after-control"}
	h, err := assemble(store, dir, plan.Token, &clock{now: plan.Now})
	if err != nil {
		return err
	}
	if point == "after-start" {
		p, err := h.port("worker-a")
		if err != nil {
			return err
		}
		if _, err = p.Commit(ctx, mutation("child-start", "start", plan.Started)); err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, "source-state"), []byte("STOPPED"), 0600); err != nil {
			return err
		}
		os.Exit(73)
	}
	c, err := h.controls()
	if err != nil {
		return err
	}
	if _, err = c.Request(ctx, h.token, plan.Request); err != nil {
		return err
	}
	// A separate fixture file models source state which survives the controller
	// process. This is simulated disposition evidence, not an external API claim.
	if err = os.WriteFile(filepath.Join(dir, "source-state"), []byte("STOPPED"), 0600); err != nil {
		return err
	}
	if point == "before-settle" {
		os.Exit(73)
	}
	p, err := h.port("worker-a")
	if err != nil {
		return err
	}
	store.armed = true
	if _, err = p.Observe(ctx, plan.Observation); err != nil {
		return err
	}
	return fmt.Errorf("probe did not terminate")
}
func crashCheck(ctx context.Context, executable, point string) error {
	dir, err := os.MkdirTemp("", "lerna-control-crash-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	h, err := setup(ctx, dir)
	if err != nil {
		return err
	}
	initial, err := h.submit(ctx, 3)
	if err != nil {
		h.db.Close()
		return err
	}
	var started tasks.RunSnapshot
	if point == "after-start" {
		p, e := h.port("worker-a")
		if e != nil {
			h.db.Close()
			return e
		}
		started, err = p.Commit(ctx, mutation("claim", "claim", initial))
	} else {
		_, started, err = h.start(ctx, initial, false)
	}
	if err != nil {
		h.db.Close()
		return err
	}
	in, err := h.request(ctx, started.Task, "CANCEL")
	if err != nil {
		h.db.Close()
		return err
	}
	now, _ := h.clock.Now()
	plan := probePlan{Token: h.token, Now: now, Started: started, Request: in, Observation: tasks.Observation{ChangeID: "probe-stopped", Qualification: tasks.QualificationOf(started), Status: "STOPPED"}}
	data, err := json.Marshal(plan)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "plan.json"), data, 0600)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "source-state"), []byte("IN_PROGRESS"), 0600)
	}
	h.db.Close()
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, executable, "control-crash-probe", dir, point)
	output, err := cmd.CombinedOutput()
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 73 {
		return fmt.Errorf("probe: %v %s", err, output)
	}
	h, err = open(dir, plan.Token, &clock{now: plan.Now})
	if err != nil {
		return err
	}
	defer h.db.Close()
	c, err := h.controls()
	if err != nil {
		return err
	}
	if point == "after-start" {
		pending, err := c.ListPending(ctx, h.token, tasks.RecoveryQuery{Namespace: "local", Limit: 10})
		if err != nil {
			return err
		}
		if len(pending.Runs) != 1 || pending.Runs[0].Task.Control.OperationID != "" {
			return fmt.Errorf("start crash not discoverable")
		}
		if _, err = c.PrepareDisposition(ctx, h.token, started.Task.Ref); err != nil {
			return err
		}
		fact, err := os.ReadFile(filepath.Join(dir, "source-state"))
		if err != nil {
			return err
		}
		if string(fact) != "STOPPED" {
			return fmt.Errorf("missing source stop fact")
		}
		p, err := h.port("worker-a")
		if err != nil {
			return err
		}
		recovered, err := p.Observe(ctx, plan.Observation)
		if err != nil {
			return err
		}
		runner, err := tasks.NewRunner(p, tasks.BrainFunc(script), limits(), h.clock)
		if err != nil {
			return err
		}
		done, err := runner.Run(ctx, recovered)
		if err != nil {
			return err
		}
		return require(done.Task.State == "COMPLETED" && done.Task.Attempts == 2 && done.Work[0].ID == started.Work[0].ID, "start crash lost bounded retry")
	}
	receipt, lookupErr := c.Lookup(ctx, h.token, "local", in.OperationID)
	current, err := h.service.Load(ctx, started.Task.Ref)
	if err != nil {
		return err
	}
	if point == "before-control" {
		if err = expect(lookupErr, authorization.NotFound); err != nil {
			return err
		}
		if err = require(current.Task.Version == started.Task.Version && current.Task.Control.OperationID == "", "partial control survived"); err != nil {
			return err
		}
		// The child has exited; its original transaction can no longer commit.
		receipt, err = c.Request(ctx, h.token, in)
		if err != nil {
			return err
		}
	} else {
		if lookupErr != nil {
			return lookupErr
		}
		if err = require(current.Task.Control.Intent == "CANCEL", "restart forgot cancel"); err != nil {
			return err
		}
	}
	again, err := c.Request(ctx, h.token, in)
	if err != nil {
		return err
	}
	if receipt != again {
		return fmt.Errorf("recovery duplicated control")
	}
	p, err := h.port("worker-a")
	if err != nil {
		return err
	}
	if point == "settle-lost-reply" {
		original, err := h.service.LookupCommit(ctx, started.Task.Ref, plan.Observation.ChangeID)
		if err != nil {
			return err
		}
		replay, err := p.Observe(ctx, plan.Observation)
		if err != nil {
			return err
		}
		if original != replay.LastCommit {
			return fmt.Errorf("settlement replay changed identity")
		}
	} else {
		pending, err := c.ListPending(ctx, h.token, tasks.RecoveryQuery{Namespace: "local", Limit: 10})
		if err != nil {
			return err
		}
		if len(pending.Runs) != 1 {
			return fmt.Errorf("lost pending disposition")
		}
		sourceState, err := os.ReadFile(filepath.Join(dir, "source-state"))
		if err != nil {
			return err
		}
		if point == "before-settle" && string(sourceState) != "STOPPED" {
			return fmt.Errorf("lost independent source fact")
		}
		if string(sourceState) != "STOPPED" {
			if err = os.WriteFile(filepath.Join(dir, "source-state"), []byte("STOPPED"), 0600); err != nil {
				return err
			}
		}
		if _, err = p.Observe(ctx, plan.Observation); err != nil {
			return err
		}
	}
	got, err := h.service.Get(ctx, h.token, started.Task.Ref)
	if err != nil {
		return err
	}
	run, err := h.service.Load(ctx, started.Task.Ref)
	if err != nil {
		return err
	}
	return require(got.State == "CANCELLED" && got.Control.Progress == "APPLIED" && got.Attempts == 1 && run.Work[0].ID == started.Work[0].ID && !run.Work[0].InFlight, "recovered control/settlement/budget differs")
}
