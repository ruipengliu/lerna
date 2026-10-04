package decision_engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

type Work struct {
	Claim      runtime.Claim
	Record     Record
	Permission Permission
}

func terminal(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}
func failRecord(record *Record, reason v.DecisionFailure) {
	record.Revision++
	record.Status = "failed"
	record.Failure = &reason
	record.Proposal = nil
	record.ProposalRef = nil
}
func (s *Service) pool(ctx context.Context) (workpool.State, string, time.Time, error) {
	var state workpool.State
	var scope string
	var now time.Time
	err := s.config.Store.Within(ctx, oldOwner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		var err error
		state, err = s.config.Store.LockPool(ctx, tx)
		if err != nil {
			return err
		}
		scope, err = s.config.Store.PoolScope(ctx, tx)
		if err != nil {
			return err
		}
		now, err = s.config.Store.Now(ctx, tx)
		return err
	})
	return state, scope, now, err
}

// Maintain expiry visits every explicitly registered member with bounded pages.
// It closes existing responsibility without needing an ordinary execution slot.
func (s *Service) Maintain(ctx context.Context) (int, error) {
	if err := finite(ctx); err != nil {
		return 0, err
	}
	state, _, _, err := s.pool(ctx)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, member := range state.Config.Members {
		err = s.config.Store.Within(ctx, member, func(ctx context.Context, tx runtime.Tx) error {
			if _, err := s.config.Store.LockPool(ctx, tx); err != nil {
				return err
			}
			now, err := s.config.Store.Now(ctx, tx)
			if err != nil {
				return err
			}
			jobs, err := s.config.Store.ExpiredCandidates(ctx, tx, now, 64)
			if err != nil {
				return err
			}
			for _, job := range jobs {
				record, err := s.config.Store.LockDecision(ctx, tx, fromObject(job.Object))
				if err != nil {
					return err
				}
				now, err = s.config.Store.Now(ctx, tx)
				if err != nil {
					return err
				}
				if record == nil || terminal(record.Status) || record.Input == nil {
					continue
				}
				deadline, err := fixedTime(record.Input.Deadline)
				if err != nil {
					return err
				}
				if now.Before(deadline) {
					continue
				}
				failRecord(record, "deadline_elapsed")
				if err = s.config.Store.SaveDecision(ctx, tx, *record); err != nil {
					return err
				}
				if err = s.config.Store.StopRevision(ctx, tx, job, job.WorkRevision); err != nil {
					return err
				}
				closed++
			}
			return nil
		})
		if err != nil {
			return closed, err
		}
	}
	return closed, nil
}

// Claim routes by the durable pool FIFO, then rechecks that exact scope and all
// current capacity under the selected member's owner transaction.
func (s *Service) Claim(ctx context.Context) (*Work, error) {
	if err := finite(ctx); err != nil {
		return nil, err
	}
	state, scope, _, err := s.pool(ctx)
	if err != nil {
		return nil, err
	}
	routePool := state.Config.ID
	var candidate *runtime.Job
	var cursor workpool.Cursor
	err = s.config.Store.Within(ctx, oldOwner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		current, err := s.config.Store.LockPool(ctx, tx)
		if err != nil {
			return err
		}
		state = current
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		limit, err := state.Config.Limit("ordinary")
		if err != nil {
			return err
		}
		active, _, _, err := s.config.Store.PoolCounts(ctx, tx, state, "ordinary", contract.ID(s.config.Owner.TenantID), now)
		if err != nil {
			return err
		}
		if active >= limit.Concurrent {
			return nil
		}
		ready, err := s.config.Store.PoolReadyTenants(ctx, tx, state, "ordinary", now)
		if err != nil {
			return err
		}
		cursor = workpool.RefreshCursor(state.Cursors["ordinary"], state.Config.Members, ready, now)
		if len(cursor.Order) == 0 {
			return nil
		}
		jobs, through, err := s.config.Store.PoolPage(ctx, tx, state, "ordinary", cursor.Order[0], cursor.After, cursor.Through, now)
		if err != nil {
			return err
		}
		cursor.Through = through
		if len(jobs) > 0 {
			job := jobs[0]
			candidate = &job
		}
		return nil
	})
	if err != nil || candidate == nil {
		return nil, err
	}
	ref := fromObject(candidate.Object)
	var observation *Record
	err = s.config.Store.Within(ctx, oldOwner(decisionOwner(ref)), func(ctx context.Context, tx runtime.Tx) error {
		var err error
		observation, err = s.config.Store.ReadDecision(ctx, tx, ref)
		return err
	})
	if err != nil {
		return nil, err
	}
	if observation == nil || observation.Input == nil || terminal(observation.Status) {
		return nil, nil
	}
	permission, err := s.config.Authority.Authorize(ctx, observation.Subject, ref, "start", observation.Input)
	if err != nil {
		return nil, err
	}
	if permission.DecisionOwner != decisionOwner(ref) || observation.Input.ComponentRef != s.config.Component {
		return nil, ErrForbidden
	}
	var work *Work
	err = s.config.Store.Within(ctx, oldOwner(decisionOwner(ref)), func(ctx context.Context, tx runtime.Tx) error {
		state, err := s.config.Store.LockPool(ctx, tx)
		if err != nil {
			return err
		}
		currentScope, err := s.config.Store.PoolScope(ctx, tx)
		if err != nil {
			return err
		}
		if currentScope != scope || state.Config.ID != routePool {
			return runtime.ErrScope
		}
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = permissionCurrent(permission, now); err != nil {
			return err
		}
		limit, err := state.Config.Limit("ordinary")
		if err != nil {
			return err
		}
		active, own, _, err := s.config.Store.PoolCounts(ctx, tx, state, "ordinary", candidate.Object.TenantID, now)
		if err != nil {
			return err
		}
		if active >= limit.Concurrent || own >= state.Config.Quota(candidate.Object.TenantID, "ordinary") {
			return nil
		}
		ready, err := s.config.Store.PoolReadyTenants(ctx, tx, state, "ordinary", now)
		if err != nil {
			return err
		}
		cursor = workpool.RefreshCursor(state.Cursors["ordinary"], state.Config.Members, ready, now)
		if len(cursor.Order) == 0 || cursor.Order[0] != candidate.Object.TenantID {
			return nil
		}
		record, err := s.config.Store.LockDecision(ctx, tx, ref)
		if err != nil {
			return err
		}
		if record == nil || terminal(record.Status) || record.Input == nil || record.InputDigest != observation.InputDigest {
			return nil
		}
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		deadline, err := fixedTime(record.Input.Deadline)
		if err != nil {
			return err
		}
		if !now.Before(deadline) {
			return nil
		}
		until := now.Add(s.config.Lease)
		if deadline.Before(until) {
			until = deadline
		}
		claim, err := s.config.Store.Claim(ctx, tx, *candidate, s.config.Worker, now, until)
		if err != nil || claim == nil {
			return err
		}
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = s.config.Store.ValidateClaim(ctx, tx, *claim, now); err != nil {
			return err
		}
		if err = permissionCurrent(permission, now); err != nil {
			return err
		}
		if err = s.config.Store.RegisterPoolClaim(ctx, tx, state, *claim); err != nil {
			return err
		}
		if err = s.config.Store.ValidatePoolClaim(ctx, tx, state, *claim, now); err != nil {
			return err
		}
		if err = workpool.RotateCursor(&cursor, true); err != nil {
			return err
		}
		if err = s.config.Store.SavePoolCursor(ctx, tx, state, "ordinary", cursor); err != nil {
			return err
		}
		work = &Work{Claim: *claim, Record: *record, Permission: permission}
		return nil
	})
	return work, err
}
func (s *Service) Start(ctx context.Context, claim runtime.Claim) (*Work, error) {
	if err := finite(ctx); err != nil {
		return nil, err
	}
	if claim.Worker != s.config.Worker || claim.Object.Kind != "decision" || claim.Phase != "decide" {
		return nil, runtime.ErrClaim
	}
	ref := fromObject(claim.Object)
	var observation *Record
	err := s.config.Store.Within(ctx, oldOwner(decisionOwner(ref)), func(ctx context.Context, tx runtime.Tx) error {
		var err error
		observation, err = s.config.Store.ReadDecision(ctx, tx, ref)
		return err
	})
	if err != nil {
		return nil, err
	}
	if observation == nil || observation.Input == nil || terminal(observation.Status) {
		return nil, runtime.ErrClaim
	}
	permission, err := s.config.Authority.Authorize(ctx, observation.Subject, ref, "start", observation.Input)
	if err != nil {
		return nil, err
	}
	if permission.DecisionOwner != decisionOwner(ref) || observation.Input.ComponentRef != s.config.Component {
		return nil, ErrForbidden
	}
	var work *Work
	err = s.config.Store.Within(ctx, oldOwner(decisionOwner(ref)), func(ctx context.Context, tx runtime.Tx) error {
		state, err := s.config.Store.LockPool(ctx, tx)
		if err != nil {
			return err
		}
		record, err := s.config.Store.LockDecision(ctx, tx, ref)
		if err != nil {
			return err
		}
		if record == nil || terminal(record.Status) || record.InputDigest != observation.InputDigest {
			return runtime.ErrClaim
		}
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = s.config.Store.ValidateClaim(ctx, tx, claim, now); err != nil {
			return err
		}
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = s.config.Store.ValidateClaim(ctx, tx, claim, now); err != nil {
			return err
		}
		if err = s.config.Store.ValidatePoolClaim(ctx, tx, state, claim, now); err != nil {
			return err
		}
		if err = permissionCurrent(permission, now); err != nil {
			return err
		}
		deadline, err := fixedTime(record.Input.Deadline)
		if err != nil {
			return err
		}
		if !now.Before(deadline) {
			return runtime.ErrClaim
		}
		record.Status = "running"
		record.StartedEpoch = claim.Epoch
		record.Revision++
		if err = s.config.Store.SaveDecision(ctx, tx, *record); err != nil {
			return err
		}
		work = &Work{Claim: claim, Record: *record, Permission: permission}
		return nil
	})
	return work, err
}
func withinLimit(value int, limit v.Revision) bool {
	max, err := strconv.ParseUint(string(limit), 10, 64)
	return err == nil && uint64(value) <= max
}
func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type completion struct {
	proposal     *v.Proposal
	proposalRef  *v.ContentRef
	artifactRefs []v.ContentRef
	usage        v.DecisionUsage
	failure      *v.DecisionFailure
}

func failedCompletion(reason v.DecisionFailure, usage v.DecisionUsage) completion {
	return completion{failure: &reason, usage: usage}
}
func (s *Service) calculate(ctx context.Context, work Work) completion {
	input := work.Record.Input
	usage := work.Record.Usage
	snapshot, err := s.config.Source.ReadSnapshot(ctx, input.SnapshotRef, work.Permission)
	if err != nil {
		return failedCompletion("snapshot_unavailable", usage)
	}
	if snapshot.Ref != input.SnapshotRef || snapshot.ComponentRef != input.ComponentRef || snapshot.TaskRef.ID != input.TaskRef.ID || snapshot.TaskRef.OwnerID != input.TaskRef.OwnerID || snapshot.TaskRef.TenantID != input.TaskRef.TenantID || len(snapshot.MaterialRefs) < 1 || len(snapshot.MaterialRefs) > 64 || len(snapshot.RequirementRefs) < 1 || len(snapshot.RequirementRefs) > 64 {
		return failedCompletion("snapshot_unavailable", usage)
	}
	lockBytes, err := s.config.Source.ReadFixtureLock(ctx, input.ComponentRef.InstallLockRef, work.Permission)
	if err != nil {
		return failedCompletion("snapshot_unavailable", usage)
	}
	inputBytes := len(snapshot.Raw) + len(lockBytes)
	var first []byte
	for index, ref := range snapshot.MaterialRefs {
		if err = ctx.Err(); err != nil {
			return failedCompletion("deadline_elapsed", usage)
		}
		material, err := s.config.Source.ReadMaterial(ctx, ref, "rule.input", work.Permission)
		if err != nil {
			return failedCompletion("snapshot_unavailable", usage)
		}
		if hash(material) != ref.Hash || strconv.Itoa(len(material)) != string(ref.ByteLength) {
			return failedCompletion("snapshot_unavailable", usage)
		}
		inputBytes += len(material)
		usage.InputBytes = v.Revision(strconv.Itoa(inputBytes))
		if !withinLimit(inputBytes, input.Limits.MaxInputBytes) {
			return failedCompletion("input_over_limit", usage)
		}
		if index == 0 {
			first = material
		}
	}
	if !withinLimit(1, input.Limits.MaxRuleSteps) {
		return failedCompletion("rule_limit_exceeded", usage)
	}
	// Fixture charge is one recorded rule step, with zero physical model calls.
	charge := new(big.Int)
	if _, ok := charge.SetString(string(input.Limits.MaxCost.IntegerValue), 10); !ok || charge.Sign() < 1 {
		return failedCompletion("budget_exhausted", usage)
	}
	usage.RuleSteps = "1"
	usage.Cost.IntegerValue = "1"
	deadline, err := fixedTime(input.Deadline)
	if err != nil || !time.Now().UTC().Before(deadline) {
		return failedCompletion("deadline_elapsed", usage)
	}
	if snapshot.Rule != "candidate_result" {
		return failedCompletion("proposal_invalid", usage)
	}
	artifactBytes := append([]byte("fixture result: "), first...)
	if !withinLimit(len(artifactBytes), input.Limits.MaxOutputBytes) {
		return failedCompletion("output_over_limit", usage)
	}
	key := string(work.Record.Ref.TenantID) + "/" + string(work.Record.Ref.OwnerID) + "/" + string(work.Record.Ref.ID) + "/" + work.Record.InputDigest
	artifact, err := s.config.Publisher.Publish(ctx, key+"/artifact", artifactBytes, snapshot.MaterialRefs, work.Permission)
	if err != nil {
		return failedCompletion("snapshot_unavailable", usage)
	}
	observed, err := s.config.Publisher.ReadPublished(ctx, artifact, work.Permission)
	if err != nil || !bytes.Equal(observed, artifactBytes) {
		return failedCompletion("snapshot_unavailable", usage)
	}
	evidence := make([]v.ProposalEvidence, 0, len(snapshot.RequirementRefs))
	for _, requirement := range snapshot.RequirementRefs {
		evidence = append(evidence, v.ProposalEvidence{RequirementRef: requirement, EvidenceRefs: []v.ContentRef{artifact}})
	}
	proposal := v.Proposal{DecisionRef: work.Record.Ref, SnapshotRef: input.SnapshotRef, GoalRevision: snapshot.GoalRevision, ControlRevision: snapshot.ControlRevision, ProcessedSourceRefs: snapshot.MaterialRefs, RequirementDelta: []v.RequirementDelta{}, Advance: v.NewProposalAdvanceCandidateResult(v.ProposalAdvanceCandidateResult{ArtifactRefs: []v.ContentRef{artifact}, Evidence: evidence, Limitations: []string{}})}
	proposalBytes, err := v.Encode(proposal)
	if err != nil {
		return failedCompletion("proposal_invalid", usage)
	}
	outputBytes := len(artifactBytes) + len(proposalBytes)
	usage.OutputBytes = v.Revision(strconv.Itoa(outputBytes))
	if !withinLimit(outputBytes, input.Limits.MaxOutputBytes) {
		return failedCompletion("output_over_limit", usage)
	}
	proposalRef, err := s.config.Publisher.Publish(ctx, key+"/proposal", proposalBytes, snapshot.MaterialRefs, work.Permission)
	if err != nil {
		return failedCompletion("snapshot_unavailable", usage)
	}
	observed, err = s.config.Publisher.ReadPublished(ctx, proposalRef, work.Permission)
	if err != nil || !bytes.Equal(observed, proposalBytes) {
		return failedCompletion("snapshot_unavailable", usage)
	}
	return completion{proposal: &proposal, proposalRef: &proposalRef, artifactRefs: []v.ContentRef{artifact}, usage: usage}
}
func (s *Service) finish(ctx context.Context, work Work, done completion) error {
	return s.config.Store.Within(ctx, oldOwner(decisionOwner(work.Record.Ref)), func(ctx context.Context, tx runtime.Tx) error {
		state, err := s.config.Store.LockPool(ctx, tx)
		if err != nil {
			return err
		}
		record, err := s.config.Store.LockDecision(ctx, tx, work.Record.Ref)
		if err != nil {
			return err
		}
		if record == nil || record.Status != "running" || record.StartedEpoch != work.Claim.Epoch || record.InputDigest != work.Record.InputDigest {
			return runtime.ErrClaim
		}
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = s.config.Store.ValidateClaim(ctx, tx, work.Claim, now); err != nil {
			return err
		}
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = s.config.Store.ValidateClaim(ctx, tx, work.Claim, now); err != nil {
			return err
		}
		if err = s.config.Store.ValidatePoolClaim(ctx, tx, state, work.Claim, now); err != nil {
			return err
		}
		if err = permissionCurrent(work.Permission, now); err != nil {
			return err
		}
		deadline, err := fixedTime(record.Input.Deadline)
		if err != nil {
			return err
		}
		if !now.Before(deadline) {
			done = failedCompletion("deadline_elapsed", done.usage)
		}
		record.Usage = done.usage
		record.Revision++
		if done.failure != nil {
			record.Status = "failed"
			record.Failure = done.failure
		} else {
			record.Status = "completed"
			record.Proposal = done.proposal
			record.ProposalRef = done.proposalRef
			record.ArtifactRefs = done.artifactRefs
		}
		public, err := record.Public()
		if err != nil {
			return err
		}
		if _, err = v.Encode(public); err != nil {
			return err
		}
		if err = s.config.Store.SaveDecision(ctx, tx, *record); err != nil {
			return err
		}
		return s.config.Store.Complete(ctx, tx, work.Claim, now)
	})
}
func (s *Service) RunClaim(ctx context.Context, claim runtime.Claim) error {
	work, err := s.Start(ctx, claim)
	if err != nil {
		return err
	}
	done := s.calculate(ctx, *work)
	return s.finish(ctx, *work, done)
}
func (s *Service) Step(ctx context.Context) (runtime.StepResult, error) {
	if err := finite(ctx); err != nil {
		return runtime.StepResult{}, err
	}
	if _, err := s.Maintain(ctx); err != nil {
		return runtime.StepResult{}, err
	}
	work, err := s.Claim(ctx)
	if err != nil {
		return runtime.StepResult{}, err
	}
	processed := 0
	if work != nil {
		if err = s.RunClaim(ctx, work.Claim); err != nil {
			return runtime.StepResult{}, err
		}
		processed = 1
	}
	var result runtime.StepResult
	err = s.config.Store.Within(ctx, oldOwner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		state, err := s.config.Store.LockPool(ctx, tx)
		if err != nil {
			return err
		}
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		next, err := s.config.Store.PoolNextWake(ctx, tx, state, "ordinary", now, now.Add(time.Second))
		if err != nil {
			return err
		}
		result = runtime.StepResult{Processed: processed, NextWake: next, WaitFor: next.Sub(now)}
		return nil
	})
	return result, err
}
func (s *Service) Run(ctx context.Context, timer runtime.Timer) error {
	if timer == nil {
		return runtime.ErrWorkBounds
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		step, err := s.Step(ctx)
		if err != nil {
			return err
		}
		if step.Processed == 0 {
			if err = timer.Wait(ctx, step.WaitFor); err != nil {
				return err
			}
		}
	}
}
