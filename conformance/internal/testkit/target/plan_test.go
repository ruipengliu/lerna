package target_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/conformance/internal/testkit/target"
)

func TestDurableReceivedRequestAppliesAfterWindowExpiry(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	input := target.Request{Key: "late-original", Resource: "fake-document", Data: []byte("late material")}
	plan := target.Plan{ID: "late-scenario", Seed: 73, Deadline: f.now.Add(10 * time.Minute), Steps: []target.Step{{ID: "receive-1", Kind: target.ReceiveOnly, Input: input}, {ID: "apply-1", Kind: target.ApplyReceived, Input: input}}}
	if _, err := writer.InstallPlan(f.ctx, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.RunEvent(f.ctx, plan.ID, "receive-1"); err != nil {
		t.Fatal(err)
	}
	_, err := writer.Query(f.ctx, input.Key)
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("received but not applied must currently be not_found: %v", err)
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	pending, err := observer.Observe(f.ctx, input.Key)
	if err != nil || !pending.Pending || pending.Value.Version != 0 || len(pending.Receives) != 1 || pending.Receives[0].Outcome != "received" {
		t.Fatalf("independent pending receive: %+v %v", pending, err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.writer = nil
	writer = f.open()
	state, err := observer.Plan(f.ctx, plan.ID)
	if err != nil || state.Cursor != 1 || state.Plan.Seed != 73 || string(state.Plan.Steps[0].Input.Data) != "late material" {
		t.Fatalf("durable plan cursor/input: %+v %v", state, err)
	}
	f.now = pending.Deadline
	_, err = writer.Write(f.ctx, input)
	if !errors.Is(err, target.ErrGuaranteeExpired) {
		t.Fatalf("expiry must not renew or apply queued original: %v", err)
	}
	_, err = writer.Query(f.ctx, input.Key)
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("expiry changed queued original: %v", err)
	}
	if _, err = writer.RunEvent(f.ctx, plan.ID, "apply-1"); err != nil {
		t.Fatal(err)
	}
	applied, err := observer.Observe(f.ctx, input.Key)
	if err != nil || applied.Pending || applied.Value.Version != 1 || string(applied.Value.Data) != "late material" || !applied.Start.Equal(pending.Start) || !applied.Deadline.Equal(pending.Deadline) {
		t.Fatalf("late original actual commit: %+v %v", applied, err)
	}
	state, err = observer.Plan(f.ctx, plan.ID)
	if err != nil || state.Cursor != 2 || len(state.Events) != 2 {
		t.Fatalf("finished cursor: %+v %v", state, err)
	}
	normal := newFixture(t)
	normalWriter := normal.open()
	control := target.Plan{ID: "normal", Seed: 73, Deadline: normal.now.Add(10 * time.Minute), Steps: []target.Step{{ID: "write-1", Kind: target.WriteNormally, Input: input}}}
	if _, err = normalWriter.InstallPlan(normal.ctx, control); err != nil {
		t.Fatal(err)
	}
	if _, err = normalWriter.RunEvent(normal.ctx, control.ID, "write-1"); err != nil {
		t.Fatal(err)
	}
	value, err := normalWriter.Read(normal.ctx, input.Resource)
	if err != nil || value.Version != 1 || string(value.Data) != "late material" {
		t.Fatalf("normal control: %+v %v", value, err)
	}
}

func TestBeforeCommitDisconnectRollsBackTargetThenNormalEventCompletes(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	input := target.Request{Key: "original", Resource: "fake-document", Data: []byte("boundary")}
	plan := target.Plan{ID: "before", Seed: 19, Deadline: f.now.Add(time.Hour), Steps: []target.Step{{ID: "disconnect", Kind: target.DisconnectBeforeCommit, Input: input}, {ID: "normal", Kind: target.WriteNormally, Input: input}}}
	if _, err := writer.InstallPlan(f.ctx, plan); err != nil {
		t.Fatal(err)
	}
	_, err := writer.RunEvent(f.ctx, plan.ID, "disconnect")
	if !errors.Is(err, target.ErrDisconnected) {
		t.Fatalf("expected precommit disconnect: %v", err)
	}
	_, err = writer.Query(f.ctx, input.Key)
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("precommit partially wrote original: %v", err)
	}
	_, err = writer.Read(f.ctx, input.Resource)
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("precommit partially wrote current value: %v", err)
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	_, err = observer.Observe(f.ctx, input.Key)
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("precommit left target receive/effect: %v", err)
	}
	state, err := observer.Plan(f.ctx, plan.ID)
	if err != nil || state.Cursor != 1 || len(state.Events) != 1 || state.Events[0].Phase != "rolled_back" {
		t.Fatalf("durable disconnect stage: %+v %v", state, err)
	}
	if _, err = writer.RunEvent(f.ctx, plan.ID, "normal"); err != nil {
		t.Fatal(err)
	}
	fact, err := observer.Observe(f.ctx, input.Key)
	if err != nil || fact.Value.Version != 1 || string(fact.Value.Data) != "boundary" || len(fact.Receives) != 1 {
		t.Fatalf("normal completion after rollback: %+v %v", fact, err)
	}
}

func TestDroppedResponseHasIndependentCommitAndDurableEventIdentity(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	input := target.Request{Key: "original", Resource: "fake-document", Data: []byte("committed")}
	plan := target.Plan{ID: "lost-response", Seed: 31, Deadline: f.now.Add(time.Hour), Steps: []target.Step{{ID: "lost", Kind: target.DropResponse, Input: input}, {ID: "normal", Kind: target.WriteNormally, Input: input}}}
	if _, err := writer.InstallPlan(f.ctx, plan); err != nil {
		t.Fatal(err)
	}
	_, err := writer.RunEvent(f.ctx, plan.ID, "lost")
	if !errors.Is(err, target.ErrResponseLost) {
		t.Fatalf("expected lost response: %v", err)
	}
	original, err := writer.Query(f.ctx, input.Key)
	if err != nil || original.Value.Version != 1 || string(original.Value.Data) != "committed" {
		t.Fatalf("lost response already committed: %+v %v", original, err)
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	fact, err := observer.Observe(f.ctx, input.Key)
	if err != nil || fact.Pending || len(fact.Receives) != 1 {
		t.Fatalf("independent actual commit: %+v %v", fact, err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.writer = nil
	writer = f.open()
	state, err := observer.Plan(f.ctx, plan.ID)
	if err != nil || state.Cursor != 1 || state.Events[0].Phase != "committed_response_lost" {
		t.Fatalf("resumed plan state: %+v %v", state, err)
	}
	_, err = writer.RunEvent(f.ctx, plan.ID, "lost")
	if !errors.Is(err, target.ErrResponseLost) {
		t.Fatalf("duplicate event must restore original failure: %v", err)
	}
	fact, err = observer.Observe(f.ctx, input.Key)
	if err != nil || len(fact.Receives) != 1 || fact.Value.Version != 1 {
		t.Fatalf("duplicate step retransmitted original: %+v %v", fact, err)
	}
	if _, err = writer.RunEvent(f.ctx, plan.ID, "normal"); err != nil {
		t.Fatal(err)
	}
	fact, err = observer.Observe(f.ctx, input.Key)
	if err != nil || fact.Value.Version != 1 || len(fact.Receives) != 2 || fact.Receives[1].Outcome != "replayed" {
		t.Fatalf("normal same original control: %+v %v", fact, err)
	}
}

func TestDuplicateOutOfOrderAndFreshScenarioReplayAreBounded(t *testing.T) {
	first := newFixture(t)
	writer := first.open()
	a := target.Request{Key: "original-a", Resource: "fake-document", Data: []byte("first")}
	b := target.Request{Key: "original-b", Resource: "fake-document", Data: []byte("second")}
	plan := target.Plan{ID: "reordered", Seed: 9007199254740993, Deadline: first.now.Add(time.Hour), Steps: []target.Step{{ID: "receive-a", Kind: target.ReceiveOnly, Input: a}, {ID: "receive-b", Kind: target.ReceiveOnly, Input: b}, {ID: "apply-b", Kind: target.ApplyReceived, Input: b}, {ID: "apply-a", Kind: target.ApplyReceived, Input: a}}}
	if _, err := writer.InstallPlan(first.ctx, plan); err != nil {
		t.Fatal(err)
	}
	_, err := writer.RunEvent(first.ctx, plan.ID, "apply-a")
	if !errors.Is(err, target.ErrOutOfOrder) {
		t.Fatalf("early event: %v", err)
	}
	if _, err = writer.RunEvent(first.ctx, plan.ID, "receive-a"); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.RunEvent(first.ctx, plan.ID, "receive-a"); err != nil {
		t.Fatal(err)
	}
	observer, err := first.observer()
	if err != nil {
		t.Fatal(err)
	}
	fact, err := observer.Observe(first.ctx, a.Key)
	if err != nil || len(fact.Receives) != 1 || !fact.Pending {
		t.Fatalf("duplicate event resent input: %+v %v", fact, err)
	}
	_, err = writer.RunEvent(first.ctx, plan.ID, "apply-b")
	if !errors.Is(err, target.ErrOutOfOrder) {
		t.Fatalf("out-of-order apply: %v", err)
	}
	if _, err = writer.RunEvent(first.ctx, plan.ID, "receive-b"); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	first.writer = nil
	writer = first.open()
	resumed, err := writer.InstallPlan(first.ctx, plan)
	if err != nil || resumed.Cursor != 2 || resumed.Plan.Seed != 9007199254740993 {
		t.Fatalf("install must resume accurate seed/cursor: %+v %v", resumed, err)
	}
	changed := plan
	changed.Seed++
	if _, err = writer.InstallPlan(first.ctx, changed); !errors.Is(err, target.ErrPlanConflict) {
		t.Fatalf("same scenario changed seed: %v", err)
	}
	first.now = first.now.Add(5 * time.Second)
	for _, event := range []string{"apply-b", "apply-a"} {
		if _, err = writer.RunEvent(first.ctx, plan.ID, event); err != nil {
			t.Fatal(err)
		}
	}
	old, err := writer.Query(first.ctx, b.Key)
	if err != nil || old.Value.Version != 1 || string(old.Value.Data) != "second" {
		t.Fatalf("reordered first commit: %+v %v", old, err)
	}
	value, err := writer.Read(first.ctx, a.Resource)
	if err != nil || value.Version != 2 || string(value.Data) != "first" {
		t.Fatalf("late original last commit: %+v %v", value, err)
	}
	for _, step := range plan.Steps {
		if _, err = writer.RunEvent(first.ctx, plan.ID, step.ID); err != nil {
			t.Fatal(err)
		}
	}
	completed, err := observer.Plan(first.ctx, plan.ID)
	if err != nil || completed.Cursor != 4 || len(completed.Events) != 4 {
		t.Fatalf("bounded completed plan: %+v %v", completed, err)
	}
	replay := newFixture(t)
	next := replay.open()
	if _, err = next.InstallPlan(replay.ctx, plan); err != nil {
		t.Fatal(err)
	}
	for i, step := range plan.Steps {
		if i == 2 {
			replay.now = replay.now.Add(5 * time.Second)
		}
		if _, err = next.RunEvent(replay.ctx, plan.ID, step.ID); err != nil {
			t.Fatal(err)
		}
	}
	secondObserver, err := replay.observer()
	if err != nil {
		t.Fatal(err)
	}
	reproduced, err := secondObserver.Plan(replay.ctx, plan.ID)
	if err != nil || !reflect.DeepEqual(completed, reproduced) {
		t.Fatalf("fresh seed/input replay: %+v %v", reproduced, err)
	}
	for _, key := range []string{a.Key, b.Key} {
		one, err := observer.Observe(first.ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		two, err := secondObserver.Observe(replay.ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if one.DatabaseID == two.DatabaseID || one.Value.Version != two.Value.Version || string(one.Value.Data) != string(two.Value.Data) || len(one.Receives) != 1 || len(two.Receives) != 1 {
			t.Fatalf("isolated reproducible target facts: %+v / %+v", one, two)
		}
	}
}

func TestNoQueryLateOriginalAndPlanDeadlinePreserveResponsibility(t *testing.T) {
	f := newFixture(t)
	f.cfg.QueryMode = target.QueryDisabled
	writer := f.open()
	input := target.Request{Key: "original", Resource: "fake-document", Data: []byte("still late")}
	plan := target.Plan{ID: "unqueryable", Seed: 11, Deadline: f.now.Add(time.Hour), Steps: []target.Step{{ID: "receive", Kind: target.ReceiveOnly, Input: input}, {ID: "apply", Kind: target.ApplyReceived, Input: input}}}
	if _, err := writer.InstallPlan(f.ctx, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.RunEvent(f.ctx, plan.ID, "receive"); err != nil {
		t.Fatal(err)
	}
	_, err := writer.Query(f.ctx, input.Key)
	if !errors.Is(err, target.ErrUnsupported) {
		t.Fatalf("no-query mode: %v", err)
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	pending, err := observer.Observe(f.ctx, input.Key)
	if err != nil || !pending.Pending {
		t.Fatalf("durable unqueryable pending: %+v %v", pending, err)
	}
	f.now = pending.Deadline
	_, err = writer.Write(f.ctx, input)
	if !errors.Is(err, target.ErrGuaranteeExpired) {
		t.Fatalf("unqueryable expiry: %v", err)
	}
	if _, err = writer.RunEvent(f.ctx, plan.ID, "apply"); err != nil {
		t.Fatal(err)
	}
	_, err = writer.Query(f.ctx, input.Key)
	if !errors.Is(err, target.ErrUnsupported) {
		t.Fatalf("observer must not supply ordinary guarantee: %v", err)
	}
	value, err := writer.Read(f.ctx, input.Resource)
	if err != nil || value.Version != 1 || string(value.Data) != "still late" {
		t.Fatalf("late no-query read: %+v %v", value, err)
	}
	expired := target.Plan{ID: "finite-expiry", Seed: 11, Deadline: f.now.Add(30 * time.Second), Steps: []target.Step{{ID: "receive", Kind: target.ReceiveOnly, Input: target.Request{Key: "pending-after-plan", Resource: "fake-document", Data: []byte("pending")}}, {ID: "apply", Kind: target.ApplyReceived, Input: target.Request{Key: "pending-after-plan", Resource: "fake-document", Data: []byte("pending")}}}}
	if _, err = writer.InstallPlan(f.ctx, expired); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.RunEvent(f.ctx, expired.ID, "receive"); err != nil {
		t.Fatal(err)
	}
	f.now = expired.Deadline
	_, err = writer.RunEvent(f.ctx, expired.ID, "apply")
	if !errors.Is(err, target.ErrPlanExpired) {
		t.Fatalf("finite plan expiry: %v", err)
	}
	remains, err := observer.Observe(f.ctx, "pending-after-plan")
	if err != nil || !remains.Pending {
		t.Fatalf("plan deadline cancelled original responsibility: %+v %v", remains, err)
	}
	state, err := observer.Plan(f.ctx, expired.ID)
	if err != nil || state.Cursor != 1 {
		t.Fatalf("expired cursor advanced: %+v %v", state, err)
	}
}

func TestPlanIdentityFiniteConfigurationAndRecordedRejection(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	input := target.Request{Key: "original", Resource: "fake-document", Data: []byte("normal")}
	plan := target.Plan{ID: "bounded", Seed: 17, Deadline: f.now.Add(time.Hour), Steps: []target.Step{{ID: "missing", Kind: target.ApplyReceived, Input: input}, {ID: "normal", Kind: target.WriteNormally, Input: input}}}
	for _, change := range []func(*target.Plan){
		func(p *target.Plan) { p.ID = strings.Repeat("x", 129) }, func(p *target.Plan) { p.Steps = nil }, func(p *target.Plan) { p.Steps = append(p.Steps, p.Steps[0]) }, func(p *target.Plan) { p.Steps[0].Kind = "unbounded" }, func(p *target.Plan) { p.Deadline = f.now }, func(p *target.Plan) { p.Deadline = f.now.Add(25 * time.Hour) }, func(p *target.Plan) { p.Deadline = time.Date(2500, 1, 1, 0, 0, 0, 0, time.UTC) },
	} {
		bad := plan
		bad.Steps = append([]target.Step{}, plan.Steps...)
		change(&bad)
		if _, err := writer.InstallPlan(f.ctx, bad); err == nil {
			t.Fatalf("unbounded/ambiguous plan accepted: %+v", bad)
		}
	}
	if _, err := writer.InstallPlan(f.ctx, plan); err != nil {
		t.Fatal(err)
	}
	changed := plan
	changed.Steps = append([]target.Step{}, plan.Steps...)
	changed.Steps[0].Input.Data = []byte("changed")
	if _, err := writer.InstallPlan(f.ctx, changed); !errors.Is(err, target.ErrPlanConflict) {
		t.Fatalf("changed exact input under original scenario: %v", err)
	}
	_, err := writer.RunEvent(f.ctx, plan.ID, "missing")
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("missing durable original cannot be applied: %v", err)
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	state, err := observer.Plan(f.ctx, plan.ID)
	if err != nil || state.Cursor != 1 || len(state.Events) != 1 || state.Events[0].Outcome != "not_found" || state.Events[0].Phase != "rejected" {
		t.Fatalf("bounded rejection record: %+v %v", state, err)
	}
	_, err = writer.RunEvent(f.ctx, plan.ID, "missing")
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("duplicate rejection changed result: %v", err)
	}
	if _, err = writer.RunEvent(f.ctx, plan.ID, "normal"); err != nil {
		t.Fatal(err)
	}
	value, err := writer.Read(f.ctx, input.Resource)
	if err != nil || value.Version != 1 || string(value.Data) != "normal" {
		t.Fatalf("normal after explicit rejection: %+v %v", value, err)
	}
}

func TestConcurrentDuplicateFaultEventCommitsOnlyOriginalOnce(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	input := target.Request{Key: "original", Resource: "fake-document", Data: []byte("normal")}
	plan := target.Plan{ID: "concurrent", Seed: 1, Deadline: f.now.Add(time.Hour), Steps: []target.Step{{ID: "lost", Kind: target.DropResponse, Input: input}}}
	if _, err := writer.InstallPlan(f.ctx, plan); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 6)
	for range 6 {
		go func() { _, err := writer.RunEvent(f.ctx, plan.ID, "lost"); results <- err }()
	}
	for range 6 {
		select {
		case err := <-results:
			if !errors.Is(err, target.ErrResponseLost) {
				t.Errorf("duplicate fault response: %v", err)
			}
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		}
	}
	observer, err := f.observer()
	if err != nil {
		t.Fatal(err)
	}
	fact, err := observer.Observe(f.ctx, input.Key)
	if err != nil || fact.Value.Version != 1 || len(fact.Receives) != 1 {
		t.Fatalf("duplicate event actual target fact: %+v %v", fact, err)
	}
	state, err := observer.Plan(f.ctx, plan.ID)
	if err != nil || state.Cursor != 1 || len(state.Events) != 1 {
		t.Fatalf("duplicate event actual cursor: %+v %v", state, err)
	}
}

func TestPlanRejectsLossyUTF8OriginalIdentities(t *testing.T) {
	f := newFixture(t)
	writer := f.open()
	original := target.Plan{ID: "exact", Seed: 1, Deadline: f.now.Add(time.Hour), Steps: []target.Step{{ID: "write", Kind: target.WriteNormally, Input: target.Request{Key: "a\xff", Resource: "fake-document", Data: []byte("normal")}}}}
	_, err := writer.InstallPlan(f.ctx, original)
	if err == nil {
		t.Fatal("invalid UTF8 key silently changed during durable plan encoding")
	}
	for _, change := range []func(*target.Plan){func(p *target.Plan) { p.ID = "a\xff" }, func(p *target.Plan) { p.Steps[0].ID = "a\xff" }, func(p *target.Plan) { p.Steps[0].Input.Resource = "a\xff" }} {
		invalid := original
		invalid.Steps = append([]target.Step{}, original.Steps...)
		invalid.Steps[0].Input.Key = "valid"
		change(&invalid)
		if _, err = writer.InstallPlan(f.ctx, invalid); err == nil {
			t.Fatal("invalid UTF8 identity persisted lossily")
		}
	}
	original.Steps[0].Input.Key = "原键"
	original.Steps[0].Input.Resource = "资源\x00准确"
	if _, err = writer.InstallPlan(f.ctx, original); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.RunEvent(f.ctx, original.ID, "write"); err != nil {
		t.Fatal(err)
	}
	fact, err := writer.Query(f.ctx, "原键")
	if err != nil || fact.Value.Resource != "资源\x00准确" || string(fact.Value.Data) != "normal" {
		t.Fatalf("normal Unicode exact input: %+v %v", fact, err)
	}
}
