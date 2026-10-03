package wasi

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
)

type process struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func decimalSeconds(seconds, nanos uint64) string {
	if nanos == 0 {
		return strconv.FormatUint(seconds, 10)
	}
	part := strconv.FormatUint(nanos+1000000000, 10)[1:]
	return strconv.FormatUint(seconds, 10) + "." + strings.TrimRight(part, "0")
}

func (r *Runtime) Start(ctx context.Context, q execution.AttemptRequest, barrier func(context.Context) error) (execution.Fact, error) {
	encoded, digest, err := r.decodeAttempt(q)
	if err != nil {
		return execution.Fact{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	p := &process{cancel: cancel, done: make(chan struct{})}
	r.mu.Lock()
	if r.closing || r.active[q.Attempt.AttemptID] != nil || len(r.active) >= r.cfg.MaxConcurrent {
		r.mu.Unlock()
		cancel()
		return execution.Fact{}, api.E("overloaded", "wasi_process_capacity_unavailable")
	}
	r.active[q.Attempt.AttemptID] = p
	r.mu.Unlock()
	defer func() { cancel(); r.mu.Lock(); delete(r.active, q.Attempt.AttemptID); close(p.done); r.mu.Unlock() }()
	if _, err = r.record(q.Attempt.AttemptID, digest); err == nil {
		return execution.Fact{}, api.E("invalid_state", "wasi_attempt_already_started")
	} else if !errors.Is(err, os.ErrNotExist) {
		return execution.Fact{}, err
	}
	module, err := r.readModule(ctx, q.Scope, q.Auth, encoded.Arguments.CodeRef)
	if err != nil {
		return execution.Fact{}, err
	}
	if err = r.verifyExecutables(); err != nil {
		return execution.Fact{}, err
	}
	// 没有任何用户程序执行发生在 Prepare 或 barrier 确认之前。
	if err = barrier(ctx); err != nil {
		return execution.Fact{}, err
	}
	record := runRecord{AttemptID: q.Attempt.AttemptID, BindingDigest: digest, Phase: "started", Result: WorkerResult{Protocol: WorkerProtocol, Output: []byte{}}, Usage: []api.Amount{}}
	if err = r.createRecord(record); err != nil {
		return execution.Fact{}, err
	}
	wall := time.Duration(encoded.Limits.WallMillis) * time.Millisecond
	childCtx, childCancel := context.WithTimeout(ctx, wall+time.Second)
	defer childCancel()
	cmd := r.command(childCtx, encoded.Limits.CPUSeconds)
	cmd.Stdin = bytes.NewReader(api.Raw(WorkerRequest{Protocol: WorkerProtocol, Module: module, Input: q.Attempt.Prepared.Cell.Namespace, MemoryPages: encoded.Limits.MemoryPages, OutputBytes: encoded.Limits.OutputBytes, WallMillis: encoded.Limits.WallMillis}))
	stdout, stderr := limitedBuffer{limit: 128 << 10}, limitedBuffer{limit: 4096}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	startErr := cmd.Start()
	if startErr == nil {
		record.PID, record.SpawnCount = cmd.Process.Pid, 1
		record.ProcessStart, err = processStart(record.PID)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			childCancel()
		}
		// 即使这个更新失答复，也必须先等待原进程退出；不得释放活动槽或重发。
		if e := r.updateRecord(record); e != nil {
			childCancel()
			cmd.Wait()
			return execution.Fact{}, e
		}
		waitErr := cmd.Wait()
		if cmd.ProcessState != nil {
			n := uint64((cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime()).Nanoseconds())
			record.Usage = []api.Amount{{Unit: "cpu_seconds", Value: decimalSeconds(n/1000000000, n%1000000000)}}
			record.UsageFinal = true
		}
		if waitErr == nil && !stdout.exceeded && !stderr.exceeded && api.Decode(stdout.Bytes(), &record.Result) == nil && record.Result.Protocol == WorkerProtocol {
			if record.Result.Success {
				var ns execution.PassiveNamespace
				if uint64(len(record.Result.Output)) > encoded.Limits.NamespaceBytes || api.Decode(record.Result.Output, &ns) != nil || execution.ValidateNamespace(ns) != nil {
					record.Result = WorkerResult{Protocol: WorkerProtocol, Reason: "cell_namespace_invalid", Output: []byte{}}
				}
			}
		} else {
			reason := "cell_process_failure"
			if ctx.Err() != nil {
				reason = "cell_cancelled"
			} else if childCtx.Err() != nil {
				reason = "cell_wall_limit"
			} else if stdout.exceeded || stderr.exceeded {
				reason = "cell_output_limit"
			}
			record.Result = WorkerResult{Protocol: WorkerProtocol, Reason: reason, Output: []byte{}}
		}
	} else {
		record.Result.Reason = "cell_process_not_started"
		record.Usage = []api.Amount{{Unit: "cpu_seconds", Value: "0"}}
		record.UsageFinal = true
	}
	record.Phase = "finished"
	if err = r.updateRecord(record); err != nil {
		return execution.Fact{}, err
	}
	return r.fact(ctx, q, record)
}

func (r *Runtime) fact(ctx context.Context, q execution.AttemptRequest, record runRecord) (execution.Fact, error) {
	fact := execution.Fact{Revision: q.Attempt.FactRevision + 1, Effect: "not_applied", MayApplyLater: false, Evidence: []api.ContentRef{}, Usage: record.Usage, UsageFinal: record.UsageFinal}
	if record.Phase == "started" {
		fact.Effect = "unknown"
		fact.MayApplyLater = true
		fact.UsageFinal = false
		return fact, nil
	}
	if record.Phase == "lost" {
		fact.Effect = "unknown"
	}
	if record.Result.Success {
		var ns execution.PassiveNamespace
		if err := api.Decode(record.Result.Output, &ns); err != nil {
			return execution.Fact{}, err
		}
		fact.Effect = "applied"
		fact.Namespace = &ns
	}
	receipt := Receipt{AttemptID: record.AttemptID, OperationID: q.Invoke.OperationID, BindingDigest: record.BindingDigest, InstallLock: r.InstallLockRef(), Phase: record.Phase, SpawnCount: record.SpawnCount, Reason: record.Result.Reason, OutputHash: api.Hash(record.Result.Output), ActuallyExited: true, Usage: record.Usage, UsageFinal: record.UsageFinal}
	b := api.Raw(receipt)
	ref, err := r.cfg.Content.Publish(ctx, q.Scope, q.Auth, execution.Publication{ContentID: "content_" + strings.TrimPrefix(api.Hash(b), "sha256:")[:32], MediaType: "application/json", Purpose: "execution_wasi_receipt", Location: r.cfg.Location, ProcessedSources: q.Attempt.Prepared.Cell.Sources, DisclosedSources: []api.ContentRef{}}, b)
	if err != nil {
		return execution.Fact{}, err
	}
	fact.Evidence = []api.ContentRef{ref}
	return fact, nil
}

func (r *Runtime) Reconcile(ctx context.Context, q execution.AttemptRequest) (execution.Fact, error) {
	_, digest, err := r.decodeAttempt(q)
	if err != nil {
		return execution.Fact{}, err
	}
	r.mu.Lock()
	active := r.active[q.Attempt.AttemptID]
	r.mu.Unlock()
	record, err := r.record(q.Attempt.AttemptID, digest)
	if errors.Is(err, os.ErrNotExist) {
		return execution.Fact{Revision: q.Attempt.FactRevision + 1, Effect: "unknown", MayApplyLater: false, Evidence: []api.ContentRef{}, Usage: []api.Amount{}, UsageFinal: false}, nil
	}
	if err != nil {
		return execution.Fact{}, err
	}
	if record.Phase == "started" && active == nil {
		if err = fenceProcess(ctx, record.PID, record.ProcessStart); err != nil {
			return execution.Fact{}, err
		}
		record.Phase = "lost"
		record.Result = WorkerResult{Protocol: WorkerProtocol, Reason: "original_process_result_unknown", Output: []byte{}}
		if err = r.updateRecord(record); err != nil {
			return execution.Fact{}, err
		}
	}
	fact, err := r.fact(ctx, q, record)
	if err != nil {
		return execution.Fact{}, err
	}
	if q.Attempt.CellCommitted && q.Attempt.ResultRef != nil {
		fact.Output, err = r.cfg.Content.ReadBytes(ctx, q.Scope, q.Auth, *q.Attempt.ResultRef, "execution_result", r.cfg.Location)
		if err != nil {
			return execution.Fact{}, err
		}
		fact.Effect = "applied"
	}
	return fact, nil
}
func (r *Runtime) Stop(ctx context.Context, q execution.AttemptRequest) (execution.StopFact, error) {
	_, digest, err := r.decodeAttempt(q)
	if err != nil {
		return execution.StopFact{}, err
	}
	r.mu.Lock()
	active := r.active[q.Attempt.AttemptID]
	r.mu.Unlock()
	if active != nil {
		active.cancel()
		select {
		case <-active.done:
		case <-ctx.Done():
			return execution.StopFact{ActuallyStopped: false, MayApplyLater: "unknown"}, ctx.Err()
		}
	} else {
		record, e := r.record(q.Attempt.AttemptID, digest)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return execution.StopFact{}, e
		}
		if e == nil && record.Phase == "started" {
			if err = fenceProcess(ctx, record.PID, record.ProcessStart); err != nil {
				return execution.StopFact{ActuallyStopped: false, MayApplyLater: "unknown"}, err
			}
		}
	}
	return execution.StopFact{ActuallyStopped: true, MayApplyLater: false}, nil
}
func (r *Runtime) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closing = true
	active := make([]*process, 0, len(r.active))
	for _, p := range r.active {
		p.cancel()
		active = append(active, p)
	}
	r.mu.Unlock()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for _, p := range active {
		select {
		case <-p.done:
		case <-deadline.C:
			return api.E("dependency_unavailable", "wasi_actual_exit_not_confirmed")
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	return errors.Join(r.lock.Close(), r.root.Close())
}
