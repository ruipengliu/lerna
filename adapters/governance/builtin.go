package governance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

type BuiltinHostConfig struct {
	Root          string
	Scope         runtime.Scope
	Content       Content
	Clock         func() time.Time
	Installations []domain.Installation
	ReadinessTTL  time.Duration
}

type BuiltinHost struct {
	w         *workspace
	allow     map[string]domain.Installation
	ttl       time.Duration
	mu        sync.Mutex
	closeMu   sync.Mutex
	closing   bool
	closed    bool
	instances map[string]*liveInstance
}
type liveInstance struct {
	request domain.InstanceRequest
	cancel  context.CancelFunc
	done    chan struct{}
}
type installJournal struct {
	Installation domain.Installation        `json:"installation"`
	State        string                     `json:"state"`
	Evidence     domain.PreparationEvidence `json:"evidence"`
}
type instanceJournal struct {
	Request      domain.InstanceRequest  `json:"request"`
	State        string                  `json:"state"`
	PID          int                     `json:"pid"`
	ProcessStart string                  `json:"process_start"`
	Evidence     domain.InstanceEvidence `json:"evidence"`
	Fence        *domain.FenceEvidence   `json:"fence,omitempty"`
}

func NewBuiltinHost(c BuiltinHostConfig) (*BuiltinHost, error) {
	if len(c.Installations) == 0 || len(c.Installations) > 128 || c.ReadinessTTL <= 0 || c.ReadinessTTL > 10*time.Minute {
		return nil, api.E("invalid_request", "finite_builtin_allowlist_and_readiness_required")
	}
	allow := map[string]domain.Installation{}
	for _, original := range c.Installations {
		var in domain.Installation
		if e := api.Decode(api.Raw(original), &in); e != nil {
			return nil, e
		}
		if !in.TrustedBuiltin || in.ABI != "go-static-v1" || in.Profile != api.Profile || len(in.Artifacts) == 0 || len(in.Artifacts) > 32 {
			return nil, api.E("unsupported", "only_registered_static_builtin_supported")
		}
		for _, r := range []api.ComponentRef{in.InstallLockRef, in.ConfigRef, in.PlatformRef} {
			if e := api.ValidateRecord("ComponentRef", r); e != nil {
				return nil, e
			}
		}
		key := directory(in.InstallLockRef)
		if _, exists := allow[key]; exists {
			return nil, api.E("invalid_request", "duplicate_builtin_install_lock")
		}
		allow[key] = in
	}
	w, e := openWorkspace(c.Root, c.Scope, c.Content, c.Clock)
	if e != nil {
		return nil, e
	}
	return &BuiltinHost{w: w, allow: allow, ttl: c.ReadinessTTL, instances: map[string]*liveInstance{}}, nil
}
func (h *BuiltinHost) allowed(in domain.Installation) error {
	original, ok := h.allow[directory(in.InstallLockRef)]
	if !ok || !api.Equal(original, in) {
		return api.E("unsupported", "installation_not_in_trusted_builtin_allowlist")
	}
	return nil
}
func installPath(in domain.Installation) string {
	return "installations/" + directory(in.InstallLockRef)
}
func instancePath(r domain.InstanceRequest) string { return "instances/" + directory(r.InstanceID) }

func (h *BuiltinHost) selftest(ctx context.Context, path string, binding any, sources []api.ContentRef) (api.ContentRef, error) {
	if e := ctx.Err(); e != nil {
		return api.ContentRef{}, e
	}
	challenge := []byte("harness-static-host-probe/" + api.NewID("probe"))
	name := path + "/selftest.bin"
	if e := h.w.write(name, challenge); e != nil {
		return api.ContentRef{}, e
	}
	actual, e := h.w.read(name)
	if e != nil {
		return api.ContentRef{}, e
	}
	if string(actual) != string(challenge) {
		return api.ContentRef{}, api.E("dependency_unavailable", "builtin_file_probe_failed")
	}
	if e = h.w.root.Remove(name); e != nil {
		return api.ContentRef{}, e
	}
	if e = h.w.syncDir(path); e != nil {
		return api.ContentRef{}, e
	}
	digest, e := api.Digest(binding)
	if e != nil {
		return api.ContentRef{}, e
	}
	return h.w.proof(ctx, "builtin_selftest", struct {
		Kind          string `json:"kind"`
		BindingDigest string `json:"binding_digest"`
		ProbeHash     string `json:"probe_hash"`
		ObservedAt    string `json:"observed_at"`
	}{"trusted-static-private-file-selftest", digest, api.Hash(actual), api.Time(h.w.clock())}, sources)
}
func (h *BuiltinHost) Prepare(ctx context.Context, in domain.Installation) (out domain.PreparationEvidence, err error) {
	if e := h.allowed(in); e != nil {
		return out, e
	}
	err = h.w.lock(ctx, func() error {
		path := installPath(in)
		var j installJournal
		e := h.w.readJSON(path+"/journal.json", &j)
		if e == nil {
			if !api.Equal(j.Installation, in) {
				return api.E("forbidden", "installation_journal_changed")
			}
			if j.State == "disposed" || j.State == "disposing" {
				return api.E("invalid_state", "installation_disposal_started")
			}
		}
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		for _, r := range in.Artifacts {
			if _, e = h.w.exact(ctx, r, "extension.prepare.artifact"); e != nil {
				return e
			}
		}
		if e == nil && j.State == "prepared" {
			out = j.Evidence
			return nil
		}
		j = installJournal{Installation: in, State: "preparing"}
		if e = h.w.writeJSON(path+"/journal.json", j); e != nil {
			return e
		}
		proof, e := h.selftest(ctx, path, in, in.Artifacts)
		if e != nil {
			return e
		}
		out = domain.PreparationEvidence{ArtifactDigest: in.InstallLockRef.Digest, ConfigDigest: in.ConfigRef.Digest, Compatible: true, IsolationVerified: true, SelfTestRef: proof, Reason: "registered_static_builtin_no_uploaded_code"}
		j.State = "prepared"
		j.Evidence = out
		return h.w.writeJSON(path+"/journal.json", j)
	})
	return out, err
}

// processStart 核验原进程身份，PID 重用不能冒充原运行者。
func processStart(pid int) (string, error) {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return "", e
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return "", api.E("invalid_state", "process_probe_invalid")
	}
	fields := strings.Fields(string(b[end+1:]))
	if len(fields) < 20 {
		return "", api.E("invalid_state", "process_probe_invalid")
	}
	if fields[0] == "Z" {
		return "", os.ErrNotExist
	}
	return fields[19], nil
}
func processExited(j instanceJournal) (bool, error) {
	start, e := processStart(j.PID)
	if errors.Is(e, os.ErrNotExist) {
		return true, nil
	}
	if e != nil {
		return false, e
	}
	return start != j.ProcessStart, nil
}
func (h *BuiltinHost) Initialize(ctx context.Context, r domain.InstanceRequest) (out domain.InstanceEvidence, err error) {
	if e := h.allowed(r.Installation); e != nil {
		return out, e
	}
	if !api.ValidID(r.InstanceID) || !api.ValidID(r.ActivationID) || !api.ValidID(r.TargetID) || r.Generation == 0 || !api.Equal(r.ConfigRef, r.Installation.ConfigRef) {
		return out, api.E("invalid_request", "exact_original_instance_required")
	}
	deadline, e := api.ParseTime(r.Deadline)
	if e != nil {
		return out, e
	}
	if !h.w.clock().Before(deadline) {
		return out, api.E("expired", "instance_prepare_deadline")
	}
	err = h.w.lock(ctx, func() error {
		h.mu.Lock()
		closing := h.closing
		h.mu.Unlock()
		if closing {
			return api.E("invalid_state", "builtin_host_closing")
		}
		var install installJournal
		if e := h.w.readJSON(installPath(r.Installation)+"/journal.json", &install); e != nil {
			return e
		}
		if install.State != "prepared" {
			return api.E("invalid_state", "installation_not_prepared")
		}
		path := instancePath(r)
		var j instanceJournal
		e := h.w.readJSON(path+"/journal.json", &j)
		if e == nil {
			if !api.Equal(j.Request, r) {
				return api.E("forbidden", "original_instance_request_changed")
			}
			h.mu.Lock()
			live := h.instances[r.InstanceID]
			h.mu.Unlock()
			if j.State == "running" && live != nil {
				select {
				case <-live.done:
					return api.E("invalid_state", "original_instance_exited_requires_reopen")
				default:
					out = j.Evidence
					return nil
				}
			}
			return api.E("invalid_state", "original_instance_cannot_be_restarted_requires_reopen")
		}
		if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		start, e := processStart(os.Getpid())
		if e != nil {
			return e
		}
		j = instanceJournal{Request: r, State: "starting", PID: os.Getpid(), ProcessStart: start}
		if e = h.w.writeJSON(path+"/journal.json", j); e != nil {
			return e
		}
		until := h.w.clock().Add(h.ttl)
		if deadline.Before(until) {
			until = deadline
		}
		running, cancel := context.WithDeadline(context.Background(), until)
		done := make(chan struct{})
		type startup struct {
			proof api.ContentRef
			err   error
		}
		started := make(chan startup, 1)
		live := &liveInstance{r, cancel, done}
		// 原实例自己完成私有文件自检。准备、自检从不调用模型或目标。
		go func() {
			defer close(done)
			proof, e := h.selftest(running, path, r, r.Installation.Artifacts)
			started <- startup{proof, e}
			if e == nil {
				<-running.Done()
			}
		}()
		var proof api.ContentRef
		select {
		case result := <-started:
			if result.err != nil {
				cancel()
				<-done
				return result.err
			}
			proof = result.proof
		case <-ctx.Done():
			cancel()
			<-done
			return ctx.Err()
		}
		out = domain.InstanceEvidence{InstanceID: r.InstanceID, Generation: r.Generation, ArtifactDigest: r.Installation.InstallLockRef.Digest, ConfigDigest: r.ConfigRef.Digest, SelfTestRef: proof, Ready: true, ExpiresAt: api.Time(until)}
		j.State = "running"
		j.Evidence = out
		if e = h.w.writeJSON(path+"/journal.json", j); e != nil {
			cancel()
			<-done
			return e
		}
		h.mu.Lock()
		h.instances[r.InstanceID] = live
		h.mu.Unlock()
		return nil
	})
	return out, err
}
func (h *BuiltinHost) Fence(ctx context.Context, r domain.InstanceRequest) (out domain.FenceEvidence, err error) {
	if e := h.allowed(r.Installation); e != nil {
		return out, e
	}
	err = h.w.lock(ctx, func() error {
		path := instancePath(r)
		var j instanceJournal
		if e := h.w.readJSON(path+"/journal.json", &j); e != nil {
			return e
		}
		if !api.Equal(j.Request, r) {
			return api.E("forbidden", "fence_original_instance_changed")
		}
		if j.Fence != nil {
			out = *j.Fence
			return nil
		}
		j.State = "fencing"
		if e := h.w.writeJSON(path+"/journal.json", j); e != nil {
			return e
		}
		h.mu.Lock()
		live := h.instances[r.InstanceID]
		h.mu.Unlock()
		if live != nil {
			live.cancel()
			select {
			case <-live.done:
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			exited, e := processExited(j)
			if e != nil {
				return e
			}
			if !exited {
				return api.E("effect_unknown", "original_instance_running_handle_unavailable")
			}
		}
		proof, e := h.w.proof(ctx, "instance_fenced", struct {
			InstanceID   string `json:"instance_id"`
			Generation   uint64 `json:"generation"`
			PID          int    `json:"pid"`
			ProcessStart string `json:"process_start"`
			Exited       bool   `json:"exited"`
			ObservedAt   string `json:"observed_at"`
		}{r.InstanceID, r.Generation, j.PID, j.ProcessStart, true, api.Time(h.w.clock())}, []api.ContentRef{j.Evidence.SelfTestRef})
		if e != nil {
			return e
		}
		out = domain.FenceEvidence{InstanceID: r.InstanceID, Generation: r.Generation, Exited: true, MayApplyLater: false, ProofRef: proof}
		j.State = "exited"
		j.Fence = &out
		if e = h.w.writeJSON(path+"/journal.json", j); e != nil {
			return e
		}
		h.mu.Lock()
		delete(h.instances, r.InstanceID)
		h.mu.Unlock()
		return nil
	})
	return out, err
}
func (h *BuiltinHost) Dispose(ctx context.Context, in domain.Installation) (out domain.DisposalEvidence, err error) {
	if e := h.allowed(in); e != nil {
		return out, e
	}
	out.ResidualRefs = []api.ObjectRef{}
	err = h.w.lock(ctx, func() error {
		path := installPath(in)
		var j installJournal
		if e := h.w.readJSON(path+"/journal.json", &j); e != nil {
			return e
		}
		j.State = "disposing"
		if e := h.w.writeJSON(path+"/journal.json", j); e != nil {
			return e
		}
		f, e := h.w.root.Open("instances")
		if errors.Is(e, os.ErrNotExist) {
			e = nil
		} else if e == nil {
			entries, re := f.ReadDir(maxRecords + 1)
			ce := f.Close()
			if re != nil && !errors.Is(re, io.EOF) {
				return re
			}
			if ce != nil {
				return ce
			}
			if len(entries) > maxRecords {
				return api.E("unsupported", "instance_scan_bound")
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					return api.E("invalid_state", "instance_journal_not_directory")
				}
				var instance instanceJournal
				if e = h.w.readJSON("instances/"+entry.Name()+"/journal.json", &instance); e != nil {
					return e
				}
				if !api.Equal(instance.Request.Installation.InstallLockRef, in.InstallLockRef) || instance.State == "exited" {
					continue
				}
				out.ResidualRefs = append(out.ResidualRefs, h.w.scope.Ref(instance.Request.InstanceID, 1))
			}
		}
		if e != nil {
			return e
		}
		if len(out.ResidualRefs) > 0 {
			return nil
		}
		// 仅删除 staging；最小原实例和终态 journal 永久保留，阻止复活。
		if e = h.w.root.RemoveAll(path + "/staging"); e != nil {
			return e
		}
		if e = h.w.syncDir(path); e != nil {
			return e
		}
		j.State = "disposed"
		if e = h.w.writeJSON(path+"/journal.json", j); e != nil {
			return e
		}
		out.Exited = true
		return nil
	})
	return out, err
}

func (h *BuiltinHost) Close() error {
	h.closeMu.Lock()
	defer h.closeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closing = true
	h.mu.Unlock()
	var requests []domain.InstanceRequest
	// 与原 Initialize 同一锁域收敛；启动在注册 handle 前不能被 Close 漏掉。
	if e := h.w.lock(ctx, func() error {
		h.mu.Lock()
		defer h.mu.Unlock()
		requests = make([]domain.InstanceRequest, 0, len(h.instances))
		for _, live := range h.instances {
			requests = append(requests, live.request)
		}
		return nil
	}); e != nil {
		return e
	}
	for _, r := range requests {
		if _, e := h.Fence(ctx, r); e != nil {
			return e
		}
	}
	if e := h.w.root.Close(); e != nil {
		return e
	}
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	return nil
}

var _ domain.LifecyclePort = (*BuiltinHost)(nil)
