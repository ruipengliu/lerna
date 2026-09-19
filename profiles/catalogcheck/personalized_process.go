package catalogcheck

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"io"
	contentpolicy "lerna/adapters/content/policy"
	contextmemory "lerna/adapters/context/memory"
	"lerna/adapters/execution/simworkflow"
	"lerna/answers"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/execution"
	"lerna/memory"
	"lerna/tasks"
	"os"
	"path/filepath"
	"time"
)

// Local, private reference-host checkpoint. Never send tokens/read materials to a model.
type actionContextCheckpoint struct {
	fileImage                                  []byte
	SelectedRecord                             string
	AbsentContexts                             []uint32
	RetiredContexts                            []uint32            `json:",omitempty"`
	ContextDigests                             map[uint32][32]byte `json:",omitempty"`
	BoundContexts                              []uint32            `json:",omitempty"`
	Operations                                 []string
	Lineage                                    answers.Lineage
	Format                                     int
	Namespace, Token, DiscoveryToken, Location string
	Now                                        time.Time
	Binding                                    actionMemoryBinding
	Baseline                                   tasks.RunSnapshot
	Request                                    execution.Request
	SnapshotSHA                                *[32]byte `json:",omitempty"`
}

// ContextSnapshots counts live or retired snapshot identities; it does not
// assert that retired bodies still exist. AbsentSnapshots counts Core-proven gaps.
type ActionRecoveryReport struct {
	Cleanup                                         memory.DeletionStatus
	ContextErased                                   bool
	ContextSnapshots                                int
	AbsentSnapshots                                 int
	Corrections                                     uint32
	TaskState                                       string
	ModelCalls, GovernedArtifacts, RevokedArtifacts int
	InitialEffect                                   string
	OriginalIdentity, Started, WasStarted           bool
	ReadAllocated                                   uint64
	State, OtherState, ValidationError, Effect      string
	Ledger                                          int64
}

// CheckpointPersonalizedAction exits 73 after durable Invocation admission,
// before the first Start and before deferred store cleanup can run.
func CheckpointPersonalizedAction(ctx context.Context, root string) error {
	return CheckpointPersonalizedActionAt(ctx, root, "")
}
func CheckpointPersonalizedActionAt(ctx context.Context, root, point string) error {
	mode, stage := "personalized-item", "invoked"
	switch point {
	case "", "invoked":
	case "effect":
		stage = "effect"
	case "bound-failure-invoked":
		mode = "personalized-reassemble-bound-failure"
	case "gap-invoked":
		mode = "personalized-reassemble-gap"
	case "gap-effect":
		mode = "personalized-reassemble-gap"
		stage = "effect"
	case "reassembled-invoked":
		mode = "personalized-reassemble-once"
	case "reassembled-effect":
		mode = "personalized-reassemble-once"
		stage = "effect"
	case "second-invoked":
		mode = "personalized-multistep"
	case "second-effect":
		mode = "personalized-multistep"
		stage = "effect"
	default:
		return fmt.Errorf("unknown checkpoint point")
	}
	_, err := runActionCaseAt(ctx, mode, nil, true, root, stage)
	return err
}
func (a *actionHost) checkpointInvocation(ctx context.Context, req execution.Request) error {
	cp := actionContextCheckpoint{SelectedRecord: a.selected, ContextDigests: a.personal.snapshotDigests, Lineage: a.personal.lineage, Format: 2, Namespace: a.h.namespace, Token: a.h.token, DiscoveryToken: a.f.host.token, Location: a.location, Now: a.h.now(), Binding: a.personal.checkpoint, Baseline: a.personal.baseline, Request: req}
	current, err := a.h.core.Load(ctx, a.personal.baseline.Task.Ref)
	if err != nil {
		return err
	}
	if err = captureContextHistory(ctx, a.personal.snapshots, &cp, current); err != nil {
		return err
	}
	for _, action := range current.Actions.Actions {
		cp.Operations = append(cp.Operations, action.OperationID)
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return fmt.Errorf("checkpoint capacity")
	}
	file, err := os.OpenFile(filepath.Join(a.f.root, "action-context.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	dir, err := os.Open(a.f.root)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return err
	}
	os.Exit(73)
	return nil
}
func readActionContext(root string) (actionContextCheckpoint, error) {
	var cp actionContextCheckpoint
	path := filepath.Join(root, "action-context.json")
	stat, err := os.Lstat(path)
	if err != nil {
		return cp, err
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 || stat.Size() > 1<<20 {
		return cp, fmt.Errorf("private bounded checkpoint required")
	}
	f, err := os.Open(path)
	if err != nil {
		return cp, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return cp, err
	}
	if len(raw) > 1<<20 || json.Unmarshal(raw, &cp) != nil || ((cp.Format != 1 && cp.Format != 2) || (cp.Format == 1 && (cp.SnapshotSHA == nil || len(cp.BoundContexts) != 0)) || (cp.Format == 2 && (cp.SnapshotSHA != nil || len(cp.ContextDigests) != 0))) || cp.Token == "" || cp.DiscoveryToken == "" || cp.Request.OperationID == "" || cp.Namespace != cp.Baseline.Task.Ref.Namespace {
		return cp, fmt.Errorf("invalid action checkpoint")
	}
	cp.fileImage = raw
	return cp, nil
}
func RestorePersonalizedInvocation(ctx context.Context, root string, revoke bool) (ActionRecoveryReport, error) {
	mutation := ""
	if revoke {
		mutation = "revoke"
	}
	return restorePersonalizedAction(ctx, root, mutation, false)
}

// RestorePersonalizedInvocationAfterDeletion deletes the source through the
// current local authority before reconciling the original persisted invocation.
// It never requests a replacement read or invokes a new business operation.
func RestorePersonalizedInvocationAfterDeletion(ctx context.Context, root string) (ActionRecoveryReport, error) {
	return restorePersonalizedAction(ctx, root, "delete", false)
}

// RestorePersonalizedTask completes the original action through Core and checks
// the four governed artifacts, including current Memory disclosure revocation.
func RestorePersonalizedTask(ctx context.Context, root string) (ActionRecoveryReport, error) {
	return restorePersonalizedAction(ctx, root, "", true)
}
func restorePersonalizedAction(ctx context.Context, root, mutation string, complete bool) (report ActionRecoveryReport, err error) {
	cp, err := readActionContext(root)
	if err != nil {
		return report, err
	}
	defs, err := simworkflow.Definitions()
	if err != nil {
		return report, err
	}
	if cp.Namespace != defs[0].Kind {
		return report, fmt.Errorf("unknown reference namespace")
	}
	h, err := openRuntime(ctx, filepath.Join(root, cp.Namespace), cp.Token, defs[0], false)
	if err != nil {
		return report, fmt.Errorf("execution host: %w", err)
	}
	defer h.close()
	// This reference host uses a deterministic clock, restored from its checkpoint.
	h.clock.advance(cp.Now.Sub(h.now()))
	def := defs[0]
	def.Kind = "local"
	discovery, err := openRuntime(ctx, filepath.Join(root, "discovery"), cp.DiscoveryToken, def, false)
	if err != nil {
		return report, fmt.Errorf("discovery host: %w", err)
	}
	defer discovery.close()
	discovery.clock.advance(cp.Now.Sub(discovery.now()))
	locations := []string{"local"}
	if cp.Location != "local" {
		locations = append(locations, cp.Location)
	}
	policy, err := contentpolicy.New([]contentpolicy.Rule{{Kind: "catalog", Key: "business-manifest", Revision: 1, Actions: []string{"process", "discover", "disclose"}, Purposes: []string{"task"}, Locations: locations, RetainUntil: cp.Now.Add(time.Hour).Unix()}})
	if err != nil {
		return report, err
	}
	selected := cp.SelectedRecord
	if selected == "" {
		selected = "item"
	}
	if selected != "item" && selected != "alternative" {
		return report, fmt.Errorf("invalid original selected record")
	}
	a := &actionHost{h: h, f: &fixture{root: root, host: discovery, dataPolicy: policy}, location: cp.Location, selected: selected}
	port, err := h.work.Actions(tasks.ActionLimits{MaxOperations: 12, MaxQueries: 128, MaxCorrections: 2, InputTokens: (&actionScript{}).Capabilities().InputUpper, OutputTokens: 1024})
	if err != nil {
		return report, fmt.Errorf("action port: %w", err)
	}
	current, err := h.core.Load(ctx, cp.Baseline.Task.Ref)
	if err != nil {
		return report, err
	}
	svc, err := a.bindRecovered(ctx, current.Task, cp.Request.DescriptorSHA256)
	if err != nil {
		return report, fmt.Errorf("execution qualification: %w", err)
	}
	original, err := svc.GetInvocation(ctx, cp.Request.OperationID)
	if err != nil {
		return report, fmt.Errorf("original invocation: %w", err)
	}
	report.WasStarted = original.Started
	report.InitialEffect = original.Effect
	a.personal, err = a.personalizeBindings(ctx, port, cp.Baseline, "", false, &cp.Binding, !original.Started)
	if err != nil {
		return report, fmt.Errorf("memory binding: %w", err)
	}
	defer a.personal.close()
	cp, err = migrateActionCheckpoint(ctx, root, a.personal.snapshots, cp, current)
	if err != nil {
		return report, fmt.Errorf("checkpoint migration: %w", err)
	}
	if mutation != "" {
		if err = a.personal.change(ctx, h, mutation); err != nil {
			return report, err
		}
	}
	if mutation == "delete" {
		var e error
		report.Cleanup, e = a.personal.observeDeletionCleanup(ctx)
		if e != nil {
			return report, e
		}

		_, e = a.personal.snapshots.Read(ctx, cp.Binding.Key)
		if e != contextassembly.Invalidated {
			return report, fmt.Errorf("deleted snapshot cleanup not confirmed: %v", e)
		}
		report.ContextErased = true
	}
	// Original effect reconciliation uses the persisted invocation and Core
	// action identities. It must survive removal of the decision's payload.
	needsContext := !original.Started || complete

	if original.Started && complete {
		expected := contextmemory.SourceReference(contextassembly.Reference{Namespace: cp.Namespace, Collection: "personal", Key: "record-choice", Revision: a.personal.checkpoint.Revision})
		if len(cp.Lineage.Sources) != 1 || !proto.Equal(cp.Lineage.Sources[0], expected) || cp.Lineage.RetainUntil <= h.now().Unix() {
			return report, fmt.Errorf("original derived source manifest unavailable")
		}
		// This is an influence manifest, not an authorization. Every new artifact
		// still passes the restored dynamic source resolver and current policy.
		a.personal.lineage = cp.Lineage
	}
	if needsContext {
		err := verifyActionCheckpoint(ctx, a.personal.snapshots, cp)
		if err == contextassembly.Invalidated && !original.Started {
			// Cleanup is a governed rejection of a new Start, not evidence that
			// the persisted invocation should be replaced or submitted again.
			report.ValidationError = err.Error()
			needsContext = false
		} else if err != nil {
			return report, err
		}
	}
	if err = verifyOriginalActions(current, cp); err != nil {
		return report, err
	}
	if needsContext {
		if _, err = verifyContextHistory(ctx, a.personal.snapshots, cp, current); err == contextassembly.Invalidated && !original.Started {
			report.ValidationError = err.Error()
			needsContext = false
		} else if err != nil {
			return report, fmt.Errorf("initial context history: %w", err)
		}
	}
	guard, err := a.personal.session.StartGuard(cp.Request, cp.Baseline.Task)
	if err != nil {
		return report, err
	}
	svc, err = svc.BindStartGuard(guard)
	if err != nil {
		return report, err
	}
	if original.Started {
		if _, e := svc.Reconcile(ctx, cp.Request.OperationID); e != nil {
			report.ValidationError = e.Error()
		}
	}
	for range 2 {
		if report.ValidationError != "" && !original.Started {
			break
		}
		if _, e := svc.Run(ctx, cp.Request.OperationID); e != nil {
			report.ValidationError = e.Error()
			break
		}
	}
	if report.ValidationError == "" {
		if err = svc.Drain(ctx, 16); err != nil {
			return report, err
		}
	}
	if complete && report.ValidationError == "" {
		model := &recoveryOnlyModel{}
		runner, e := brain.NewActions(model, a, port)
		if e != nil {
			return report, e
		}
		run, e := runner.Run(ctx, cp.Baseline.Task.Ref)
		report.ModelCalls = model.calls
		if e != nil {
			return report, e
		}
		report.TaskState = run.Task.State

	}
	got, err := svc.GetInvocation(ctx, cp.Request.OperationID)
	if err != nil {
		return report, fmt.Errorf("final invocation: %w", err)
	}
	rawA, _ := json.Marshal(got.Request)
	rawB, _ := json.Marshal(cp.Request)
	report.OriginalIdentity = string(rawA) == string(rawB)
	report.Started = got.Started
	report.Effect = got.Effect
	for _, id := range a.personal.grantIDs {
		grant, err := h.grants.Get(ctx, h.token, id)
		if err != nil {
			return report, err
		}
		report.ReadAllocated += grant.Allocated
	}
	target, err := h.target.Snapshot(ctx, selected)
	if err != nil {
		return report, err
	}
	report.State, report.Ledger = target.State, target.Ledger

	otherRecord := "alternative"
	if selected == "alternative" {
		otherRecord = "item"
	}
	other, err := h.target.Snapshot(ctx, otherRecord)
	if err != nil {
		return report, err
	}
	report.OtherState = other.State
	final, err := h.core.Load(ctx, cp.Baseline.Task.Ref)
	if err != nil {
		return report, err
	}
	report.Corrections = final.Actions.Corrections
	if err = verifyOriginalActions(final, cp); err != nil {
		return report, err
	}
	// Recovery reporting verifies original identities even when a new Start was
	// refused or only an already-started effect was reconciled. Confirmed
	// retirement preserves history, but never authorizes another execution.
	if report.ContextSnapshots, err = verifyCheckpointHistory(ctx, a.personal.snapshots, cp, final, true); err != nil {
		return report, fmt.Errorf("final context history: %w", err)
	}
	report.AbsentSnapshots = len(cp.AbsentContexts)
	if needsContext && !(report.ValidationError != "" && !got.Started) {
		err := verifyActionCheckpoint(ctx, a.personal.snapshots, cp)
		if err != nil {
			return report, err
		}
	}
	// Verify recovery history before this diagnostic deliberately revokes source
	// permissions. Its cleanup worker may immediately retire the current body.
	if complete && report.TaskState == "COMPLETED" {
		var artifacts ActionReport
		if err = a.checkDerived(ctx, final, &artifacts); err != nil {
			return report, err
		}
		report.GovernedArtifacts, report.RevokedArtifacts = artifacts.GovernedArtifacts, artifacts.RevokedArtifacts
	}
	return report, nil
}

// Wrap the actual target at its public Start boundary; no simulated journal
// writes are used to manufacture Started or a business effect.
type effectCrashDriver struct {
	execution.Driver
	checkpoint func(context.Context) error
}

func (d effectCrashDriver) Start(ctx context.Context, call execution.Call) error {
	if err := d.Driver.Start(ctx, call); err != nil {
		return err
	}
	return d.checkpoint(ctx)
}
func (a *actionHost) effectCheckpointService(ctx context.Context, req execution.Request) (*execution.Service, error) {
	var cap execution.Capability
	for _, entry := range a.h.entries {
		if entry.Ref.Digest == req.DescriptorSHA256 {
			cap = entry.Capability
			break
		}
	}
	current, err := a.h.core.Load(ctx, a.personal.baseline.Task.Ref)
	if err != nil {
		return nil, err
	}
	driver := effectCrashDriver{Driver: a.h.target, checkpoint: func(ctx context.Context) error { return a.checkpointInvocation(ctx, req) }}
	svc, err := execution.New(a.h.grants, a.h.work.WithActionRecovery(tasks.QualificationOf(current)), a.h.access, driver, a.h.binding, cap, config(), a.h.operation)
	if err != nil {
		return nil, err
	}
	return svc.WithResourceControl(a.h.scope(), a.h.target)
}

// A recovery that requests a new model result fails visibly instead of silently
// turning this fixture into a new decision or consuming an external request.
type recoveryOnlyModel struct{ calls int }

func (*recoveryOnlyModel) Capabilities() brain.Capabilities { return (&actionScript{}).Capabilities() }
func (m *recoveryOnlyModel) Generate(context.Context, brain.Request) (brain.Result, error) {
	m.calls++
	return brain.Result{}, fmt.Errorf("unexpected model generation during recovery")
}

func verifyOriginalActions(run tasks.RunSnapshot, cp actionContextCheckpoint) error {
	ops := cp.Operations
	if len(ops) == 0 {
		ops = []string{cp.Request.OperationID}
	}
	if len(ops) > 64 || run.Actions == nil || len(run.Actions.Actions) != len(ops) {
		return fmt.Errorf("recovery changed action count")
	}
	for i, op := range ops {
		if run.Actions.Actions[i].OperationID != op {
			return fmt.Errorf("recovery replaced Core action")
		}
	}
	return nil
}

// Missing context is not inferred from a missing row alone. Only Core's original
// zero-request, metadata-only failure permits an explicitly recorded absence.
func unboundDecision(run tasks.RunSnapshot, number uint32) bool {
	if run.Actions == nil || number == 0 || int(number) > len(run.Actions.Decisions) {
		return false
	}
	d := run.Actions.Decisions[number-1]
	r := d.Record
	if d.Number != number || d.Admitted || r == nil || r.Evidence != "" || r.Usage != (tasks.GenerationUsage{}) || d.Rejection != r.Error {
		return false
	}
	return (r.Error == "CONTEXT_INVALIDATED" || r.Error == "CONTEXT_DENIED") && r.Proposal.Kind == "" && r.Proposal.Reason == "" && r.Proposal.Evidence == "" && len(r.Proposal.Actions) == 0
}

type checkpointSnapshots interface {
	contextassembly.Store
	contextassembly.CheckpointStore
}

func verifyActionCheckpoint(ctx context.Context, store contextassembly.CheckpointStore, cp actionContextCheckpoint) error {
	if cp.Format == 1 && cp.SnapshotSHA != nil {
		return store.BindCheckpoint(ctx, cp.Binding.Key, *cp.SnapshotSHA)
	}
	return store.VerifyCheckpoint(ctx, cp.Binding.Key)
}

func captureContextHistory(ctx context.Context, store checkpointSnapshots, cp *actionContextCheckpoint, run tasks.RunSnapshot) error {
	want := cp.Binding.Key.Decision
	if want == 0 || want > 512 {
		return fmt.Errorf("invalid context history size")
	}
	cp.AbsentContexts = nil
	cp.RetiredContexts = nil
	cp.BoundContexts = nil
	for n := uint32(1); uint64(n) <= want; n++ {
		key := cp.Binding.Key
		key.Decision = uint64(n)
		snapshot, err := store.Read(ctx, key)
		if err == contextassembly.Missing && unboundDecision(run, n) {
			if _, known := cp.ContextDigests[n]; known {
				return fmt.Errorf("previously bound context disappeared")
			}
			cp.AbsentContexts = append(cp.AbsentContexts, n)
			continue
		}
		if err == nil {
			digest := sha256.Sum256(snapshot.Document)
			if old, known := cp.ContextDigests[n]; known && old != digest {
				return fmt.Errorf("previously bound context changed")
			}
			err = store.BindCheckpoint(ctx, key, digest)
		}
		if err == contextassembly.Invalidated && uint64(n) < want {
			if err = store.VerifyCheckpoint(ctx, key); err != contextassembly.Invalidated {
				return fmt.Errorf("retired comparison not erased: %v", err)
			}
			cp.RetiredContexts = append(cp.RetiredContexts, n)
			continue
		}
		if err != nil {
			return err
		}
		cp.BoundContexts = append(cp.BoundContexts, n)
	}
	cp.ContextDigests = nil
	cp.SnapshotSHA = nil
	_, err := verifyContextHistory(ctx, store, *cp, run)
	return err
}

func verifyContextHistory(ctx context.Context, store checkpointSnapshots, cp actionContextCheckpoint, run tasks.RunSnapshot) (int, error) {
	return verifyCheckpointHistory(ctx, store, cp, run, false)
}

// Maintenance accepts confirmed retirement of the current comparison, but this
// does not authorize a new Start. Execution still requires a live checkpoint.
func verifyCheckpointHistory(ctx context.Context, store checkpointSnapshots, cp actionContextCheckpoint, run tasks.RunSnapshot, maintenance bool) (int, error) {
	want := cp.Binding.Key.Decision
	if want == 0 || want > 512 || run.Actions == nil || len(run.Actions.Decisions) < int(want) || run.Task.Ref.Namespace != cp.Binding.Key.Namespace || run.Task.Ref.TaskID != cp.Binding.Key.TaskID {
		return 0, fmt.Errorf("invalid context history authority")
	}
	digests := cp.ContextDigests
	live := cp.BoundContexts
	if cp.Format == 1 {
		if len(digests) == 0 && len(cp.AbsentContexts) == 0 && len(cp.RetiredContexts) == 0 && cp.SnapshotSHA != nil {
			digests = map[uint32][32]byte{uint32(want): *cp.SnapshotSHA}
		}
		for n := range digests {
			live = append(live, n)
		}
	}
	if len(live)+len(cp.AbsentContexts)+len(cp.RetiredContexts) != int(want) {
		return 0, fmt.Errorf("incomplete decision snapshot history")
	}
	seen := map[uint32]bool{}
	for _, n := range cp.AbsentContexts {
		if n == 0 || uint64(n) >= want || seen[n] || !unboundDecision(run, n) {
			return 0, fmt.Errorf("unproven unbound decision")
		}
		seen[n] = true
		key := cp.Binding.Key
		key.Decision = uint64(n)
		if _, err := store.Read(ctx, key); err != contextassembly.Missing {
			return 0, fmt.Errorf("unbound context is present or unavailable")
		}
	}
	for _, n := range cp.RetiredContexts {
		if n == 0 || uint64(n) >= want || seen[n] || run.Actions.Decisions[n-1].Number != n {
			return 0, fmt.Errorf("invalid retired context history")
		}
		seen[n] = true
		key := cp.Binding.Key
		key.Decision = uint64(n)
		if err := store.VerifyCheckpoint(ctx, key); err != contextassembly.Invalidated {
			return 0, fmt.Errorf("context retirement not confirmed")
		}
	}
	for _, n := range live {
		if n == 0 || uint64(n) > want || seen[n] {
			return 0, fmt.Errorf("invalid decision snapshot number")
		}
		seen[n] = true
		key := cp.Binding.Key
		key.Decision = uint64(n)
		var err error
		if cp.Format == 1 {
			err = store.BindCheckpoint(ctx, key, digests[n])
		} else {
			err = store.VerifyCheckpoint(ctx, key)
		}
		if err == contextassembly.Invalidated && (uint64(n) < want || maintenance) {
			if err = store.VerifyCheckpoint(ctx, key); err == contextassembly.Invalidated {
				continue
			}
		}
		if err != nil {
			return 0, err
		}
	}
	return len(live) + len(cp.RetiredContexts), nil
}
