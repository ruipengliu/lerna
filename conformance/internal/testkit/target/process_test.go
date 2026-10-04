package target_test

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	process "github.com/ruipengliu/lerna/conformance/internal/testkit/process"
	"github.com/ruipengliu/lerna/conformance/internal/testkit/target"
)

type targetProcessConfig struct {
	Path, Identity, Scenario, Gate, Event string
	Generation                            int
	Now                                   time.Time
}
type targetProcessFrame struct {
	Scenario, Stage, Event string
	Generation             int
	Settings               *target.Settings `json:",omitempty"`
	Result                 *target.Event    `json:",omitempty"`
	Error                  string           `json:",omitempty"`
}

func TestIndependentTargetProcess(t *testing.T) {
	if os.Getenv("LERNA_INDEPENDENT_TARGET_PROCESS") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	pipes, err := process.OpenInherited()
	if pipes != nil {
		defer func() {
			if e := pipes.Close(); e != nil {
				t.Error(e)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	var cfg targetProcessConfig
	if err = pipes.Receive(ctx, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Scenario == "" || cfg.Generation < 1 || cfg.Now.IsZero() || cfg.Event == "" {
		t.Fatal("invalid finite target child configuration")
	}
	writer, err := target.Open(ctx, target.Config{Path: cfg.Path, Identity: cfg.Identity, Window: time.Minute, QueryMode: target.QueryEnabled, IOTimeout: 2 * time.Second, BusyTimeout: 10 * time.Millisecond, Now: func() time.Time { return cfg.Now }})
	if writer != nil {
		defer func() {
			if e := writer.Close(); e != nil {
				t.Error(e)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	frame := func(stage string) targetProcessFrame {
		return targetProcessFrame{Scenario: cfg.Scenario, Stage: stage, Event: cfg.Event, Generation: cfg.Generation}
	}
	if cfg.Gate != "" {
		if cfg.Gate != "before_commit" && cfg.Gate != "committed_before_reply" {
			t.Fatal("unsupported private target checkpoint")
		}
		target.ConfigureProcessGate(writer, func(gatectx context.Context, stage, eventID string) error {
			if stage != cfg.Gate {
				return nil
			}
			if eventID != cfg.Event {
				return errors.New("target gate event identity mismatch")
			}
			if err := pipes.Emit(gatectx, frame(stage)); err != nil {
				return err
			}
			var release targetProcessFrame
			if err := pipes.Receive(gatectx, &release); err != nil {
				return err
			}
			if release.Stage != "release" || release.Scenario != cfg.Scenario || release.Event != cfg.Event || release.Generation != cfg.Generation {
				return errors.New("wrong target checkpoint release")
			}
			return nil
		})
	}
	settings, err := writer.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ready := frame("configured")
	ready.Settings = &settings
	if err = pipes.Emit(ctx, ready); err != nil {
		t.Fatal(err)
	}
	var command targetProcessFrame
	if err = pipes.Receive(ctx, &command); err != nil {
		t.Fatal(err)
	}
	if command.Scenario != cfg.Scenario || command.Event != cfg.Event || command.Generation != cfg.Generation || command.Stage != "run" {
		t.Fatal("wrong target child run identity")
	}
	event, err := writer.RunEvent(ctx, cfg.Scenario, cfg.Event)
	reply := frame("target_reply")
	reply.Result = &event
	if err != nil {
		if !errors.Is(err, target.ErrResponseLost) {
			t.Fatal(err)
		}
		reply.Error = "response_lost"
	}
	if err = pipes.Respond(ctx, reply); err != nil {
		t.Fatal(err)
	}
}

func startTargetChild(t *testing.T, f *fixture, scenario, event, gate string, generation int) *process.Child {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 12*time.Second)
	f.closers = append(f.closers, func() error { cancel(); return nil })
	child, err := process.New(ctx, "TestIndependentTargetProcess", "LERNA_INDEPENDENT_TARGET_PROCESS=1")
	if child != nil {
		f.children = append(f.children, child)
	} // Before even Start.
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	if err = child.Send(ctx, targetProcessConfig{Path: f.cfg.Path, Identity: f.cfg.Identity, Scenario: scenario, Event: event, Gate: gate, Generation: generation, Now: f.now}); err != nil {
		t.Fatal(err)
	}
	var ready targetProcessFrame
	if err = child.Event(ctx, &ready); err != nil {
		t.Fatal(err)
	}
	if ready.Stage != "configured" || ready.Scenario != scenario || ready.Generation != generation || ready.Settings == nil || ready.Settings.JournalMode != "wal" || ready.Settings.Synchronous != 2 || ready.Settings.ForeignKeys != 1 || len(ready.Settings.Migrations) != 2 {
		t.Fatal("target child configuration/actual settings mismatch")
	}
	t.Logf("target process scenario=%s event=%s generation=%d SQLite=%s DB=%s migrations=%v", scenario, event, generation, ready.Settings.SQLiteVersion, ready.Settings.DatabaseID, ready.Settings.Migrations)
	if err = child.Send(ctx, targetProcessFrame{Scenario: scenario, Event: event, Generation: generation, Stage: "run"}); err != nil {
		t.Fatal(err)
	}
	return child
}

func finishTargetChild(t *testing.T, f *fixture, child *process.Child, scenario, event string, generation int) target.Event {
	t.Helper()
	var reply targetProcessFrame
	if err := child.Reply(f.ctx, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Stage != "target_reply" || reply.Scenario != scenario || reply.Event != event || reply.Generation != generation || reply.Result == nil || reply.Error != "" {
		t.Fatal("normal target reply mismatch")
	}
	confirmed, err := child.Wait(f.ctx)
	if !confirmed || err != nil {
		t.Fatal("normal target child exit:", err)
	}
	confirmed, err = child.Stop(f.ctx)
	if !confirmed || err != nil {
		t.Fatal("normal target physical cleanup:", err)
	}
	return *reply.Result
}

func TestTargetNormalChildCommitsOriginalPlanAndFact(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	input := target.Request{Key: "original-process-key", Resource: "fake-process-document", Data: []byte{0, 255, 10}}
	if _, err := writer.InstallPlan(f.ctx, target.Plan{ID: "process-normal", Seed: 73, Deadline: f.now.Add(3 * time.Minute), Steps: []target.Step{{ID: "write-1", Kind: target.WriteNormally, Input: input}}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.writer = nil
	child := startTargetChild(t, f, "process-normal", "write-1", "", 1)
	event := finishTargetChild(t, f, child, "process-normal", "write-1", 1)
	if event.Phase != "committed" {
		t.Fatalf("normal child event: %+v", event)
	}
	writer = f.open()
	assertProcessTargetFact(t, f, writer, input, "process-normal", 1)
}

func assertProcessTargetFact(t *testing.T, f *fixture, writer *target.Target, input target.Request, scenario string, cursor int) target.Fact {
	t.Helper()
	query, err := writer.Query(f.ctx, input.Key)
	if err != nil || query.Value.Version != 1 || string(query.Value.Data) != string(input.Data) {
		t.Fatal("ordinary original query mismatch", err)
	}
	value, err := writer.Read(f.ctx, input.Resource)
	if err != nil || value.Version != 1 || string(value.Data) != string(input.Data) {
		t.Fatal("ordinary value mismatch", err)
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	fact, err := observer.Observe(f.ctx, input.Key)
	if err != nil || fact.Pending || fact.Receipt.Key != input.Key || len(fact.Receives) != 1 || fact.Value.Version != 1 || string(fact.Value.Data) != string(input.Data) {
		t.Fatal("independent target fact mismatch", err)
	}
	plan, err := observer.Plan(f.ctx, scenario)
	if err != nil || plan.Cursor != cursor || plan.Plan.Seed != 73 {
		t.Fatal("durable plan cursor mismatch", err)
	}
	return fact
}

func TestTargetSIGKILLBeforeCommitRollsBackFactAndCursor(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	input := target.Request{Key: "original-process-key", Resource: "fake-process-document", Data: []byte{0, 255, 10}}
	if _, err := writer.InstallPlan(f.ctx, target.Plan{ID: "process-before", Seed: 73, Deadline: f.now.Add(3 * time.Minute), Steps: []target.Step{{ID: "write-1", Kind: target.WriteNormally, Input: input}}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.writer = nil
	child := startTargetChild(t, f, "process-before", "write-1", "before_commit", 1)
	var gate targetProcessFrame
	if err := child.Event(f.ctx, &gate); err != nil {
		t.Fatal("actual target pre-COMMIT checkpoint missing:", err)
	}
	if gate.Stage != "before_commit" || gate.Scenario != "process-before" || gate.Event != "write-1" || gate.Generation != 1 {
		t.Fatal("wrong target pre-COMMIT identity")
	}
	killTargetChild(t, f, child)
	writer = f.open()
	if _, err := writer.Query(f.ctx, input.Key); !errors.Is(err, target.ErrNotFound) {
		t.Fatal("pre-COMMIT original query unexpectedly committed", err)
	}
	if _, err := writer.Read(f.ctx, input.Resource); !errors.Is(err, target.ErrNotFound) {
		t.Fatal("pre-COMMIT value unexpectedly committed", err)
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = observer.Observe(f.ctx, input.Key); !errors.Is(err, target.ErrNotFound) {
		t.Fatal("pre-COMMIT receipt/receive unexpectedly durable", err)
	}
	plan, err := observer.Plan(f.ctx, "process-before")
	if err != nil || plan.Cursor != 0 || len(plan.Events) != 0 {
		t.Fatal("pre-COMMIT cursor unexpectedly advanced", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.writer = nil
	// A fresh child/coordinator resumes the saved original event.
	successor := startTargetChild(t, f, plan.Plan.ID, plan.Plan.Steps[plan.Cursor].ID, "", 2)
	finishTargetChild(t, f, successor, "process-before", "write-1", 2)
	writer = f.open()
	assertProcessTargetFact(t, f, writer, input, "process-before", 1)
}

func killTargetChild(t *testing.T, f *fixture, child *process.Child) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
	defer cancel()
	if err := child.KillWait(ctx); err != nil {
		t.Fatal("target SIGKILL not confirmed:", err)
	}
	var reply targetProcessFrame
	if err := child.Reply(ctx, &reply); err != io.EOF {
		t.Fatal("target replied despite held checkpoint", err)
	}
	confirmed, err := child.Stop(ctx)
	if !confirmed || err != nil {
		t.Fatal("killed target physical closure unconfirmed:", err)
	}
}
