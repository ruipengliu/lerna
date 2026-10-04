package decision_engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

type Work struct {
	Compute    bool
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
			jobs, err := s.config.Store.MaintenanceCandidates(ctx, tx, now, 64)
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
				if !record.LegacyBillingRetired && now.Before(deadline) {
					continue
				}
				reason := v.DecisionFailure("deadline_elapsed")
				if record.LegacyBillingRetired {
					reason = "billing_basis_unsupported"
					if record.LegacyUnaccountedStart || record.Status != "accepted" {
						reason = "usage_unavailable"
					}
				}
				failRecord(record, reason)
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
	permission, err := s.observePermission(ctx, observation.Subject, ref, "start", observation.Input)
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
	permission, err := s.observePermission(ctx, observation.Subject, ref, "start", observation.Input)
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
		if record.OriginalPermission == nil || !reflect.DeepEqual(*record.OriginalPermission, permission) {
			return ErrForbidden
		}
		if err := record.preparedShape(); err != nil {
			return err
		}
		if !record.hasPrepared() && record.StartedEpoch == claim.Epoch {
			return runtime.ErrClaim
		}
		compute := !record.hasPrepared()
		if compute {
			reason := v.DecisionFailure("")
			starts, parseErr := strconv.ParseUint(string(record.Usage.RuleStarts), 10, 64)
			limit, limitErr := strconv.ParseUint(string(record.Input.Limits.MaxRuleSteps), 10, 64)
			if parseErr != nil || limitErr != nil {
				return ErrUnavailable
			}
			cost, ok := new(big.Int).SetString(string(record.Usage.Cost.IntegerValue), 10)
			if !ok {
				return ErrUnavailable
			}
			charge, ok := new(big.Int).SetString(string(permission.RuleStartCharge.IntegerValue), 10)
			if !ok || charge.Sign() < 1 {
				return ErrForbidden
			}
			ceiling, ok := new(big.Int).SetString(string(record.Input.Limits.MaxCost.IntegerValue), 10)
			if !ok {
				return ErrUnavailable
			}
			cost.Add(cost, charge)
			if permission.ChargeBasis != "durable_rule_start" || !supportedRuleVersion(permission.RuleVersion) {
				reason = "billing_basis_unsupported"
			} else if starts >= limit {
				reason = "rule_limit_exceeded"
			} else if permission.RuleStartCharge.Unit != record.Input.Limits.MaxCost.Unit || cost.Cmp(ceiling) > 0 {
				reason = "budget_exhausted"
			}
			if reason != "" {
				failRecord(record, reason)
				if err = s.config.Store.SaveDecision(ctx, tx, *record); err != nil {
					return err
				}
				job := runtime.Job{ID: claim.JobID, Object: claim.Object, Phase: claim.Phase, WorkRevision: claim.ClaimedRevision}
				if err = s.config.Store.StopRevision(ctx, tx, job, claim.ClaimedRevision); err != nil {
					return err
				}
				work = &Work{Claim: claim, Record: *record, Permission: permission}
				return nil
			}
			if record.MeasurementPending {
				record.MeasurementUnknown = true
			}
			record.StartSequence++
			record.Usage.RuleStarts = v.Revision(strconv.FormatUint(starts+1, 10))
			record.Usage.Cost.IntegerValue = v.Revision(cost.String())
			record.Usage.MeasurementsComplete = false
			record.MeasurementPending = true
		}
		record.Status = "running"
		record.StartedEpoch = claim.Epoch
		record.Revision++
		if err = s.config.Store.SaveDecision(ctx, tx, *record); err != nil {
			return err
		}
		work = &Work{Claim: claim, Record: *record, Permission: permission, Compute: compute}
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
func preparedDigest(p Prepared) (string, error) {
	p.Digest = ""
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return hash(append([]byte("lerna-decision-prepared-1\n"), data...)), nil
}
func (p Prepared) Validate(record Record) error {
	digest, err := preparedDigest(p)
	if err != nil || p.Digest != digest || p.InputDigest != record.InputDigest || p.StartSequence != record.StartSequence {
		return ErrUnavailable
	}
	if len(p.ArtifactBytes)+len(p.ProposalBytes) > v.MaxBodyBytes || hash(p.ArtifactBytes) != p.ArtifactRef.Hash || hash(p.ProposalBytes) != p.ProposalRef.Hash || strconv.Itoa(len(p.ArtifactBytes)) != string(p.ArtifactRef.ByteLength) || strconv.Itoa(len(p.ProposalBytes)) != string(p.ProposalRef.ByteLength) {
		return ErrUnavailable
	}
	encoded, err := v.Encode(p.Proposal)
	if err != nil || !bytes.Equal(encoded, p.ProposalBytes) || p.Proposal.DecisionRef != record.Ref || record.Input == nil || p.Proposal.SnapshotRef != record.Input.SnapshotRef || !reflect.DeepEqual(p.Sources, p.Proposal.ProcessedSourceRefs) || !withinLimit(len(p.ArtifactBytes)+len(p.ProposalBytes), record.Input.Limits.MaxOutputBytes) {
		return ErrUnavailable
	}
	return nil
}

type completion struct {
	prepared                           *Prepared
	preparedV2                         *PreparedV2
	inputBytes, outputBytes, ruleSteps int
	failure                            *v.DecisionFailure
	err                                error
}

func failedCompletion(reason v.DecisionFailure, inputBytes, outputBytes, ruleSteps int) completion {
	return completion{failure: &reason, inputBytes: inputBytes, outputBytes: outputBytes, ruleSteps: ruleSteps}
}
func sourceFailure(err error) v.DecisionFailure {
	if errors.Is(err, ErrInputLimit) {
		return "input_over_limit"
	}
	return "snapshot_unavailable"
}
func (s *Service) ioContext(ctx context.Context, work Work) (context.Context, context.CancelFunc, error) {
	deadline, err := fixedTime(work.Record.Input.Deadline)
	if err != nil {
		return nil, nil, err
	}
	if work.Permission.ValidUntil.Before(deadline) {
		deadline = work.Permission.ValidUntil
	}
	if work.Claim.LeaseUntil.Before(deadline) {
		deadline = work.Claim.LeaseUntil
	}
	bounded, cancel := context.WithDeadline(ctx, deadline)
	if err = bounded.Err(); err != nil {
		cancel()
		return nil, nil, err
	}
	return bounded, cancel, nil
}

// calculate returns confirmed local observations; it cannot publish bytes.
func (s *Service) calculate(ctx context.Context, work Work) completion {
	bounded, cancel, err := s.ioContext(ctx, work)
	if err != nil {
		return failedCompletion("deadline_elapsed", 0, 0, 0)
	}
	defer cancel()
	ctx = bounded
	input := work.Record.Input
	cap, err := strconv.ParseInt(string(input.Limits.MaxInputBytes), 10, 64)
	if err != nil {
		return failedCompletion("input_over_limit", 0, 0, 0)
	}
	snapshot, err := s.config.Source.ReadSnapshot(ctx, input.SnapshotRef, work.Permission, cap)
	inputBytes := len(snapshot.Raw)
	if err != nil {
		return failedCompletion(sourceFailure(err), inputBytes, 0, 0)
	}
	if snapshot.Ref != input.SnapshotRef || snapshot.ComponentRef != input.ComponentRef || snapshot.TaskRef.ID != input.TaskRef.ID || snapshot.TaskRef.OwnerID != input.TaskRef.OwnerID || snapshot.TaskRef.TenantID != input.TaskRef.TenantID || len(snapshot.MaterialRefs) < 1 || len(snapshot.MaterialRefs) > 63 || len(snapshot.RequirementRefs) < 1 || len(snapshot.RequirementRefs) > 64 {
		return failedCompletion("snapshot_unavailable", inputBytes, 0, 0)
	}
	lock, err := s.config.Source.ReadFixtureLock(ctx, input.ComponentRef.InstallLockRef, work.Permission, cap-int64(inputBytes))
	inputBytes += len(lock.Raw) + len(lock.ManifestRaw)
	if err != nil {
		return failedCompletion(sourceFailure(err), inputBytes, 0, 0)
	}
	if lock.ComponentRef != input.ComponentRef || lock.RuleVersion != work.Permission.RuleVersion || lock.ChargeBasis != work.Permission.ChargeBasis || lock.RuleStartCharge != work.Permission.RuleStartCharge {
		return failedCompletion("snapshot_unavailable", inputBytes, 0, 0)
	}
	var manifest struct {
		Kind            string   `json:"kind"`
		Snapshot        Snapshot `json:"snapshot"`
		RuleVersion     string   `json:"rule_version"`
		ChargeBasis     string   `json:"charge_basis,omitempty"`
		RuleStartCharge v.Amount `json:"rule_start_charge,omitzero"`
	}
	if err = json.Unmarshal(lock.ManifestRaw, &manifest); err != nil {
		return failedCompletion("snapshot_unavailable", inputBytes, 0, 0)
	}
	observedSnapshot := snapshot
	observedSnapshot.Raw = nil
	if !reflect.DeepEqual(observedSnapshot, manifest.Snapshot) {
		return failedCompletion("snapshot_unavailable", inputBytes, 0, 0)
	}
	materialRefs := snapshot.MaterialRefs
	if work.Permission.RuleVersion == "fixture-rule/3" {
		materialRefs = proposalMaterialRefs(snapshot)
		if len(materialRefs) > 63 {
			return failedCompletion("proposal_invalid", inputBytes, 0, 0)
		}
	}
	processed := append([]v.ContentRef{lock.ManifestRef}, materialRefs...)
	var first []byte
	for index, ref := range materialRefs {
		expected, err := strconv.ParseInt(string(ref.ByteLength), 10, 64)
		if err != nil || expected > cap-int64(inputBytes) {
			return failedCompletion("input_over_limit", inputBytes, 0, 0)
		}
		material, err := s.config.Source.ReadMaterial(ctx, ref, "rule.input", work.Permission, cap-int64(inputBytes))
		inputBytes += len(material)
		if err != nil {
			return failedCompletion(sourceFailure(err), inputBytes, 0, 0)
		}
		if hash(material) != ref.Hash || strconv.Itoa(len(material)) != string(ref.ByteLength) {
			return failedCompletion("snapshot_unavailable", inputBytes, 0, 0)
		}
		if index == 0 {
			first = material
		}
	}
	if ctx.Err() != nil {
		return failedCompletion("deadline_elapsed", inputBytes, 0, 0)
	}
	if work.Permission.RuleVersion == "fixture-rule/3" {
		return s.calculateProposalV3(ctx, work, snapshot, processed, first, inputBytes)
	}
	if snapshot.Rule != "candidate_result" {
		return failedCompletion("proposal_invalid", inputBytes, 0, 1)
	}
	artifactBytes := append([]byte("fixture result: "), first...)
	key := string(work.Record.Ref.TenantID) + "/" + string(work.Record.Ref.OwnerID) + "/" + string(work.Record.Ref.ID) + "/" + work.Record.InputDigest
	artifact, err := s.config.Publisher.PlanPublication(ctx, key+"/artifact", artifactBytes, processed, work.Permission)
	if err != nil {
		return completion{inputBytes: inputBytes, ruleSteps: 1, err: err}
	}
	evidence := make([]v.ProposalEvidence, 0, len(snapshot.RequirementRefs))
	for _, requirement := range snapshot.RequirementRefs {
		evidence = append(evidence, v.ProposalEvidence{RequirementRef: requirement, EvidenceRefs: []v.ContentRef{artifact}})
	}
	proposal := v.Proposal{DecisionRef: work.Record.Ref, SnapshotRef: input.SnapshotRef, GoalRevision: snapshot.GoalRevision, ControlRevision: snapshot.ControlRevision, ProcessedSourceRefs: processed, RequirementDelta: []v.RequirementDelta{}, Advance: v.NewProposalAdvanceCandidateResult(v.ProposalAdvanceCandidateResult{ArtifactRefs: []v.ContentRef{artifact}, Evidence: evidence, Limitations: []string{}})}
	proposalBytes, err := v.Encode(proposal)
	if err != nil {
		return failedCompletion("proposal_invalid", inputBytes, len(artifactBytes), 1)
	}
	outputBytes := len(artifactBytes) + len(proposalBytes)
	if !withinLimit(outputBytes, input.Limits.MaxOutputBytes) {
		return failedCompletion("output_over_limit", inputBytes, outputBytes, 1)
	}
	proposalRef, err := s.config.Publisher.PlanPublication(ctx, key+"/proposal", proposalBytes, processed, work.Permission)
	if err != nil {
		return completion{inputBytes: inputBytes, outputBytes: outputBytes, ruleSteps: 1, err: err}
	}
	prepared := Prepared{StartSequence: work.Record.StartSequence, InputDigest: work.Record.InputDigest, ArtifactKey: key + "/artifact", ArtifactRef: artifact, ArtifactBytes: artifactBytes, ProposalKey: key + "/proposal", ProposalRef: proposalRef, Proposal: proposal, ProposalBytes: proposalBytes, Sources: processed}
	prepared.Digest, err = preparedDigest(prepared)
	if err != nil {
		return failedCompletion("proposal_invalid", inputBytes, outputBytes, 1)
	}
	preview := work.Record
	preview.Status = "completed"
	preview.Proposal = &proposal
	preview.ProposalRef = &proposalRef
	preview.ArtifactRefs = []v.ContentRef{artifact}
	public, err := preview.Public()
	if err != nil {
		return failedCompletion("proposal_invalid", inputBytes, outputBytes, 1)
	}
	if _, err = v.Encode(public); err != nil {
		return failedCompletion("output_over_limit", inputBytes, outputBytes, 1)
	}
	return completion{prepared: &prepared, inputBytes: inputBytes, outputBytes: outputBytes, ruleSteps: 1}
}
func addObservation(current v.Revision, delta int) (v.Revision, error) {
	value, ok := new(big.Int).SetString(string(current), 10)
	if !ok || delta < 0 {
		return "", ErrUnavailable
	}
	value.Add(value, big.NewInt(int64(delta)))
	return v.Revision(value.String()), nil
}
func confirmObservation(record *Record, done completion) error {
	var err error
	if record.Usage.InputBytes, err = addObservation(record.Usage.InputBytes, done.inputBytes); err != nil {
		return err
	}
	if record.Usage.OutputBytes, err = addObservation(record.Usage.OutputBytes, done.outputBytes); err != nil {
		return err
	}
	if record.Usage.RuleSteps, err = addObservation(record.Usage.RuleSteps, done.ruleSteps); err != nil {
		return err
	}
	record.MeasurementPending = false
	record.Usage.MeasurementsComplete = !record.MeasurementUnknown
	return nil
}
func (s *Service) lockedWork(ctx context.Context, tx runtime.Tx, work Work) (*Record, time.Time, error) {
	state, err := s.config.Store.LockPool(ctx, tx)
	if err != nil {
		return nil, time.Time{}, err
	}
	record, err := s.config.Store.LockDecision(ctx, tx, work.Record.Ref)
	if err != nil {
		return nil, time.Time{}, err
	}
	if record == nil || record.Status != "running" || record.StartedEpoch != work.Claim.Epoch || record.InputDigest != work.Record.InputDigest || record.StartSequence != work.Record.StartSequence {
		return nil, time.Time{}, runtime.ErrClaim
	}
	now, err := s.config.Store.Now(ctx, tx)
	if err != nil {
		return nil, time.Time{}, err
	}
	if err = s.config.Store.ValidateClaim(ctx, tx, work.Claim, now); err != nil {
		return nil, time.Time{}, err
	}
	now, err = s.config.Store.Now(ctx, tx)
	if err != nil {
		return nil, time.Time{}, err
	}
	if err = s.config.Store.ValidateClaim(ctx, tx, work.Claim, now); err != nil {
		return nil, time.Time{}, err
	}
	if err = s.config.Store.ValidatePoolClaim(ctx, tx, state, work.Claim, now); err != nil {
		return nil, time.Time{}, err
	}
	if err = permissionCurrent(work.Permission, now); err != nil {
		return nil, time.Time{}, err
	}
	deadline, err := fixedTime(record.Input.Deadline)
	if err != nil || !now.Before(deadline) {
		return nil, time.Time{}, runtime.ErrClaim
	}
	return record, now, nil
}
func (s *Service) savePrepared(ctx context.Context, work Work, done completion) (Work, error) {
	preview := work.Record
	preview.Prepared, preview.PreparedV2 = done.prepared, done.preparedV2
	proposed, err := preview.preparedOutput()
	if err != nil {
		return work, err
	}
	err = s.config.Store.Within(ctx, oldOwner(decisionOwner(work.Record.Ref)), func(ctx context.Context, tx runtime.Tx) error {
		record, _, err := s.lockedWork(ctx, tx, work)
		if err != nil {
			return err
		}
		if record.hasPrepared() {
			existing, err := record.preparedOutput()
			if err != nil {
				return err
			}
			if existing.digest != proposed.digest {
				return ErrPublicationConflict
			}
			work.Record = *record
			return nil
		}
		if !record.MeasurementPending {
			return runtime.ErrClaim
		}
		record.Prepared, record.PreparedV2 = done.prepared, done.preparedV2
		if _, err = record.preparedOutput(); err != nil {
			return err
		}
		if err = confirmObservation(record, done); err != nil {
			return err
		}
		record.Revision++
		if err = s.config.Store.SaveDecision(ctx, tx, *record); err != nil {
			return err
		}
		work.Record = *record
		return nil
	})
	if errors.Is(err, runtime.ErrCommitUnknown) {
		// Only an independently confirmed identical handoff may proceed to publish.
		err = s.config.Store.Within(ctx, oldOwner(decisionOwner(work.Record.Ref)), func(ctx context.Context, tx runtime.Tx) error {
			record, e := s.config.Store.ReadDecision(ctx, tx, work.Record.Ref)
			if e != nil {
				return e
			}
			if record == nil || record.StartedEpoch != work.Claim.Epoch || terminal(record.Status) {
				return runtime.ErrCommitUnknown
			}
			actual, e := record.preparedOutput()
			if e != nil || actual.digest != proposed.digest {
				return runtime.ErrCommitUnknown
			}
			work.Record = *record
			return nil
		})
	}
	return work, err
}
func (s *Service) publishPrepared(ctx context.Context, work Work) error {
	prepared, err := work.Record.preparedOutput()
	if err != nil {
		return err
	}
	bounded, cancel, err := s.ioContext(ctx, work)
	if err != nil {
		return err
	}
	defer cancel()
	for _, publication := range prepared.publications {
		if err = bounded.Err(); err != nil {
			return err
		}
		actual, err := s.config.Publisher.Publish(bounded, publication.key, publication.body, prepared.sources, work.Permission)
		if err != nil {
			return err
		}
		if actual != publication.ref {
			return ErrPublicationConflict
		}
		if err = bounded.Err(); err != nil {
			return err
		}
		bytesRead, err := s.config.Publisher.ReadPublished(bounded, actual, work.Permission)
		if err != nil {
			return err
		}
		if !bytes.Equal(bytesRead, publication.body) {
			return ErrPublicationConflict
		}
	}
	return nil
}
func (s *Service) finish(ctx context.Context, work Work, done completion) error {
	return s.config.Store.Within(ctx, oldOwner(decisionOwner(work.Record.Ref)), func(ctx context.Context, tx runtime.Tx) error {
		record, now, err := s.lockedWork(ctx, tx, work)
		if err != nil {
			return err
		}
		if !record.hasPrepared() {
			if err = confirmObservation(record, done); err != nil {
				return err
			}
		}
		record.Revision++
		if done.failure != nil {
			record.Status = "failed"
			record.Failure = done.failure
		} else {
			prepared, err := record.preparedOutput()
			if err != nil {
				return err
			}
			prior, err := work.Record.preparedOutput()
			if err != nil || prepared.digest != prior.digest {
				return runtime.ErrClaim
			}
			record.Status = "completed"
			record.Proposal = &prepared.proposal
			record.ProposalRef = &prepared.proposalRef
			record.ArtifactRefs = prepared.artifactRefs
		}
		if err = s.config.Store.SaveDecision(ctx, tx, *record); err != nil {
			return err
		}
		return s.config.Store.Complete(ctx, tx, work.Claim, now)
	})
}
func (s *Service) deferPrepared(ctx context.Context, work Work) error {
	return s.config.Store.Within(ctx, oldOwner(decisionOwner(work.Record.Ref)), func(ctx context.Context, tx runtime.Tx) error {
		record, now, err := s.lockedWork(ctx, tx, work)
		if err != nil {
			return err
		}
		record.Revision++
		record.PublicationAttempts++
		if record.PublicationAttempts >= 8 {
			reason := v.DecisionFailure("snapshot_unavailable")
			record.Status = "failed"
			record.Failure = &reason
			if err = s.config.Store.SaveDecision(ctx, tx, *record); err != nil {
				return err
			}
			return s.config.Store.Complete(ctx, tx, work.Claim, now)
		}
		due := now.Add(100 * time.Millisecond)
		record.Status = "waiting"
		record.Reason = "dependency_unavailable"
		record.WakeAt = v.Time(due.UTC().Format("2006-01-02T15:04:05.000000Z"))
		if err = s.config.Store.SaveDecision(ctx, tx, *record); err != nil {
			return err
		}
		return s.config.Store.DeferClaim(ctx, tx, work.Claim, now, due)
	})
}
func (s *Service) RunClaim(ctx context.Context, claim runtime.Claim) error {
	work, err := s.Start(ctx, claim)
	if err != nil {
		return err
	}
	if terminal(work.Record.Status) {
		return nil
	}
	if work.Compute {
		done := s.calculate(ctx, *work)
		if done.err != nil {
			reason := v.DecisionFailure("snapshot_unavailable")
			done.failure = &reason
			return s.finish(ctx, *work, done)
		}
		if done.failure != nil {
			return s.finish(ctx, *work, done)
		}
		updated, err := s.savePrepared(ctx, *work, done)
		if err != nil {
			return err
		}
		work = &updated
	}
	if err = s.publishPrepared(ctx, *work); err != nil {
		if errors.Is(err, ErrPublicationConflict) {
			reason := v.DecisionFailure("proposal_invalid")
			return s.finish(ctx, *work, completion{failure: &reason})
		}
		return s.deferPrepared(ctx, *work)
	}
	return s.finish(ctx, *work, completion{})
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
