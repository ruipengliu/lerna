package answer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"lerna/adapters/contentpolicy"
	"lerna/answers"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/internal/randomid"
	"lerna/tasks"
	"os"
	"path/filepath"
	"time"
)

// This private reference-host checkpoint contains credentials and exact read
// material, not a duplicate of Memory content. It must stay on authorized local
// storage. Restore never bootstraps policy, writes Memory or issues a new grant.
type answerContextCheckpoint struct {
	fileImage                 []byte
	MissingRetainUntil        int64
	ReferenceOnly             bool
	Format                    int
	Token, Location           string
	Baseline                  tasks.RunSnapshot
	KeyDER                    []byte
	ReadID, Material, GrantID string
	Rule                      contentpolicy.Rule
	SnapshotSHA256            *[32]byte `json:",omitempty"`
}
type AnswerRecoveryReport struct {
	ReferenceOnly                  bool
	UsedRequests, ReservedRequests uint32
	ModelCalls                     int
	Answer                         string
	Restored, Published            bool
	ReadAllocated                  uint64
	ValidationError                string
}

func contextKey(c *answerContextCheckpoint) contextassembly.Key {
	return contextassembly.Key{Namespace: c.Baseline.Task.Ref.Namespace, TaskID: c.Baseline.Task.Ref.TaskID, Decision: 1}
}

// CheckpointPersonalizedAnswer is a subprocess-only crash probe. It exits 73
// after syncing the checkpoint, before any deferred database cleanup runs.
func CheckpointPersonalizedAnswer(ctx context.Context, root string) error {
	return CheckpointPersonalizedAnswerAt(ctx, root, "ready")
}

// CheckpointPersonalizedAnswerAt is a subprocess-only probe; successful commit
// exits 73 without cleanup at the requested decision boundary.
func CheckpointPersonalizedAnswerAt(ctx context.Context, root, point string) error {
	if point == "" {
		point = "ready"
	}
	if point != "ready" && point != "dispatched" && point != "output" && point != "reference" && point != "missing" {
		return fmt.Errorf("unknown checkpoint boundary")
	}

	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, e := open(ctx, root, "", "test-model-location", l)
	if e != nil {
		return e
	}
	defer h.close()
	s, e := h.submit(ctx, l)
	if e != nil {
		return e
	}
	s, e = start(ctx, h, s)
	if e != nil {
		return e
	}
	stopRenewal := keepAnswerRecoveryLease(ctx, h, tasks.QualificationOf(s))
	defer stopRenewal()
	style := "concise"
	if point == "missing" {
		style = "missing"
	}
	if point == "reference" {
		style = "detailed"
	}
	p, e := h.personalizeBound(ctx, s, style, true, nil, point == "reference")
	if e != nil {
		return e
	}
	defer p.close()
	if _, e = p.session.Assemble(ctx, s.Task, h.location, brain.MaxInputBytes); e != nil {
		return e
	}
	snapshot, e := p.snapshots.Read(ctx, contextKey(p.checkpoint))
	if e != nil {
		return e
	}
	if p.checkpoint.ReferenceOnly && bytes.Contains(snapshot.Document, []byte("detailed")) {
		return fmt.Errorf("reference checkpoint retained Memory body")
	}
	if e = p.snapshots.BindCheckpoint(ctx, contextKey(p.checkpoint), sha256.Sum256(snapshot.Document)); e != nil {
		return e
	}
	p.checkpoint.Format = 2
	p.checkpoint.SnapshotSHA256 = nil
	if point == "dispatched" {
		if e = h.generation.BeginRequest(ctx, tasks.QualificationOf(s), 0); e != nil {
			return e
		}
	} else if point == "output" {
		b, e := brain.NewAnswer(&personalizedModel{}, p.session, h.access, h.generation, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: brain.MaxAnswerBytes, SettlementTimeout: time.Second})
		if e != nil {
			return e
		}
		proposal, e := b.Decide(ctx, tasks.DecisionInput{Task: s.Task, Work: s.Work[0], Generation: s.Generations[0]})
		if e != nil {
			return e
		}
		if proposal.Result == "" {
			return fmt.Errorf("output checkpoint has no saved answer")
		}
	}
	renewed, e := h.generation.Commit(ctx, tasks.WorkChange{Kind: "renew", ChangeID: "context-checkpoint-renew", Qualification: tasks.QualificationOf(s)})
	if e != nil {
		return e
	}
	p.checkpoint.Baseline = renewed
	data, e := json.Marshal(p.checkpoint)
	if e != nil {
		return e
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("checkpoint capacity")
	}
	file, e := os.OpenFile(filepath.Join(root, "personalized-checkpoint.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	dir, e := os.Open(root)
	if e != nil {
		return e
	}
	defer dir.Close()
	if e = dir.Sync(); e != nil {
		return e
	}
	os.Exit(73)
	return nil
}
func readAnswerCheckpoint(root string) (*answerContextCheckpoint, error) {
	path := filepath.Join(root, "personalized-checkpoint.json")
	stat, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 || stat.Size() > 1<<20 {
		return nil, fmt.Errorf("private bounded checkpoint required")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil {
		return nil, e
	}
	if len(raw) > 1<<20 {
		return nil, fmt.Errorf("checkpoint capacity")
	}
	var cp answerContextCheckpoint
	if json.Unmarshal(raw, &cp) != nil || ((cp.Format != 1 && cp.Format != 2) || (cp.Format == 1 && cp.SnapshotSHA256 == nil) || (cp.Format == 2 && cp.SnapshotSHA256 != nil)) || len(cp.Baseline.Work) != 1 || len(cp.Baseline.Generations) != 1 || cp.Baseline.Task.Ref.Namespace != "local" || cp.Token == "" || cp.Location == "" || len(cp.KeyDER) > 4096 || len(cp.KeyDER) == 0 || cp.ReadID == "" || len(cp.Material) > 65536 || cp.Material == "" || cp.GrantID == "" {
		return nil, fmt.Errorf("invalid recovery checkpoint")
	}
	cp.fileImage = raw
	return &cp, nil
}
func RestorePersonalizedAnswer(ctx context.Context, root string, revoke bool) (AnswerRecoveryReport, error) {
	report := AnswerRecoveryReport{}
	cp, e := readAnswerCheckpoint(root)
	if e != nil {
		return report, e
	}
	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, e := open(ctx, root, cp.Token, cp.Location, l)
	if e != nil {
		return report, e
	}
	defer h.close()
	// Renewal succeeds only for still-current original worker qualification.
	// An expired/replaced worker cannot use this path to invent a new decision.
	if _, e = h.generation.Commit(ctx, tasks.WorkChange{Kind: "renew", ChangeID: "context-recovery-renew", Qualification: tasks.QualificationOf(cp.Baseline)}); e != nil {
		// Report the rejected attempt using current durable accounting. Do not
		// initialize Memory bindings, issue reads, or take over this generation.
		current, loadErr := h.core.Load(ctx, cp.Baseline.Task.Ref)
		if loadErr != nil {
			return report, loadErr
		}
		if maintenanceErr := maintainAnswerCheckpoint(ctx, root, cp); maintenanceErr != nil {
			return report, fmt.Errorf("checkpoint maintenance: %w", maintenanceErr)
		}
		report.ValidationError = e.Error()
		report.UsedRequests = current.Task.ModelUsedRequests
		report.ReservedRequests = current.Task.ModelReservedRequests
		return report, nil
	}
	stopRenewal := keepAnswerRecoveryLease(ctx, h, tasks.QualificationOf(cp.Baseline))
	defer stopRenewal()
	p, e := h.personalizeBound(ctx, cp.Baseline, "", false, cp, cp.ReferenceOnly)
	if e != nil {
		return report, e
	}
	defer p.close()
	cp, e = migrateAnswerCheckpoint(ctx, root, p.snapshots, contextKey(cp), cp)
	if e != nil {
		return report, e
	}
	p.checkpoint = cp
	snapshot, e := p.snapshots.Read(ctx, contextKey(cp))
	if e != nil {
		return report, e
	}
	if e = verifyAnswerCheckpoint(ctx, p.snapshots, cp); e != nil {
		return report, fmt.Errorf("original snapshot integrity: %w", e)
	}
	report.Restored = true
	report.ReferenceOnly = cp.ReferenceOnly
	if cp.ReferenceOnly && bytes.Contains(snapshot.Document, []byte("detailed")) {
		return report, fmt.Errorf("reference checkpoint retained Memory body")
	}
	if revoke {
		if e = p.change(ctx, h, "revoke"); e != nil {
			return report, e
		}
	}
	s := cp.Baseline
	currentDecision, e := h.core.Load(ctx, s.Task.Ref)
	if e != nil {
		return report, e
	}
	if len(currentDecision.Generations) != 1 || currentDecision.Generations[0].OutputOperation != s.Generations[0].OutputOperation {
		return report, fmt.Errorf("original generation changed")
	}
	var validation error
	if currentDecision.Generations[0].Started != 0 || currentDecision.Generations[0].Settled {
		_, validation = h.port.Recover(ctx, s.Task.Ref)
	} else {
		model := &controlledModel{fn: func(ctx context.Context, req brain.Request) (brain.Result, error) {
			report.ModelCalls++
			return (&personalizedModel{}).Generate(ctx, req)
		}}
		b, e := brain.NewAnswer(model, p.session, h.access, h.generation, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: brain.MaxAnswerBytes, SettlementTimeout: time.Second})
		if e != nil {
			return report, e
		}
		proposal, e := b.Decide(ctx, tasks.DecisionInput{Task: s.Task, Work: s.Work[0], Generation: s.Generations[0]})
		validation = e
		if validation == nil {
			_, validation = h.port.Commit(ctx, tasks.WorkChange{Kind: "complete", ChangeID: "recovered-context-publication", Qualification: tasks.QualificationOf(s), Finished: true, Proposal: proposal})
		}
	}
	if validation != nil {
		report.ValidationError = validation.Error()
	}
	current, e := h.core.Load(ctx, s.Task.Ref)
	if e != nil {
		return report, e
	}
	report.UsedRequests = current.Task.ModelUsedRequests
	report.ReservedRequests = current.Task.ModelReservedRequests
	report.Published = current.Task.State == "COMPLETED" && current.Task.Result != ""
	if report.Published {
		published, e := answers.Query(ctx, h.client, s.Task.Ref, h.content, h.binding(), "task")
		if e != nil {
			return report, e
		}
		var answer brain.Answer
		if published.Availability != "available" || json.Unmarshal(published.Body, &answer) != nil {
			return report, fmt.Errorf("restored published output unavailable")
		}
		report.Answer = answer.Text
	}
	grant, e := p.grants.Get(ctx, h.token, p.grantID)
	if e != nil {
		return report, e
	}
	report.ReadAllocated = grant.Allocated
	e = verifyAnswerCheckpoint(ctx, p.snapshots, cp)
	if e == contextassembly.Invalidated && validation != nil && !report.Published {
		// Cleanup may retire the rejected decision while original usage and
		// publication outcome are being reconciled. No body is needed here.
		return report, nil
	}
	if e != nil {
		return report, e
	}
	return report, nil
}

// The initial renewal above must succeed before starting this loop. It renews
// only the original qualification and stops on failure; every subsequent Core
// operation still enforces current ownership, lease and control state.
func keepAnswerRecoveryLease(parent context.Context, h *harness, q tasks.Qualification) func() {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(limits().RenewEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			id, err := randomid.New()
			if err != nil {
				return
			}
			call, stop := context.WithTimeout(ctx, limits().IOTimeout)
			_, err = h.generation.Commit(call, tasks.WorkChange{Kind: "renew", ChangeID: id, Qualification: q})
			stop()
			if err != nil {
				return
			}
		}
	}()
	return func() { cancel(); <-done }
}

func verifyAnswerCheckpoint(ctx context.Context, store contextassembly.CheckpointStore, cp *answerContextCheckpoint) error {
	// Legacy files still supply their original comparison; never initialize a
	// missing format-2 proof by hashing whatever a restored store happens to hold.
	if cp.Format == 1 && cp.SnapshotSHA256 != nil {
		return store.BindCheckpoint(ctx, contextKey(cp), *cp.SnapshotSHA256)
	}
	return store.VerifyCheckpoint(ctx, contextKey(cp))
}
