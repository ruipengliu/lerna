package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var crashPoints = []string{"before-claim", "after-claim", "proposal-before-admission", "completion-lost-reply"}

type probePlan struct {
	Token   string
	Now     time.Time
	Initial tasks.RunSnapshot
	Claim   tasks.WorkChange
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
	if s.point == "before-claim" {
		os.Exit(73)
	}
	err := s.Store.Commit(ctx, v, state)
	if err == nil {
		os.Exit(73)
	}
	return err
}

// RunProbe is an explicit disposable verification subprocess, never a service RPC.
func RunProbe(ctx context.Context, dir, point string) error {
	valid := false
	for _, p := range crashPoints {
		valid = valid || point == p
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
	store := &crashStore{Store: db, point: point, armed: point == "before-claim" || point == "after-claim"}
	h, err := assemble(store, dir, plan.Token, &clock{now: plan.Now})
	if err != nil {
		return err
	}
	p, err := h.port("worker-a")
	if err != nil {
		return err
	}
	claimed, err := p.Commit(ctx, plan.Claim)
	if err != nil {
		return err
	}
	proposal, err := script(ctx, tasks.DecisionInput{Task: claimed.Task, Work: claimed.Work[0], RemainingSteps: claimed.Task.Constraints.MaxSteps - claimed.Task.Attempts})
	if err != nil {
		return err
	}
	if point == "proposal-before-admission" {
		os.Exit(73)
	}
	store.armed = true
	complete := mutation("probe-complete", "complete", claimed)
	complete.Proposal = proposal
	if _, err = p.Commit(ctx, complete); err != nil {
		return err
	}
	return fmt.Errorf("probe did not terminate")
}
func crashCheck(ctx context.Context, executable, point string) error {
	dir, err := os.MkdirTemp("", "lerna-worker-crash-")
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
	now, _ := h.clock.Now()
	plan := probePlan{Token: h.token, Now: now, Initial: initial, Claim: mutation("probe-claim", "claim", initial)}
	data, err := json.Marshal(plan)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "plan.json"), data, 0600)
	}
	h.db.Close()
	if err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, executable, "worker-crash-probe", dir, point)
	output, err := command.CombinedOutput()
	if command.ProcessState == nil || command.ProcessState.ExitCode() != 73 {
		return fmt.Errorf("probe exit: %v %s", err, output)
	}
	h, err = open(dir, plan.Token, &clock{now: now})
	if err != nil {
		return err
	}
	defer h.db.Close()
	p, err := h.port("worker-a")
	if err != nil {
		return err
	}
	run, err := h.service.Load(ctx, initial.Task.Ref)
	if err != nil {
		return err
	}
	claim, lookupErr := p.Lookup(ctx, initial.Task.Ref, plan.Claim.ChangeID)
	if point == "before-claim" {
		// The child has exited, so its pending database call cannot later commit.
		if err = expect(lookupErr, authorization.NotFound); err != nil {
			return err
		}
		if err = require(run.Task.State == "QUEUED" && run.Task.Attempts == 0 && run.Work[0].Generation == 0, "partial claim survived"); err != nil {
			return err
		}
		claim, err = p.Commit(ctx, plan.Claim)
		if err != nil {
			return err
		}
		run, err = complete(ctx, p, "recovered-complete", claim)
		if err != nil {
			return err
		}
	} else {
		if lookupErr != nil {
			return lookupErr
		}
		if err = require(claim.Task.Attempts == 1 && claim.Work[0].ID == initial.Work[0].ID, "claim journal missing"); err != nil {
			return err
		}
		if point == "completion-lost-reply" {
			completion := mutation("probe-complete", "complete", claim)
			completion.Proposal = proposal(claim)
			recovered, err := p.Lookup(ctx, initial.Task.Ref, completion.ChangeID)
			if err != nil {
				return err
			}
			replay, err := p.Commit(ctx, completion)
			if err != nil {
				return err
			}
			if err = require(recovered.LastCommit == replay.LastCommit && replay.Task.Attempts == 1 && replay.Task.State == "COMPLETED", "completion republished or lost"); err != nil {
				return err
			}
			_, err = p.Commit(ctx, mutation("terminal-claim", "claim", replay))
			if err = expect(err, authorization.Conflict); err != nil {
				return err
			}
			run = replay
		} else {
			if err = require(run.Task.State == "RUNNING" && run.Task.Attempts == 1 && run.Task.Result == "", "unadmitted proposal published"); err != nil {
				return err
			}
			h.clock.advance(2 * time.Second)
			runner, err := tasks.NewRunner(p, tasks.BrainFunc(script), limits(), h.clock)
			if err != nil {
				return err
			}
			run, err = runner.Run(ctx, run)
			if err != nil {
				return err
			}
			if err = require(run.Task.Attempts == 2 && run.Work[0].Generation == 2, "restart reset uncertain attempt"); err != nil {
				return err
			}
		}
	}
	got, err := h.client.Get(ctx, initial.Task.Ref)
	if err != nil {
		return err
	}
	receipt, err := h.service.LookupCommit(ctx, initial.Task.Ref, run.LastCommit.ChangeID)
	if err != nil {
		return err
	}
	return require(got.State == "COMPLETED" && got.Result == "scripted answer" && run.Work[0].ID == initial.Work[0].ID && run.Work[0].Done && receipt == run.LastCommit, "recovered atomic bundle differs")
}
