package governance_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

// 这是实例宿主边界的有状态文件夹具，不保存 Grant/Approval 权威。
type lifecycleHost struct {
	root    string
	proof   api.ContentRef
	entered chan struct{}
	resume  chan struct{}
	starts  atomic.Int32
}

func (h *lifecycleHost) Prepare(_ context.Context, in governance.Installation) (governance.PreparationEvidence, error) {
	return governance.PreparationEvidence{Compatible: true, IsolationVerified: true, ArtifactDigest: in.InstallLockRef.Digest, ConfigDigest: in.ConfigRef.Digest, SelfTestRef: h.proof}, nil
}
func (h *lifecycleHost) Initialize(ctx context.Context, in governance.InstanceRequest) (governance.InstanceEvidence, error) {
	if h.starts.Add(1) == 1 && h.entered != nil {
		close(h.entered)
		select {
		case <-h.resume:
		case <-ctx.Done():
			return governance.InstanceEvidence{}, ctx.Err()
		}
	}
	if err := os.WriteFile(filepath.Join(h.root, in.InstanceID), []byte(in.Installation.InstallLockRef.Digest), 0600); err != nil {
		return governance.InstanceEvidence{}, err
	}
	return governance.InstanceEvidence{InstanceID: in.InstanceID, Generation: in.Generation, ArtifactDigest: in.Installation.InstallLockRef.Digest, ConfigDigest: in.ConfigRef.Digest, SelfTestRef: h.proof, Ready: true, ExpiresAt: in.Deadline}, nil
}
func (h *lifecycleHost) Fence(_ context.Context, in governance.InstanceRequest) (governance.FenceEvidence, error) {
	err := os.Remove(filepath.Join(h.root, in.InstanceID))
	if err != nil && !os.IsNotExist(err) {
		return governance.FenceEvidence{}, err
	}
	return governance.FenceEvidence{InstanceID: in.InstanceID, Generation: in.Generation, Exited: true, ProofRef: h.proof}, nil
}
func (h *lifecycleHost) Dispose(_ context.Context, in governance.Installation) (governance.DisposalEvidence, error) {
	entries, err := os.ReadDir(h.root)
	return governance.DisposalEvidence{Exited: len(entries) == 0, ResidualRefs: []api.ObjectRef{}}, err
}

func approveOriginal(t *testing.T, f *fixture, c api.Command, r api.Receipt) {
	t.Helper()
	if r.Stage != "accepted" {
		t.Fatalf("expected original accepted: %+v", r)
	}
	var output governance.ConfirmedOutput
	if err := api.Decode(r.Output, &output); err != nil {
		t.Fatal(err)
	}
	confirm := query[governance.ConfirmationView](t, f, "confirmation.read", governance.IDInput{ID: output.ConfirmationRef.ObjectID})
	_, decided := command(t, f, "confirmation.decide", confirm.RequestID, governance.ConfirmationDecision{RequestID: confirm.RequestID, RequestRevision: confirm.Revision, Decision: "approved", Challenge: confirm.Challenge, PreviewRefs: confirm.PreviewRefs}, nil)
	if decided.Stage != "applied" {
		t.Fatalf("decide: %+v", decided)
	}
	drain(t, f, "governance.confirmation")
	original, err := f.dispatcher.Lookup(f.ctx, f.auth, c.CommandID)
	if err != nil || original.Stage != "applied" {
		t.Fatalf("original: %+v %v", original, err)
	}
}
func installation(t *testing.T, f *fixture, target string) governance.Installation {
	t.Helper()
	install := governance.Installation{InstallLockRef: component("install"), Artifacts: []api.ContentRef{ref(t, f, "artifact")}, DependencyRefs: []api.ComponentRef{}, ConfigRef: component("config"), PlatformRef: component("platform"), ABI: "go-static-v1", Profile: api.Profile, ReadFormats: []string{"v1"}, WriteFormats: []string{"v1"}, TrustedBuiltin: true, IsolationRefs: []api.ContentRef{}}
	c, r := command(t, f, "extensions.prepare", target, governance.PrepareRequest{Installation: install, TargetRef: f.scope.Ref(target, 1)}, nil)
	if r.Stage != "accepted" {
		t.Fatalf("prepare: %+v", r)
	}
	drain(t, f, "governance.prepare")
	prepared, err := f.dispatcher.Lookup(f.ctx, f.auth, c.CommandID)
	if err != nil || prepared.Stage != "applied" {
		t.Fatalf("prepared: %+v %v", prepared, err)
	}
	return install
}
func approval(t *testing.T, f *fixture, install governance.Installation, target string) api.ObjectRef {
	t.Helper()
	id := api.NewID("approval")
	in := governance.ApprovalCreate{ApprovalID: id, InstallLockRef: install.InstallLockRef, Purpose: "compatibility", EvidenceRefs: []api.ObjectRef{}, CompatibilityEvidenceRefs: []api.ContentRef{ref(t, f, "compatibility")}, Rollout: governance.RolloutPolicy{TargetIDs: []string{target}, BatchSizes: []uint64{1}, MinimumSamples: 1, ObservationSeconds: 1, MaxErrorRate: "0", MaximumStartWindowSeconds: 60}, ExpiresAt: api.Time(time.Now().Add(time.Hour)), PreviewRefs: []api.ContentRef{ref(t, f, "preview")}}
	c, r := command(t, f, "release.approval.create", id, in, nil)
	approveOriginal(t, f, c, r)
	return f.scope.Ref(id, 1)
}
func TestActivationCallbackCannotOverwriteNewerCurrentInstance(t *testing.T) {
	h := &lifecycleHost{root: t.TempDir(), entered: make(chan struct{}), resume: make(chan struct{})}
	f := environment(t, governance.Options{Lifecycle: h})
	h.proof = ref(t, f, "selftest")
	target := api.NewID("target")
	_, registered := command(t, f, "extensions.target.register", target, governance.TargetRegister{TargetID: target, DataFormat: "v1"}, nil)
	if registered.Stage != "applied" {
		t.Fatalf("register target: %+v", registered)
	}
	install := installation(t, f, target)
	ap := approval(t, f, install, target)
	revision := uint64(1)
	activate := governance.ActivateRequest{TargetID: target, ExpectedGeneration: 0, InstallLockRef: install.InstallLockRef, ApprovalRef: ap, ConfigRef: install.ConfigRef, PrepareDeadline: api.Time(time.Now().Add(time.Hour))}
	oldCommand, old := command(t, f, "extensions.activate", target, activate, &revision)
	if old.Stage != "accepted" {
		t.Fatalf("activate A: %+v", old)
	}
	view := query[governance.ExtensionRead](t, f, "extensions.read", governance.IDInput{ID: target})
	if view.Head.Enabled || view.Readiness != nil {
		t.Fatalf("receipt prematurely published readiness: %+v", view)
	}
	jobs, status, err := f.store.Claim(f.ctx, f.scope, api.NewID("worker"), []string{"governance.activate"}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(jobs) != 1 {
		t.Fatalf("claim A: %+v %s %v", jobs, status, err)
	}
	handler, _ := f.registry.Job("governance.activate")
	finished := make(chan error, 1)
	go func() { finished <- handler(f.ctx, f.store, f.scope, jobs[0]) }()
	select {
	case <-h.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("initializer did not enter")
	}
	newCommand, newReceipt := command(t, f, "extensions.activate", target, activate, &revision)
	if newReceipt.Stage != "accepted" {
		t.Fatalf("activate B: %+v", newReceipt)
	}
	drain(t, f, "governance.activate")
	newView := query[governance.ExtensionRead](t, f, "extensions.read", governance.IDInput{ID: target})
	if !newView.Head.Enabled || newView.Head.Generation != 1 || newView.Readiness == nil {
		t.Fatalf("B not ready: %+v", newView)
	}
	close(h.resume)
	select {
	case err = <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("A callback did not finish")
	}
	current := query[governance.ExtensionRead](t, f, "extensions.read", governance.IDInput{ID: target})
	if !api.Equal(current.Head, newView.Head) {
		t.Fatalf("stale A changed B: %+v", current)
	}
	a, err := f.dispatcher.Lookup(f.ctx, f.auth, oldCommand.CommandID)
	if err != nil || a.Stage != "rejected" || a.Error.Reason != "generation_changed" {
		t.Fatalf("A: %+v %v", a, err)
	}
	b, err := f.dispatcher.Lookup(f.ctx, f.auth, newCommand.CommandID)
	if err != nil || b.Stage != "applied" {
		t.Fatalf("B: %+v %v", b, err)
	}
	drain(t, f, "governance.stop")
	if _, err = os.Stat(filepath.Join(h.root, newView.Readiness.InstanceID)); err != nil {
		t.Fatalf("A cleanup removed B: %v", err)
	}
}
