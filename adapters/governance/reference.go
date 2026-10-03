package governance

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	domain "github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

const ReferenceClass = "reference-rule-file"
const ReferenceReportV1 = "reference-report-v1"

// ReferenceReportBodyV0 是真实旧模板：只保存 body，独立 judge 会发现缺失标题。
const ReferenceReportBodyV0 = "reference-report-body-v0"

type ReferenceImplementation struct {
	Ref      api.ComponentRef `json:"ref"`
	Strategy string           `json:"strategy"`
}
type ReferenceRunnerConfig struct {
	Root            string
	Scope           runtime.Scope
	Content         Content
	Clock           func() time.Time
	Keys            *platform.Keyring
	Implementations []ReferenceImplementation
	// AfterTargetWrite 是宿主故障注入边界；错误保留原未知责任，禁止重试写入。
	AfterTargetWrite func(context.Context, domain.RunnerAttempt) error
}
type ReferenceRunner struct {
	w          *workspace
	keys       *platform.Keyring
	registry   *ReferenceRegistry
	afterWrite func(context.Context, domain.RunnerAttempt) error
	mu         sync.Mutex
	active     map[string]*activeRun
	closing    bool
}
type activeRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type pairJournal struct {
	Pair             domain.RunnerPair        `json:"pair"`
	State            string                   `json:"state"`
	Evidence         domain.PairEvidence      `json:"evidence"`
	CandidateAttempt string                   `json:"candidate_attempt,omitempty"`
	BaselineAttempt  string                   `json:"baseline_attempt,omitempty"`
	Stop             *domain.PairStopEvidence `json:"stop,omitempty"`
}
type attemptJournal struct {
	Request      domain.RunnerAttempt       `json:"request"`
	State        string                     `json:"state"`
	PID          int                        `json:"pid"`
	ProcessStart string                     `json:"process_start"`
	Observation  *domain.AttemptObservation `json:"observation,omitempty"`
}

func NewReferenceRunner(c ReferenceRunnerConfig) (*ReferenceRunner, error) {
	if c.Keys == nil || len(c.Keys.Keys) == 0 || len(c.Implementations) == 0 || len(c.Implementations) > 128 {
		return nil, api.E("unsupported", "registered_reference_code_and_entry_keys_required")
	}
	registry, e := NewReferenceRegistry(c.Implementations)
	if e != nil {
		return nil, e
	}
	// 密钥由受信配置登记；入口从不接受消息携带的公钥、URL 或身份。
	keys := &platform.Keyring{Keys: map[string]platform.RegisteredKey{}}
	for kid, key := range c.Keys.Keys {
		key.Purposes = append([]string{}, key.Purposes...)
		key.Private = nil
		keys.Keys[kid] = key
	}
	w, e := openWorkspace(c.Root, c.Scope, c.Content, c.Clock)
	if e != nil {
		return nil, e
	}
	return &ReferenceRunner{w: w, keys: keys, registry: registry, afterWrite: c.AfterTargetWrite, active: map[string]*activeRun{}}, nil
}
func pairPath(runID, sampleID string) string                { return "pairs/" + directory([]string{runID, sampleID}) }
func attemptPath(attemptID string) string                   { return "attempts/" + directory(attemptID) + ".json" }
func pairIdentity(pair domain.RunnerPair) domain.RunnerPair { pair.Permit = ""; return pair }
func (r *ReferenceRunner) implementation(ref api.ComponentRef) (ReferenceImplementation, error) {
	return r.registry.implementation(ref)
}
func (r *ReferenceRunner) CheckEvaluationPlan(plan domain.EvaluationPlan) error {
	return r.registry.CheckEvaluationPlan(plan)
}
func (r *ReferenceRunner) checkPair(pair domain.RunnerPair) error {
	if !api.ValidID(pair.RunID) || !api.ValidID(pair.Plan.PlanID) || !api.ValidID(pair.Sample.SampleID) || !pair.Plan.Frozen || pair.Sample.Class != ReferenceClass || pair.Plan.FullDenominator > 10000 || pair.StartBefore != pair.Plan.ObservationCutoff {
		return api.E("unsupported", "only_frozen_reference_rule_file_samples_supported")
	}
	if pair.CandidateEnvironmentKey != pair.RunID+"/"+pair.Sample.SampleID+"/candidate" || pair.BaselineEnvironmentKey != pair.RunID+"/"+pair.Sample.SampleID+"/baseline" {
		return api.E("forbidden", "original_environment_key_changed")
	}
	if e := r.CheckEvaluationPlan(pair.Plan); e != nil {
		return e
	}
	if pair.Sample.InputRef.TenantID != r.w.scope.TenantID || pair.Sample.TruthRef.TenantID != r.w.scope.TenantID {
		return api.E("forbidden", "reference_sample_scope_changed")
	}
	return nil
}
func (r *ReferenceRunner) verifyPair(pair domain.RunnerPair) error {
	digest, e := api.Digest(pairIdentity(pair))
	if e != nil {
		return e
	}
	var last error
	// 调度器可能从任一臂进入，但必须绑定这对原样本的一个准确 SampleRun。
	for _, arm := range []string{"candidate", "baseline"} {
		ref := domain.EvaluationSampleRunRef(r.w.scope, pair.Plan.PlanID, pair.Sample.SampleID, arm)
		_, e = r.keys.Verify(pair.Permit, platform.ProofClaims{TenantID: r.w.scope.TenantID, Issuer: r.w.scope.OwnerID, Audience: r.w.scope.OwnerID, Purpose: "evaluation_prepare", ObjectRef: ref, WindowID: ref.ObjectID, Digest: digest, StartBefore: pair.StartBefore}, r.w.clock())
		if e == nil {
			return nil
		}
		last = e
	}
	return last
}
func (r *ReferenceRunner) verifyAttempt(in domain.RunnerAttempt, start bool) error {
	if !api.ValidID(in.AttemptID) || !api.ValidID(in.RunID) || !api.ValidID(in.PlanID) || !api.ValidID(in.SampleID) || (in.Arm != "candidate" && in.Arm != "baseline") || in.EnvironmentKey != in.RunID+"/"+in.SampleID+"/"+in.Arm {
		return api.E("forbidden", "exact_original_reference_attempt_required")
	}
	original := in
	original.Permit = ""
	digest, e := api.Digest(original)
	if e != nil {
		return e
	}
	expected := platform.ProofClaims{TenantID: r.w.scope.TenantID, Issuer: r.w.scope.OwnerID, Audience: r.w.scope.OwnerID, Purpose: "evaluation_start", ObjectRef: r.w.scope.Ref(in.AttemptID, 1), WindowID: in.AttemptID, Digest: digest, StartBefore: in.StartBefore}
	if start {
		_, e = r.keys.Verify(in.Permit, expected, r.w.clock())
	} else {
		_, e = r.keys.VerifySource(in.Permit, expected)
	}
	return e
}
func (r *ReferenceRunner) closed(path string) error {
	_, e := r.w.root.Lstat(path + "/closed.json")
	if e == nil {
		return api.E("invalid_state", "reference_environment_sealed")
	}
	if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return nil
}
func reportGoal(b []byte) (brain.GoalSpec, error) {
	validator, e := api.NewValidator(brain.GoalSchema())
	if e != nil {
		return brain.GoalSpec{}, e
	}
	if e = validator.Validate(b); e != nil {
		return brain.GoalSpec{}, api.E("unsupported", "reference_runner_requires_exact_report_template")
	}
	var goal brain.GoalSpec
	if e = api.Decode(b, &goal); e != nil {
		return goal, e
	}
	if goal.Kind != "report" || len(goal.SavePath) > 1024 || !filepath.IsLocal(goal.SavePath) || filepath.Clean(goal.SavePath) == "." {
		return goal, api.E("unsupported", "reference_report_requires_relative_private_target_path")
	}
	return goal, nil
}

func (r *ReferenceRunner) PreparePair(ctx context.Context, pair domain.RunnerPair) (out domain.PairEvidence, err error) {
	if e := r.checkPair(pair); e != nil {
		return out, e
	}
	if e := r.verifyPair(pair); e != nil {
		return out, e
	}
	err = r.w.lock(ctx, func() error {
		path := pairPath(pair.RunID, pair.Sample.SampleID)
		if e := r.closed(path); e != nil {
			return e
		}
		var j pairJournal
		e := r.w.readJSON(path+"/journal.json", &j)
		if e == nil {
			if !api.Equal(j.Pair, pairIdentity(pair)) {
				return api.E("forbidden", "frozen_pair_binding_changed")
			}
			if j.State == "prepared" {
				for _, source := range []api.ContentRef{pair.Sample.InputRef, pair.Sample.TruthRef} {
					if _, e := r.w.exact(ctx, source, "evaluation.runner.prepared_current"); e != nil {
						return e
					}
				}
				if e := r.preparedFiles(path, pair); e != nil {
					return e
				}
				out = j.Evidence
				return nil
			}
			if j.State != "preparing" {
				return api.E("invalid_state", "reference_pair_closed")
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		input, e := r.w.exact(ctx, pair.Sample.InputRef, "evaluation.runner.input")
		if e != nil {
			return e
		}
		if _, e = reportGoal(input); e != nil {
			return e
		}
		truth, e := r.w.exact(ctx, pair.Sample.TruthRef, "evaluation.runner.truth")
		if e != nil {
			return e
		}
		j = pairJournal{Pair: pairIdentity(pair), State: "preparing"}
		if e = r.w.writeJSON(path+"/journal.json", j); e != nil {
			return e
		}
		// 真值只在 supervisor/judge 路径；编译好的两臂生成函数只取得输入。
		if e = r.w.write(path+"/judge/truth", truth); e != nil {
			return e
		}
		for _, arm := range []string{"candidate", "baseline"} {
			env := path + "/environments/" + arm
			if e = r.w.write(env+"/input.json", input); e != nil {
				return e
			}
			actual, e := r.w.read(env + "/input.json")
			if e != nil {
				return e
			}
			if !bytes.Equal(actual, input) {
				return api.E("dependency_unavailable", "reference_environment_input_probe_failed")
			}
			if e = r.w.mkdir(env + "/target"); e != nil {
				return e
			}
		}
		if e = r.closed(path); e != nil {
			return e
		}
		proof, e := r.w.proof(ctx, "reference_pair_prepared", struct {
			Class        string `json:"class"`
			RunID        string `json:"run_id"`
			PlanID       string `json:"plan_id"`
			SampleID     string `json:"sample_id"`
			CandidateKey string `json:"candidate_key"`
			BaselineKey  string `json:"baseline_key"`
			InputHash    string `json:"input_hash"`
			TruthHash    string `json:"truth_hash"`
			ObservedAt   string `json:"observed_at"`
		}{ReferenceClass, pair.RunID, pair.Plan.PlanID, pair.Sample.SampleID, pair.CandidateEnvironmentKey, pair.BaselineEnvironmentKey, api.Hash(input), api.Hash(truth), api.Time(r.w.clock())}, []api.ContentRef{pair.Sample.InputRef, pair.Sample.TruthRef})
		if e != nil {
			return e
		}
		out = domain.PairEvidence{CandidatePrepared: true, BaselinePrepared: true, CandidateEnvironmentRef: r.w.scope.Ref(id("environment", pair.CandidateEnvironmentKey), 1), BaselineEnvironmentRef: r.w.scope.Ref(id("environment", pair.BaselineEnvironmentKey), 1), ProofRef: proof}
		j.State = "prepared"
		j.Evidence = out
		return r.w.writeJSON(path+"/journal.json", j)
	})
	return out, err
}

func (r *ReferenceRunner) preparedFiles(path string, pair domain.RunnerPair) error {
	var previous os.FileInfo
	for _, arm := range []string{"candidate", "baseline"} {
		env := path + "/environments/" + arm
		b, e := r.w.read(env + "/input.json")
		if e != nil {
			return e
		}
		if api.Hash(b) != pair.Sample.InputRef.Hash || uint64(len(b)) != pair.Sample.InputRef.ByteLength {
			return api.E("forbidden", "prepared_reference_input_changed")
		}
		info, e := r.w.root.Lstat(env)
		if e != nil {
			return e
		}
		if !info.IsDir() || (previous != nil && os.SameFile(previous, info)) {
			return api.E("forbidden", "reference_arms_not_independent_directories")
		}
		previous = info
		info, e = r.w.root.Lstat(env + "/target")
		if e != nil {
			return e
		}
		if !info.IsDir() {
			return api.E("invalid_state", "reference_target_not_directory")
		}
	}
	truth, e := r.w.read(path + "/judge/truth")
	if e != nil {
		return e
	}
	if api.Hash(truth) != pair.Sample.TruthRef.Hash || uint64(len(truth)) != pair.Sample.TruthRef.ByteLength {
		return api.E("forbidden", "prepared_reference_truth_changed")
	}
	return nil
}

func (r *ReferenceRunner) binding(in domain.RunnerAttempt, pair domain.RunnerPair) error {
	implementation := pair.Plan.CandidateRef
	if in.Arm == "baseline" {
		implementation = pair.Plan.BaselineRef
	}
	if in.PlanID != pair.Plan.PlanID || in.RunID != pair.RunID || in.SampleID != pair.Sample.SampleID || !api.Equal(in.ImplementationRef, implementation) || !api.Equal(in.InputRef, pair.Sample.InputRef) || !api.Equal(in.TruthRef, pair.Sample.TruthRef) || in.Seed != pair.Plan.Seed || in.StartBefore != pair.StartBefore || !api.Equal(in.Budget, pair.Plan.Budget) {
		return api.E("forbidden", "attempt_not_bound_to_frozen_pair")
	}
	return nil
}
func (r *ReferenceRunner) Lookup(ctx context.Context, in domain.RunnerAttempt) (out domain.AttemptObservation, known bool, err error) {
	if e := r.verifyAttempt(in, false); e != nil {
		return out, false, e
	}
	err = r.w.lock(ctx, func() error {
		var j attemptJournal
		e := r.w.readJSON(attemptPath(in.AttemptID), &j)
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		if e != nil {
			return e
		}
		if !api.Equal(j.Request, in) {
			return api.E("forbidden", "original_attempt_changed")
		}
		if j.Observation != nil {
			out = *j.Observation
			known = true
		}
		return nil
	})
	return out, known, err
}

func (r *ReferenceRunner) Run(ctx context.Context, in domain.RunnerAttempt) (out domain.AttemptObservation, err error) {
	if e := r.verifyAttempt(in, false); e != nil {
		return out, e
	}
	err = r.w.lock(ctx, func() error {
		var original attemptJournal
		e := r.w.readJSON(attemptPath(in.AttemptID), &original)
		if e == nil {
			if !api.Equal(original.Request, in) {
				return api.E("forbidden", "original_attempt_changed")
			}
			if original.Observation != nil {
				out = *original.Observation
				return nil
			}
			return api.E("effect_unknown", "original_reference_attempt_must_not_be_restarted")
		}
		if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if e = r.verifyAttempt(in, true); e != nil {
			return e
		}
		path := pairPath(in.RunID, in.SampleID)
		if e = r.closed(path); e != nil {
			return e
		}
		var pair pairJournal
		if e = r.w.readJSON(path+"/journal.json", &pair); e != nil {
			return e
		}
		if pair.State != "prepared" {
			return api.E("invalid_state", "both_reference_arms_not_prepared")
		}
		if e = r.binding(in, pair.Pair); e != nil {
			return e
		}
		deadline, e := api.ParseTime(in.StartBefore)
		if e != nil {
			return e
		}
		running, cancel := context.WithTimeout(ctx, deadline.Sub(r.w.clock()))
		defer cancel()
		active := &activeRun{cancel: cancel, done: make(chan struct{})}
		r.mu.Lock()
		if r.closing {
			r.mu.Unlock()
			return api.E("invalid_state", "reference_host_closing")
		}
		r.active[path] = active
		r.mu.Unlock()
		defer func() { r.mu.Lock(); delete(r.active, path); r.mu.Unlock(); close(active.done) }()
		ctx = running
		claimed := pair.CandidateAttempt
		if in.Arm == "baseline" {
			claimed = pair.BaselineAttempt
		}
		if claimed != "" && claimed != in.AttemptID {
			return api.E("forbidden", "reference_arm_already_has_original_attempt")
		}
		implementation, e := r.implementation(in.ImplementationRef)
		if e != nil {
			return e
		}
		// 已准备副本不能替代实际入口对当前数据用途的核验。
		for _, source := range []api.ContentRef{in.InputRef, in.TruthRef} {
			if _, e = r.w.exact(ctx, source, "evaluation.runner.start_current"); e != nil {
				return e
			}
		}
		env := path + "/environments/" + in.Arm
		input, e := r.w.read(env + "/input.json")
		if e != nil {
			return e
		}
		if api.Hash(input) != in.InputRef.Hash {
			return api.E("forbidden", "prepared_input_changed")
		}
		goal, e := reportGoal(input)
		if e != nil {
			return e
		}
		if in.Arm == "candidate" {
			pair.CandidateAttempt = in.AttemptID
		} else {
			pair.BaselineAttempt = in.AttemptID
		}
		if e = r.w.writeJSON(path+"/journal.json", pair); e != nil {
			return e
		}
		process, e := processStart(os.Getpid())
		if e != nil {
			return e
		}
		original = attemptJournal{Request: in, State: "started", PID: os.Getpid(), ProcessStart: process}
		if e = r.w.writeJSON(attemptPath(in.AttemptID), original); e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = r.closed(path); e != nil {
			return e
		}
		if e = r.verifyAttempt(in, true); e != nil {
			return e
		}
		started := time.Now()
		// 编译的 candidate/baseline 函数只接触 GoalSpec，不持有 judge 或根目录。
		output := brain.ReportBytes(goal)
		if implementation.Strategy == ReferenceReportBodyV0 {
			output = []byte(goal.Body)
		}
		target := env + "/target/" + filepath.Clean(goal.SavePath)
		if e = r.w.write(target, output); e != nil {
			return e
		}
		if r.afterWrite != nil {
			if e = r.afterWrite(ctx, in); e != nil {
				return e
			}
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		actual, e := r.w.read(target)
		if e != nil {
			return e
		}
		truth, e := r.w.read(path + "/judge/truth")
		if e != nil {
			return e
		}
		if api.Hash(truth) != in.TruthRef.Hash || uint64(len(truth)) != in.TruthRef.ByteLength {
			return api.E("forbidden", "independent_truth_changed")
		}
		outcome := "fail"
		accepted := false
		if bytes.Equal(actual, truth) {
			outcome = "pass"
			accepted = true
		}
		observed := api.Time(r.w.clock())
		latency := uint64(time.Since(started) / time.Millisecond)
		operation := r.w.scope.Ref(id("operation", []string{in.RunID, in.AttemptID}), 1)
		proof, e := r.w.proof(ctx, "reference_independent_readback", struct {
			Class          string           `json:"class"`
			RunID          string           `json:"run_id"`
			AttemptID      string           `json:"attempt_id"`
			Arm            string           `json:"arm"`
			Implementation api.ComponentRef `json:"implementation_ref"`
			Target         string           `json:"target"`
			InputHash      string           `json:"input_hash"`
			TruthHash      string           `json:"truth_hash"`
			ActualHash     string           `json:"actual_hash"`
			ActualLength   uint64           `json:"actual_length"`
			Outcome        string           `json:"outcome"`
			ObservedAt     string           `json:"observed_at"`
			Latency        uint64           `json:"latency_millis"`
			Operation      api.ObjectRef    `json:"operation_ref"`
		}{ReferenceClass, in.RunID, in.AttemptID, in.Arm, in.ImplementationRef, goal.SavePath, in.InputRef.Hash, api.Hash(truth), api.Hash(actual), uint64(len(actual)), outcome, observed, latency, operation}, []api.ContentRef{in.InputRef, in.TruthRef})
		if e != nil {
			return e
		}
		out = domain.AttemptObservation{Outcome: outcome, TruthRef: in.TruthRef, Usage: []api.Amount{{Unit: "USD", Value: "0"}}, UsageFinal: true, CostUpperBound: []api.Amount{{Unit: "USD", Value: "0"}}, LatencyMillis: latency, ModelClaimedComplete: false, SystemAccepted: accepted, MayApplyLater: false, OperationRef: operation, ProofRef: proof, SafeRetry: false, ObservedAt: observed}
		original.State = "observed"
		original.Observation = &out
		return r.w.writeJSON(attemptPath(in.AttemptID), original)
	})
	return out, err
}

func (r *ReferenceRunner) Seal(ctx context.Context, pair domain.RunnerPair) (out domain.PairStopEvidence, err error) {
	if e := r.checkPair(pair); e != nil {
		return out, e
	}
	path := pairPath(pair.RunID, pair.Sample.SampleID)
	identity := pairIdentity(pair)
	// 先持久关闭原入口，不能因等待旧 IO 而让 queued 的启动继续进入。
	var current pairJournal
	e := r.w.readJSON(path+"/journal.json", &current)
	if e == nil && !api.Equal(current.Pair, identity) {
		return out, api.E("forbidden", "seal_original_pair_changed")
	}
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return out, e
	}
	if e = r.w.writeJSON(path+"/closed.json", struct {
		PairDigest string `json:"pair_digest"`
	}{directory(identity)}); e != nil {
		return out, e
	}
	r.mu.Lock()
	active := r.active[path]
	r.mu.Unlock()
	if active != nil {
		active.cancel()
		select {
		case <-active.done:
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
	err = r.w.lock(ctx, func() error {
		var j pairJournal
		e := r.w.readJSON(path+"/journal.json", &j)
		if errors.Is(e, os.ErrNotExist) {
			j = pairJournal{Pair: identity, State: "sealed"}
		} else if e != nil {
			return e
		}
		if !api.Equal(j.Pair, identity) {
			return api.E("forbidden", "seal_original_pair_changed")
		}
		if j.Stop != nil {
			out = *j.Stop
			return nil
		}
		// 持有同一 durable lock 证明原同步写入已退出；未知 outcome 仍留原 journal。
		for _, arm := range []string{"candidate", "baseline"} {
			if e = r.w.root.RemoveAll(path + "/environments/" + arm); e != nil {
				return e
			}
		}
		if e = r.w.root.RemoveAll(path + "/judge"); e != nil {
			return e
		}
		if e = r.w.syncDir(path); e != nil {
			return e
		}
		proof, e := r.w.proof(ctx, "reference_pair_sealed", struct {
			Class      string `json:"class"`
			RunID      string `json:"run_id"`
			SampleID   string `json:"sample_id"`
			PairDigest string `json:"pair_digest"`
			ObservedAt string `json:"observed_at"`
		}{ReferenceClass, pair.RunID, pair.Sample.SampleID, directory(identity), api.Time(r.w.clock())}, []api.ContentRef{pair.Sample.InputRef, pair.Sample.TruthRef})
		if e != nil {
			return e
		}
		out = domain.PairStopEvidence{CandidateStopped: true, BaselineStopped: true, CandidateDestroyed: true, BaselineDestroyed: true, MayApplyLater: false, ProofRef: proof}
		j.State = "sealed"
		j.Stop = &out
		return r.w.writeJSON(path+"/journal.json", j)
	})
	return out, err
}
func (r *ReferenceRunner) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.mu.Lock()
	r.closing = true
	active := make([]*activeRun, 0, len(r.active))
	for _, running := range r.active {
		active = append(active, running)
	}
	r.mu.Unlock()
	for _, running := range active {
		running.cancel()
		select {
		case <-running.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return r.w.root.Close()
}

var _ domain.EvaluationRunner = (*ReferenceRunner)(nil)
