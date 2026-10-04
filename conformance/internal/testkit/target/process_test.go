package target_test

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
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
	return finishTargetChildOutcome(t, f, child, scenario, event, generation, "")
}

func finishTargetChildOutcome(t *testing.T, f *fixture, child *process.Child, scenario, event string, generation int, expectedError string) target.Event {
	t.Helper()
	var reply targetProcessFrame
	if err := child.Reply(f.ctx, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Stage != "target_reply" || reply.Scenario != scenario || reply.Event != event || reply.Generation != generation || reply.Result == nil || reply.Error != expectedError {
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

func TestTargetIsolatedSameSeedReplaysFiniteNormalAndLossEvents(t *testing.T) {
	steps := []target.Step{
		{ID: "normal-original", Kind: target.WriteNormally, Input: target.Request{Key: "normal-key", Resource: "fake-seed-document", Data: []byte{0, 1}}},
		{ID: "loss-original", Kind: target.DropResponse, Input: target.Request{Key: "lost-key", Resource: "fake-seed-document", Data: []byte{255, 10}}},
	}
	var originalState target.PlanState
	var originalID string
	for index, scenario := range []string{"seed-original", "seed-isolated"} {
		f := newFixture(t)
		writer := f.open()
		plan := target.Plan{ID: scenario, Seed: 73, Deadline: f.now.Add(3 * time.Minute), Steps: steps}
		if _, err := writer.InstallPlan(f.ctx, plan); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		f.writer = nil
		observer, err := f.observer()
		if err != nil {
			t.Fatal(err)
		}
		for position := range steps {
			state, err := observer.Plan(f.ctx, scenario)
			if err != nil || state.Cursor != position || state.Plan.Seed != 73 {
				t.Fatal("original finite scenario cursor unavailable", err)
			}
			step := state.Plan.Steps[state.Cursor]
			child := startTargetChild(t, f, scenario, step.ID, "", position+1)
			expectedError := ""
			if step.Kind == target.DropResponse {
				expectedError = "response_lost"
			}
			event := finishTargetChildOutcome(t, f, child, scenario, step.ID, position+1, expectedError)
			if event.Step != position || event.ID != steps[position].ID || event.Kind != steps[position].Kind {
				t.Fatal("new coordinator changed original event identity/order")
			}
			if position == 0 && (event.Phase != "committed" || event.Outcome != "applied") || position == 1 && (event.Phase != "committed_response_lost" || event.Outcome != "response_lost") {
				t.Fatal("wrong normal or explicitly planned response loss outcome", event)
			}
		}
		writer = f.open()
		for position, step := range steps {
			query, err := writer.Query(f.ctx, step.Input.Key)
			if err != nil || query.Value.Version != int64(position+1) || string(query.Value.Data) != string(step.Input.Data) {
				t.Fatal("ordinary original processing fact changed during finite replay", err)
			}
			fact, err := observer.Observe(f.ctx, step.Input.Key)
			if err != nil || fact.Pending || len(fact.Receives) != 1 || fact.Value.Version != int64(position+1) {
				t.Fatal("isolated scenario did not actually receive/apply each original once", err)
			}
		}
		value, err := writer.Read(f.ctx, steps[1].Input.Resource)
		if err != nil || value.Version != 2 || string(value.Data) != string([]byte{255, 10}) {
			t.Fatal("independent finite scenario current value mismatch", err)
		}
		state, err := observer.Plan(f.ctx, scenario)
		if err != nil || state.Cursor != 2 || len(state.Events) != 2 || !reflect.DeepEqual(state.Plan, plan) {
			t.Fatal("isolated scenario lost fixed plan/cursor/event history", err)
		}
		settings, err := writer.Settings(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			originalState, originalID = state, settings.DatabaseID
		} else {
			// Logical replay compares exact saved steps, events and cursor. It does
			// not reset the first scenario or equate independent physical identity.
			if settings.DatabaseID == originalID || state.Plan.ID == originalState.Plan.ID || state.Plan.Seed != originalState.Plan.Seed || !reflect.DeepEqual(state.Plan.Steps, originalState.Plan.Steps) || !reflect.DeepEqual(state.Events, originalState.Events) || state.Cursor != originalState.Cursor {
				t.Fatal("sameSeed replay aliased original identity or changed finite definition")
			}
		}
	}
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

func TestTargetSIGKILLAfterCommitRetainsOriginalFactAndReplayCursor(t *testing.T) {
	for _, kill := range []bool{false, true} {
		name := "normal_release"
		if kill {
			name = "SIGKILL_response_unknown"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			writer := f.open()
			input := target.Request{Key: "original-process-key", Resource: "fake-process-document", Data: []byte{0, 255, 10}}
			if _, err := writer.InstallPlan(f.ctx, target.Plan{ID: "process-after", Seed: 73, Deadline: f.now.Add(3 * time.Minute), Steps: []target.Step{{ID: "write-1", Kind: target.WriteNormally, Input: input}}}); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			f.writer = nil
			child := startTargetChild(t, f, "process-after", "write-1", "committed_before_reply", 1)
			var gate targetProcessFrame
			if err := child.Event(f.ctx, &gate); err != nil {
				t.Fatal("actual target post-COMMIT checkpoint missing:", err)
			}
			if gate.Stage != "committed_before_reply" || gate.Scenario != "process-after" || gate.Event != "write-1" || gate.Generation != 1 {
				t.Fatal("wrong target post-COMMIT identity")
			}
			if kill {
				killTargetChild(t, f, child)
			} else {
				if err := child.Send(f.ctx, targetProcessFrame{Scenario: "process-after", Event: "write-1", Generation: 1, Stage: "release"}); err != nil {
					t.Fatal(err)
				}
				finishTargetChild(t, f, child, "process-after", "write-1", 1)
			}
			writer = f.open()
			original := assertProcessTargetFact(t, f, writer, input, "process-after", 1)
			observer, err := f.observer()
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := observer.Plan(f.ctx, "process-after")
			if err != nil || len(persisted.Events) != 1 {
				t.Fatal("saved original event absent", err)
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			f.writer = nil
			// Fresh coordinator opens the same scenario, querying its actual cursor.
			replay := startTargetChild(t, f, persisted.Plan.ID, persisted.Events[0].ID, "", 2)
			fixed := finishTargetChild(t, f, replay, "process-after", "write-1", 2)
			if fixed != persisted.Events[0] {
				t.Fatal("completed original event changed after coordinator restart")
			}
			writer = f.open()
			after := assertProcessTargetFact(t, f, writer, input, "process-after", 1)
			if !reflect.DeepEqual(original, after) {
				t.Fatal("completed replay changed original value/window/receives/database identity")
			}
		})
	}
}

func TestTargetPendingOriginalSurvivesProcessRestartAndLateApply(t *testing.T) {
	for _, kill := range []bool{false, true} {
		name := "normal_release"
		if kill {
			name = "SIGKILL_after_durable_receive"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			writer := f.open()
			input := target.Request{Key: "pending-original-key", Resource: "fake-pending-document", Data: []byte{0, 255, 10}}
			plan := target.Plan{ID: "process-pending", Seed: 73, Deadline: f.now.Add(3 * time.Minute), Steps: []target.Step{{ID: "receive-original", Kind: target.ReceiveOnly, Input: input}, {ID: "apply-original", Kind: target.ApplyReceived, Input: input}}}
			if _, err := writer.InstallPlan(f.ctx, plan); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			f.writer = nil
			child := startTargetChild(t, f, plan.ID, "receive-original", "committed_before_reply", 1)
			var gate targetProcessFrame
			if err := child.Event(f.ctx, &gate); err != nil {
				t.Fatal(err)
			}
			if gate.Stage != "committed_before_reply" || gate.Scenario != plan.ID || gate.Event != "receive-original" || gate.Generation != 1 {
				t.Fatal("wrong original received commit identity")
			}
			observer, err := f.observer()
			if err != nil {
				t.Fatal(err)
			}
			pending, err := observer.Observe(f.ctx, input.Key)
			if err != nil || !pending.Pending || pending.Value.Version != 0 || pending.Key != input.Key || len(pending.Receives) != 1 || pending.Receives[0].Outcome != "received" {
				t.Fatal("actual durable received responsibility absent", err)
			}
			if kill {
				killTargetChild(t, f, child)
			} else {
				if err = child.Send(f.ctx, targetProcessFrame{Stage: "release", Scenario: plan.ID, Event: "receive-original", Generation: 1}); err != nil {
					t.Fatal(err)
				}
				event := finishTargetChild(t, f, child, plan.ID, "receive-original", 1)
				if event.Phase != "durably_received" {
					t.Fatal("normal receive falsely reported applied")
				}
			}
			writer = f.open()
			afterRestart, err := observer.Observe(f.ctx, input.Key)
			if err != nil || !reflect.DeepEqual(pending, afterRestart) {
				t.Fatal("process restart changed original pending/window/receive/identity", err)
			}
			state, err := observer.Plan(f.ctx, plan.ID)
			if err != nil || state.Cursor != 1 || len(state.Events) != 1 || !reflect.DeepEqual(state.Plan, plan) {
				t.Fatal("restart lost original finite plan/event/cursor", err)
			}
			// Query's current absence does not erase this received responsibility.
			if _, err = writer.Query(f.ctx, input.Key); !errors.Is(err, target.ErrNotFound) {
				t.Fatal("pending request falsely became a submitted effect", err)
			}
			if _, err = writer.Read(f.ctx, input.Resource); !errors.Is(err, target.ErrNotFound) {
				t.Fatal("pending request falsely became a resource value", err)
			}
			f.now = pending.Deadline
			if _, err = writer.Write(f.ctx, input); !errors.Is(err, target.ErrGuaranteeExpired) {
				t.Fatal("expired retransmission renewed or cancelled original", err)
			}
			if _, err = writer.Query(f.ctx, input.Key); !errors.Is(err, target.ErrNotFound) {
				t.Fatal("expired retransmission applied pending original", err)
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			f.writer = nil
			// A new coordinator chooses the original next event from saved cursor.
			apply := startTargetChild(t, f, state.Plan.ID, state.Plan.Steps[state.Cursor].ID, "", 2)
			event := finishTargetChild(t, f, apply, plan.ID, "apply-original", 2)
			if event.Phase != "committed" || event.Kind != target.ApplyReceived || event.Outcome != "applied" {
				t.Fatal("late original was not actually applied", event)
			}
			writer = f.open()
			fact, err := observer.Observe(f.ctx, input.Key)
			if err != nil || fact.Pending || fact.DatabaseID != pending.DatabaseID || fact.Value.Version != 1 || string(fact.Value.Data) != string(input.Data) || fact.Start != pending.Start || fact.Deadline != pending.Deadline || len(fact.Receives) != 2 || fact.Receives[0] != pending.Receives[0] || fact.Receives[1].Outcome != "guarantee_expired" {
				t.Fatal("late apply changed original identity/window/arrival facts", err)
			}
			query, err := writer.Query(f.ctx, input.Key)
			if err != nil || query.Value.Version != 1 || string(query.Value.Data) != string(input.Data) || query.Start != pending.Start || query.Deadline != pending.Deadline {
				t.Fatal("ordinary current original processing fact absent", err)
			}
			value, err := writer.Read(f.ctx, input.Resource)
			if err != nil || value.Version != 1 || string(value.Data) != string(input.Data) {
				t.Fatal("ordinary late applied value absent", err)
			}
			state, err = observer.Plan(f.ctx, plan.ID)
			if err != nil || state.Cursor != 2 || len(state.Events) != 2 || state.Events[0].ID != "receive-original" || state.Events[1].ID != "apply-original" {
				t.Fatal("late apply did not retain original finite history", err)
			}
		})
	}
}
