package tasks_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	sqliteauth "lerna/adapters/authorization/sqlite"
	"lerna/authorization"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type handoffProbe struct {
	SourcePath, TargetPath, SourceToken, TargetToken, Step, Output string
	SourceKey, TargetKey                                           []byte
	Now                                                            time.Time
	Request                                                        tasks.HandoffRequest
	Envelope                                                       tasks.HandoffEnvelope
}

func TestHandoffProcessProbe(t *testing.T) {
	path := os.Getenv("HARNESS_HANDOFF_PROBE")
	if path == "" {
		t.Skip("subprocess only")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var in handoffProbe
	if e = json.Unmarshal(raw, &in); e != nil {
		t.Fatal(e)
	}
	dbPath, token, owner, key, peer, peerKey := in.SourcePath, in.SourceToken, "local-owner", in.SourceKey, "target-owner", in.TargetKey
	if in.Step == "prepare" || in.Step == "activate" {
		dbPath, token, owner, key, peer, peerKey = in.TargetPath, in.TargetToken, "target-owner", in.TargetKey, "local-owner", in.SourceKey
	}
	db, e := sqliteauth.Open(dbPath)
	if e != nil {
		t.Fatal(e)
	}
	a, e := authorization.New(db, &clock{in.Now}, authConfig())
	if e != nil {
		t.Fatal(e)
	}
	s, e := tasks.New(a, tasks.Config{Namespace: "local", Resource: "root", Owner: owner, MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	cfg := publicHandoffConfig()
	cfg.PrivateKey = key
	cfg.Peers = map[string]ed25519.PublicKey{peer: ed25519.PrivateKey(peerKey).Public().(ed25519.PublicKey)}
	h, e := s.Handoffs(token, cfg)
	if e != nil {
		t.Fatal(e)
	}
	var out any
	ctx := context.Background()
	switch in.Step {
	case "begin":
		out, e = h.Begin(ctx, in.Request)
	case "export":
		out, e = h.Export(ctx, in.Request.Ref, in.Request.OperationID)
	case "prepare":
		out, e = h.Prepare(ctx, in.Envelope)
	case "seal":
		out, e = h.Seal(ctx, in.Envelope)
	case "activate":
		out, e = h.Activate(ctx, in.Envelope)
	default:
		t.Fatal("invalid step")
	}
	if e != nil {
		t.Fatal(e)
	}
	raw, e = json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(in.Output, raw, 0600); e != nil {
		t.Fatal(e)
	}
	// Probe output is diagnostic; no application response or clean shutdown.
	os.Exit(80)
}
func handoffCrash(t *testing.T, in handoffProbe) []byte {
	t.Helper()
	dir := t.TempDir()
	in.Output = filepath.Join(dir, "diagnostic.json")
	raw, e := json.Marshal(in)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "input.json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHandoffProcessProbe$", "-test.v")
	cmd.Env = append(os.Environ(), "HARNESS_HANDOFF_PROBE="+path)
	output, e := cmd.CombinedOutput()
	exit, ok := e.(*exec.ExitError)
	if !ok || exit.ExitCode() != 80 {
		t.Fatalf("step %s exit: %v %s", in.Step, e, output)
	}
	t.Logf("step=%s pid=%d exit=80", in.Step, cmd.ProcessState.Pid())
	raw, e = os.ReadFile(in.Output)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestHandoffEveryDurableWindowSurvivesExit(t *testing.T) {
	s, a, token, c, sourcePath := setup(t)
	enableHandoff(t, a, token, c)
	_, ta, tt, tc, targetPath := setup(t)
	enableHandoff(t, ta, tt, tc)
	target, e := tasks.New(ta, tasks.Config{Namespace: "local", Resource: "root", Owner: "target-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	sub := submission(t, a, token, c)
	task, e := s.Submit(ctx, token, sub)
	if e != nil {
		t.Fatal(e)
	}
	op, _ := a.NewOperation(ctx, token)
	in := handoffProbe{SourcePath: sourcePath, TargetPath: targetPath, SourceToken: token, TargetToken: tt, SourceKey: publicHandoffConfig().PrivateKey, TargetKey: publicHandoffConfig().PrivateKey, Now: c.now, Request: tasks.HandoffRequest{OperationID: op, Ref: task.Ref, Target: "target-owner", ExpectedVersion: task.Version}}
	for _, step := range []string{"begin", "export", "prepare", "seal", "activate"} {
		in.Step = step
		handoffCrash(t, in)        // result unavailable to the application
		raw := handoffCrash(t, in) // recover using exactly the original request
		if step == "export" || step == "prepare" || step == "seal" {
			if e = json.Unmarshal(raw, &in.Envelope); e != nil {
				t.Fatal(e)
			}
		}
		old, e := s.Load(ctx, task.Ref)
		if e != nil {
			t.Fatal(e)
		}
		_, e = workerPort(t, s, token, "source").Commit(ctx, tasks.WorkChange{Kind: "claim", ChangeID: "forbidden-" + step, Qualification: tasks.QualificationOf(old)})
		if !authorization.Is(e, authorization.Unavailable) {
			t.Fatal("old owner runnable", step, e)
		}
		if step != "activate" {
			if _, e = target.Load(ctx, task.Ref); !authorization.Is(e, authorization.NotFound) {
				t.Fatal("target runnable before activation", step, e)
			}
		}
	}
	r, e := target.Load(ctx, task.Ref)
	if e != nil || r.Task.OwnerEpoch != 2 {
		t.Fatal(r, e)
	}
	if got, e := target.Submit(ctx, tt, sub); e != nil || got.Ref != task.Ref {
		t.Fatal("original admission after process recovery", e)
	}
	if _, e = workerPort(t, target, tt, "target").Commit(ctx, tasks.WorkChange{Kind: "claim", ChangeID: "activated", Qualification: tasks.QualificationOf(r)}); e != nil {
		t.Fatal(e)
	}
}

type uncertainHandoffStore struct {
	back      tasks.RuntimeStore
	remaining int
	after     bool
}

func (s *uncertainHandoffStore) UpdateRuntime(ctx context.Context, fn func(authorization.RuntimeTransaction) error) error {
	fail := false
	if s.remaining > 0 {
		s.remaining--
		fail = s.remaining == 0
	}
	e := s.back.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error {
		if e := fn(tx); e != nil {
			return e
		}
		if fail && !s.after {
			return context.DeadlineExceeded
		}
		return nil
	})
	if e == nil && fail && s.after {
		return context.DeadlineExceeded
	}
	return e
}
func TestHandoffUnknownAndFailedCommits(t *testing.T) {
	s, a, token, c, _ := setup(t)
	enableHandoff(t, a, token, c)
	_, ta, tt, tc, _ := setup(t)
	enableHandoff(t, ta, tt, tc)
	sw := &uncertainHandoffStore{back: a}
	tw := &uncertainHandoffStore{back: ta}
	source, e := tasks.New(sw, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	target, e := tasks.New(tw, tasks.Config{Namespace: "local", Resource: "root", Owner: "target-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		t.Fatal(e)
	}
	sk, tk := publicHandoffConfig().PrivateKey, publicHandoffConfig().PrivateKey
	cfg := publicHandoffConfig()
	cfg.PrivateKey = sk
	cfg.Peers = map[string]ed25519.PublicKey{"target-owner": tk.Public().(ed25519.PublicKey)}
	sh, _ := source.Handoffs(token, cfg)
	cfg.PrivateKey = tk
	cfg.Peers = map[string]ed25519.PublicKey{"local-owner": sk.Public().(ed25519.PublicKey)}
	th, _ := target.Handoffs(tt, cfg)
	ctx := context.Background()
	task, e := s.Submit(ctx, token, submission(t, a, token, c))
	if e != nil {
		t.Fatal(e)
	}
	op, _ := a.NewOperation(ctx, token)
	_, e = sh.Begin(ctx, tasks.HandoffRequest{OperationID: op, Ref: task.Ref, Target: "target-owner", ExpectedVersion: task.Version})
	if e != nil {
		t.Fatal(e)
	}
	offer, e := sh.Export(ctx, task.Ref, op)
	if e != nil {
		t.Fatal(e)
	}
	ack, e := th.Prepare(ctx, offer)
	if e != nil {
		t.Fatal(e)
	}
	sw.remaining = 2
	if _, e = sh.Seal(ctx, ack); e != context.DeadlineExceeded {
		t.Fatal(e)
	}
	h, e := sh.Status(ctx, task.Ref, op)
	if e != nil || h.Phase != "PREPARING" || len(h.Activation.Body) != 0 {
		t.Fatal("failed seal published activation", h, e)
	}
	sw.remaining = 2
	sw.after = true
	if _, e = sh.Seal(ctx, ack); e != context.DeadlineExceeded {
		t.Fatal(e)
	}
	h, e = sh.Status(ctx, task.Ref, op)
	if e != nil || h.Phase != "SEALED" {
		t.Fatal("unknown seal lost", h, e)
	}
	activation, e := sh.Seal(ctx, ack)
	if e != nil {
		t.Fatal(e)
	}
	tw.remaining = 2
	if _, e = th.Activate(ctx, activation); e != context.DeadlineExceeded {
		t.Fatal(e)
	}
	if _, e = target.Load(ctx, task.Ref); !authorization.Is(e, authorization.NotFound) {
		t.Fatal("failed activation created task", e)
	}
	tw.remaining = 2
	tw.after = true
	if _, e = th.Activate(ctx, activation); e != context.DeadlineExceeded {
		t.Fatal(e)
	}
	h, e = th.Activate(ctx, activation)
	if e != nil || h.Phase != "ACTIVE" {
		t.Fatal(h, e)
	}
	r, e := target.Load(ctx, task.Ref)
	if e != nil || r.Task.OwnerEpoch != 2 {
		t.Fatal("unknown activation incremented twice", e)
	}
}
