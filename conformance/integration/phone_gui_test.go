package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

// 公开 Dispatcher + 实际数据库 + 独立手机文件目标。远端 Content/授权按已授权
// fixture seam 替代；授权拒绝发生在原真实 StartBarrier，不预置 Operation/目标结果。
type phoneGUIFixture struct {
	*executionFixture
	phones        *target.SimulatedPhones
	root          string
	ids           []string
	lastCommandID string
}

type nativePhoneTruth struct {
	ControlEpoch uint64 `json:"control_epoch"`
	Automatic    bool   `json:"automatic"`
	State        struct {
		Screen         string `json:"screen"`
		Note           string `json:"note"`
		WiFi           bool   `json:"wifi"`
		Version        uint64 `json:"version"`
		FocusedControl string `json:"focused_control"`
		ScrollOffset   uint64 `json:"scroll_offset"`
	} `json:"state"`
	Attempts []struct {
		AttemptID   string `json:"attempt_id"`
		OperationID string `json:"operation_id"`
		InputDigest string `json:"input_digest"`
	} `json:"attempts"`
}

func newPhoneGUIFixture(t *testing.T) *phoneGUIFixture {
	t.Helper()
	f := &phoneGUIFixture{executionFixture: newExecutionFixture(t), root: t.TempDir(), ids: []string{api.NewID("resource"), api.NewID("resource"), api.NewID("resource")}}
	var err error
	f.phones, err = target.NewSimulatedPhones(f.root, f.ids)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if f.phones != nil {
			if err := f.phones.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	f.bind(t)
	return f
}

func (f *phoneGUIFixture) bind(t *testing.T) {
	t.Helper()
	service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: f.content, Authority: f.authority, Location: "device", Drivers: []domain.Driver{f.phones, &target.PhoneGUIDriver{Phones: f.phones}}, ResourceDriver: f.phones})
	if err != nil {
		t.Fatal(err)
	}
	f.registry = rt.NewRegistry()
	if err = service.Register(f.registry); err != nil {
		t.Fatal(err)
	}
	f.dispatcher.Registry = f.registry
}

func (f *phoneGUIFixture) acquire(t *testing.T, id string) domain.ResourceLease {
	t.Helper()
	r := f.command(t, "resource.acquire", id, domain.AcquireInput{ResourceID: id, HolderID: api.NewID("holder"), InstanceID: api.NewID("instance"), LeaseUntil: api.Time(time.Now().Add(time.Minute))}, nil)
	if r.Stage != "applied" {
		t.Fatalf("resource acquisition %+v", r)
	}
	f.drain(t)
	var lease domain.ResourceLease
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &lease)
	if !lease.ActuallyStopped || lease.State != "held" {
		t.Fatalf("resource not ready %+v", lease)
	}
	return lease
}

func (f *phoneGUIFixture) observe(t *testing.T, lease domain.ResourceLease) domain.Observation {
	t.Helper()
	id := api.NewID("observation")
	r := f.command(t, "resource.observe", lease.ResourceID, domain.ObserveInput{ResourceID: lease.ResourceID, ObservationID: id, HolderID: lease.HolderID, InstanceID: lease.InstanceID, ControlEpoch: lease.ControlEpoch}, nil)
	if r.Stage != "accepted" {
		t.Fatalf("observe %+v", r)
	}
	f.drain(t)
	var result domain.ObserveOutput
	f.query(t, "resource.observation.get", id, domain.ObservationIDInput{ObservationID: id}, &result)
	if !result.Ready || result.Observation == nil {
		t.Fatalf("missing accurate observation %+v", result)
	}
	return *result.Observation
}

func (f *phoneGUIFixture) invocation(t *testing.T, observed domain.Observation, args target.PhoneGUIArguments) domain.InvokeInput {
	t.Helper()
	args.ResourceID = observed.ResourceRef.ObjectID
	args.InstanceID = observed.InstanceID
	args.ControlEpoch = observed.ControlEpoch
	args.ObservationID = observed.ObservationID
	if args.TargetVersion == "" {
		args.TargetVersion = observed.TargetVersion
	}
	args.ActionBefore = observed.ActionBefore
	invoke := f.invokeInput(t, false)
	var intent domain.ExecutionIntent
	bytes, err := f.content.ReadBytes(context.Background(), f.sc, f.auth, invoke.IntentRef, "prepare", "device")
	if err != nil || api.Decode(bytes, &intent) != nil {
		t.Fatal(err)
	}
	invoke.CapabilityRef = target.PhoneGUICapability().Ref
	intent.CapabilityRef = invoke.CapabilityRef
	intent.ArgumentsRef = f.put(t, api.Raw(args))
	intent.ResourceRefs = []api.ObjectRef{observed.ResourceRef}
	intent.ProcessedSourceRefs = []api.ContentRef{}
	intent.CostBound = []api.Amount{{Unit: "USD", Value: "0"}}
	invoke.IntentRef = f.put(t, api.Raw(intent))
	invoke.IntentHash, err = api.Digest(intent)
	if err != nil {
		t.Fatal(err)
	}
	return invoke
}

func (f *phoneGUIFixture) act(t *testing.T, invoke domain.InvokeInput) domain.OperationView {
	t.Helper()
	r := f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
	f.lastCommandID = r.CommandID
	if r.Stage != "applied" {
		t.Fatalf("GUI invoke rejected %+v", r)
	}
	f.drain(t)
	var view domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
	return view
}

func (f *phoneGUIFixture) physical(t *testing.T, id string) nativePhoneTruth {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(f.root, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var truth nativePhoneTruth
	if err = json.Unmarshal(body, &truth); err != nil {
		t.Fatal(err)
	}
	return truth
}

func (f *phoneGUIFixture) reopen(t *testing.T) {
	t.Helper()
	if err := f.phones.Close(); err != nil {
		t.Fatal(err)
	}
	f.phones = nil
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	if f.postgresDSN != "" {
		f.st, err = postgres.Open(context.Background(), f.postgresDSN, postgres.WithExpectedDatabaseID(f.sc.DatabaseID))
	} else {
		f.st, err = sqlite.Open(f.databasePath, sqlite.WithExpectedDatabaseID(f.sc.DatabaseID))
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.st.Close(); err != nil {
			t.Error(err)
		}
	})
	f.dispatcher.Store = f.st
	f.phones, err = target.NewSimulatedPhones(f.root, f.ids)
	if err != nil {
		t.Fatal(err)
	}
	f.bind(t)
}

func (f *phoneGUIFixture) evidence(t *testing.T, kind, action string, invoke domain.InvokeInput, view domain.OperationView, before, after *domain.Observation, physical nativePhoneTruth) {
	t.Helper()
	backend := "sqlite"
	if f.postgresDSN != "" {
		backend = "postgres"
	}
	result := map[string]any{"test": t.Name(), "kind": kind, "action": action, "backend": backend, "scope": f.sc, "command_id": f.lastCommandID, "task_ref": invoke.TaskRef, "operation_ref": f.sc.Ref(invoke.OperationID, view.Operation.Revision), "operation": view.Operation, "attempts": view.Attempts.Items, "physical": physical, "capability_ref": invoke.CapabilityRef}
	if before != nil {
		result["before_observation"] = before
	}
	if after != nil {
		result["after_observation"] = after
	}
	t.Logf("GUI_EVIDENCE %s", api.Raw(result))
}

func TestPublicGUIAllGesturesThreeDevicesObserveActionObserveVerify(t *testing.T) {
	f := newPhoneGUIFixture(t)
	for index, id := range f.ids {
		lease := f.acquire(t, id)
		text := fmt.Sprintf("设备 %d 的独立准确笔记\nsecond line", index)
		steps := []struct {
			args                  target.PhoneGUIArguments
			screen, note, focused string
			scroll                uint64
		}{
			{args: target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}}, screen: "notes"},
			{args: target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 240}}, screen: "notes", focused: "note_editor"},
			{args: target.PhoneGUIArguments{Action: "input", Text: &text}, screen: "notes", note: text, focused: "note_editor"},
			{args: target.PhoneGUIArguments{Action: "swipe", From: &target.PhonePoint{X: 180, Y: 400}, To: &target.PhonePoint{X: 180, Y: 140}}, screen: "notes", note: text, focused: "note_editor", scroll: 260},
			{args: target.PhoneGUIArguments{Action: "back"}, screen: "notes", note: text, scroll: 260},
			{args: target.PhoneGUIArguments{Action: "back"}, screen: "home", note: text},
		}
		for position, step := range steps {
			before := f.observe(t, lease)
			var tree target.PhoneState
			if err := api.Decode(before.Data, &tree); err != nil || tree.UI == nil || tree.UI.Width != 360 || tree.UI.Height != 640 || len(tree.UI.Controls) == 0 {
				t.Fatalf("device %d position %d usable tree absent %v", index, position, err)
			}
			invoke := f.invocation(t, before, step.args)
			view := f.act(t, invoke)
			if view.Operation.Effect != "applied" || view.Operation.ExecutionState != "closed" || view.Operation.Attempts.TotalCount != 1 || len(view.Attempts.Items) != 1 || !view.Operation.UsageFinal {
				t.Fatalf("device %d gesture %s public facts %+v", index, step.args.Action, view)
			}
			after := f.observe(t, lease)
			var observed target.PhoneState
			if err := api.Decode(after.Data, &observed); err != nil || observed.Screen != step.screen || observed.Note != step.note || observed.FocusedControl != step.focused || observed.ScrollOffset != step.scroll || observed.Version != uint64(position+2) {
				t.Fatalf("device %d gesture %s current UI %+v %v", index, step.args.Action, observed, err)
			}
			truth := f.physical(t, id)
			if truth.State.Screen != step.screen || truth.State.Note != step.note || truth.State.FocusedControl != step.focused || truth.State.ScrollOffset != step.scroll || truth.State.Version != uint64(position+2) || len(truth.Attempts) != position+1 || truth.Attempts[position].AttemptID != view.Attempts.Items[0].AttemptID || truth.Attempts[position].OperationID != invoke.OperationID {
				t.Fatalf("device %d gesture %s independent target truth %+v", index, step.args.Action, truth)
			}
			f.evidence(t, "observe_action_observe_independent_truth", step.args.Action, invoke, view, &before, &after, truth)
		}
	}
}

func TestPublicGUIRejectsForgedOldObservationBeforePhysicalEntry(t *testing.T) {
	f := newPhoneGUIFixture(t)
	id := f.ids[0]
	lease := f.acquire(t, id)
	old := f.observe(t, lease)
	if err := f.phones.HumanChange(context.Background(), id, "home"); err != nil {
		t.Fatal(err)
	}
	// 原 ID/窗口保持旧观察，但伪造当前版本；不能只在目标处比较当前版本而绕过原观察。
	invoke := f.invocation(t, old, target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}, TargetVersion: "2"})
	view := f.act(t, invoke)
	truth := f.physical(t, id)
	if view.Operation.Effect != "not_started" || !view.NewAttemptsClosed || truth.State.Screen != "home" || truth.State.Version != 2 || len(truth.Attempts) != 0 {
		t.Fatalf("forged old observation reached real exit: %+v physical=%+v", view, truth)
	}
	f.evidence(t, "old_observation_current_version_forgery_rejected", "click", invoke, view, &old, nil, truth)
}

func TestPublicGUIPermissionLimitsRemainAtOriginalResourceAndStartBarrier(t *testing.T) {
	f := newPhoneGUIFixture(t)
	id := f.ids[0]
	unauthorized := f.auth
	unauthorized.Roles = []string{"viewer"}
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.sc.OwnerID, CommandID: api.NewID("command"), Method: "resource.acquire", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(domain.AcquireInput{ResourceID: id, HolderID: api.NewID("holder"), InstanceID: api.NewID("instance"), LeaseUntil: api.Time(time.Now().Add(time.Minute))})}
	denied, err := f.dispatcher.Command(context.Background(), unauthorized, api.Raw(command))
	if err != nil || denied.Stage != "rejected" || denied.Error == nil || denied.Error.Code != "forbidden" {
		t.Fatalf("unprivileged resource acquired: %+v %v", denied, err)
	}
	lease := f.acquire(t, id)
	before := f.observe(t, lease)
	invoke := f.invocation(t, before, target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}})
	originalReceipt := f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
	f.lastCommandID = originalReceipt.CommandID
	if originalReceipt.Stage != "applied" {
		t.Fatal(originalReceipt)
	}
	f.authority.denied = true
	f.drain(t)
	var view domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
	truth := f.physical(t, id)
	if view.Operation.Effect != "not_started" || !view.Operation.UsageFinal || !view.NewAttemptsClosed || truth.State.Screen != "home" || truth.State.Version != 1 || len(truth.Attempts) != 0 {
		t.Fatalf("revoked original use altered target: %+v physical=%+v", view, truth)
	}
	f.evidence(t, "resource_role_and_current_use_denied_before_entry", "click", invoke, view, &before, nil, truth)
}

func TestPublicGUIUnknownMediaFailureReopensAndQueriesOriginalAttempt(t *testing.T) {
	f := newPhoneGUIFixture(t)
	id := f.ids[0]
	lease := f.acquire(t, id)
	for _, point := range []*target.PhonePoint{{X: 180, Y: 160}, {X: 180, Y: 240}} {
		before := f.observe(t, lease)
		view := f.act(t, f.invocation(t, before, target.PhoneGUIArguments{Action: "click", Point: point}))
		if view.Operation.Effect != "applied" {
			t.Fatalf("setup click failed %+v", view)
		}
	}
	before := f.observe(t, lease)
	text := "exact original input survives a missing action result"
	invoke := f.invocation(t, before, target.PhoneGUIArguments{Action: "input", Text: &text})
	physicalActions := 0
	f.phones.Fault = func(resource, point string) error {
		if resource == id && point == "applied" {
			physicalActions++
			return errors.New("fixture lost reply after exact durable GUI input")
		}
		return nil
	}
	unknown := f.act(t, invoke)
	originalCommandID := f.lastCommandID
	if unknown.Operation.Effect != "unknown" || len(unknown.Attempts.Items) != 1 || unknown.Operation.UsageFinal || physicalActions != 1 {
		t.Fatalf("lost result guessed effect or repeated action %+v physical=%d", unknown, physicalActions)
	}
	originalAttemptID := unknown.Attempts.Items[0].AttemptID
	truth := f.physical(t, id)
	if truth.State.Note != text || truth.State.Version != 4 || len(truth.Attempts) != 3 || truth.Attempts[2].AttemptID != originalAttemptID {
		t.Fatalf("real unknown input target not applied once %+v", truth)
	}
	var occupied domain.ResourceLease
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &occupied)
	if occupied.InflightWrite != originalAttemptID {
		t.Fatal("unknown input released physical writer")
	}
	// 真正使原目标介质路径不可读；核对失败必须保留原 unknown，而非猜 not_applied。
	path := filepath.Join(f.root, id+".json")
	if err := os.Rename(path, path+".unavailable"); err != nil {
		t.Fatal(err)
	}
	if r := f.command(t, "execution.reconcile", invoke.OperationID, domain.ReconcileInput{OperationID: invoke.OperationID}, nil); r.Stage != "applied" {
		t.Fatal(r)
	}
	if err := rt.Drain(context.Background(), f.st, f.sc, f.registry, 100); err == nil {
		t.Fatal("missing physical target unexpectedly yielded a conclusive result")
	}
	var stillUnknown domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &stillUnknown)
	if stillUnknown.Operation.Effect != "unknown" || stillUnknown.Attempts.Items[0].AttemptID != originalAttemptID {
		t.Fatalf("failed target read discarded original unknown %+v", stillUnknown)
	}
	if err := os.Rename(path+".unavailable", path); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	if receipt, err := f.dispatcher.Lookup(context.Background(), f.auth, originalCommandID); err != nil || receipt.Stage != "applied" || receipt.CommandID != originalCommandID {
		t.Fatalf("reopen did not preserve original command acceptance: %+v %v", receipt, err)
	}
	if r := f.command(t, "execution.reconcile", invoke.OperationID, domain.ReconcileInput{OperationID: invoke.OperationID}, nil); r.Stage != "applied" {
		t.Fatal(r)
	}
	f.drain(t)
	var recovered domain.OperationView
	waitCtx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	for {
		if err := rt.Drain(waitCtx, f.st, f.sc, f.registry, 100); err != nil {
			t.Fatal(err)
		}
		recovered = domain.OperationView{}
		f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &recovered)
		if recovered.Operation.Effect == "applied" {
			break
		}
		select {
		case <-waitCtx.Done():
			t.Fatalf("original query did not resume after original claim deadline: %+v", recovered)
		case <-time.After(400 * time.Millisecond):
		}
	}
	truth = f.physical(t, id)
	if recovered.Operation.Effect != "applied" || !recovered.NewAttemptsClosed || !recovered.Operation.UsageFinal || recovered.Operation.Attempts.TotalCount != 1 || len(recovered.Attempts.Items) != 1 || recovered.Attempts.Items[0].AttemptID != originalAttemptID || len(truth.Attempts) != 3 || truth.State.Version != 4 || truth.State.Note != text {
		t.Fatalf("reopen did not recover original action exactly once %+v target=%+v", recovered, truth)
	}
	after := f.observe(t, lease)
	f.lastCommandID = originalCommandID
	f.evidence(t, "lost_result_target_unavailable_database_and_target_reopen_original_query", "input", invoke, recovered, &before, &after, truth)
}

func TestPublicGUIInterruptBeforeEntryThenOwnerTakeoverRequiresFreshEpochAndObservation(t *testing.T) {
	f := newPhoneGUIFixture(t)
	id := f.ids[0]
	lease := f.acquire(t, id)
	before := f.observe(t, lease)
	interrupted := f.invocation(t, before, target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}})
	r := f.command(t, "execution.invoke", interrupted.OperationID, interrupted, nil)
	if r.Stage != "applied" {
		t.Fatal(r)
	}
	interruptedCommandID := r.CommandID
	if r = f.command(t, "execution.cancel", interrupted.OperationID, domain.CancelInput{OperationID: interrupted.OperationID, OrchestratorID: interrupted.TaskRef.OwnerID, TaskRef: interrupted.TaskRef, Reason: "user interrupts before GUI entry"}, nil); r.Stage != "applied" {
		t.Fatal(r)
	}
	f.drain(t)
	var cancelled domain.OperationView
	f.query(t, "execution.get", interrupted.OperationID, domain.OperationIDInput{OperationID: interrupted.OperationID}, &cancelled)
	truth := f.physical(t, id)
	if !cancelled.NewAttemptsClosed || cancelled.Operation.Effect != "not_started" || cancelled.Operation.Attempts.TotalCount != 0 || truth.State.Version != 1 || len(truth.Attempts) != 0 {
		t.Fatalf("interrupt executed physical gesture %+v native=%+v", cancelled, truth)
	}
	f.lastCommandID = interruptedCommandID
	f.evidence(t, "interrupt_before_physical_entry", "click", interrupted, cancelled, &before, nil, truth)
	oldInvoke := f.invocation(t, before, target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}})
	if r = f.command(t, "execution.invoke", oldInvoke.OperationID, oldInvoke, nil); r.Stage != "applied" {
		t.Fatal(r)
	}
	var current domain.ResourceLease
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &current)
	revision := current.Revision
	r = f.command(t, "resource.takeover", id, domain.TakeoverInput{ResourceID: id, Reason: "user owns the foreground device"}, &revision)
	if r.Stage != "applied" {
		t.Fatal(r)
	}
	var saved domain.ResourceLease
	if err := api.Decode(r.Output, &saved); err != nil || saved.ControlEpoch != 2 || saved.ActuallyStopped {
		t.Fatalf("saved takeover claimed premature actual stop %+v %v", saved, err)
	}
	f.drain(t)
	var fenced domain.OperationView
	f.query(t, "execution.get", oldInvoke.OperationID, domain.OperationIDInput{OperationID: oldInvoke.OperationID}, &fenced)
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &current)
	truth = f.physical(t, id)
	if fenced.Operation.Effect != "not_started" || !fenced.NewAttemptsClosed || !current.ActuallyStopped || current.State != "released" || current.ControlEpoch != 2 || truth.ControlEpoch != 2 || truth.Automatic || truth.State.Version != 1 || len(truth.Attempts) != 0 {
		t.Fatalf("owner takeover allowed old gesture %+v lease=%+v native=%+v", fenced, current, truth)
	}
	oldAcquire := f.command(t, "resource.acquire", id, domain.AcquireInput{ResourceID: id, HolderID: lease.HolderID, InstanceID: api.NewID("instance"), ExpectedControlEpoch: 1, LeaseUntil: api.Time(time.Now().Add(time.Minute))}, nil)
	if oldAcquire.Stage != "rejected" {
		t.Fatal("old epoch regained automatic control")
	}
	r = f.command(t, "resource.acquire", id, domain.AcquireInput{ResourceID: id, HolderID: lease.HolderID, InstanceID: api.NewID("instance"), ExpectedControlEpoch: 2, LeaseUntil: api.Time(time.Now().Add(time.Minute))}, nil)
	if r.Stage != "applied" {
		t.Fatal(r)
	}
	f.drain(t)
	current = domain.ResourceLease{}
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &current)
	if current.ControlEpoch != 3 || current.InstanceID == lease.InstanceID {
		t.Fatal("fresh automatic control did not replace epoch and instance")
	}
	fresh := f.observe(t, current)
	legitimate := f.invocation(t, fresh, target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}})
	view := f.act(t, legitimate)
	after := f.observe(t, current)
	truth = f.physical(t, id)
	if view.Operation.Effect != "applied" || truth.State.Screen != "notes" || truth.State.Version != 2 || len(truth.Attempts) != 1 || truth.ControlEpoch != 3 {
		t.Fatalf("legitimate fresh epoch action did not succeed %+v target=%+v", view, truth)
	}
	f.evidence(t, "owner_takeover_old_entry_fenced_fresh_epoch_and_observation", "click", legitimate, view, &fresh, &after, truth)
}

func TestPublicGUIUnknownTakeoverRetainsOriginalEffectAndCannotAcquireEarly(t *testing.T) {
	f := newPhoneGUIFixture(t)
	id := f.ids[0]
	lease := f.acquire(t, id)
	before := f.observe(t, lease)
	invoke := f.invocation(t, before, target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}})
	f.phones.Fault = func(resource, point string) error {
		if resource == id && point == "applied" {
			return errors.New("fixture lost click reply before owner takeover")
		}
		return nil
	}
	unknown := f.act(t, invoke)
	originalCommandID := f.lastCommandID
	if unknown.Operation.Effect != "unknown" || len(unknown.Attempts.Items) != 1 {
		t.Fatal("unknown original GUI attempt missing")
	}
	originalAttemptID := unknown.Attempts.Items[0].AttemptID
	var current domain.ResourceLease
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &current)
	revision := current.Revision
	r := f.command(t, "resource.takeover", id, domain.TakeoverInput{ResourceID: id, Reason: "user interrupts an unknown GUI effect"}, &revision)
	if r.Stage != "applied" {
		t.Fatal(r)
	}
	var saved domain.ResourceLease
	if err := api.Decode(r.Output, &saved); err != nil || saved.ControlEpoch != 2 || saved.ActuallyStopped || saved.InflightWrite != originalAttemptID {
		t.Fatalf("saved takeover discarded unknown physical responsibility %+v %v", saved, err)
	}
	early := f.command(t, "resource.acquire", id, domain.AcquireInput{ResourceID: id, HolderID: lease.HolderID, InstanceID: api.NewID("instance"), ExpectedControlEpoch: 2, LeaseUntil: api.Time(time.Now().Add(time.Minute))}, nil)
	if early.Stage != "rejected" || early.Error == nil || early.Error.Reason != "resource_conflict" {
		t.Fatalf("unknown lease freed early %+v", early)
	}
	f.drain(t)
	var recovered domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &recovered)
	current = domain.ResourceLease{}
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &current)
	truth := f.physical(t, id)
	if !recovered.NewAttemptsClosed || !recovered.ActuallyStopped || recovered.Operation.Effect != "applied" || !recovered.Operation.UsageFinal || len(recovered.Attempts.Items) != 1 || recovered.Attempts.Items[0].AttemptID != originalAttemptID || current.InflightWrite != "" || !current.ActuallyStopped || current.State != "released" || len(truth.Attempts) != 1 || truth.State.Screen != "notes" || truth.State.Version != 2 || truth.ControlEpoch != 2 || truth.Automatic {
		t.Fatalf("takeover lost late effect or repeated gesture %+v lease=%+v native=%+v", recovered, current, truth)
	}
	f.lastCommandID = originalCommandID
	f.evidence(t, "unknown_owner_takeover_closed_entry_original_late_effect", "click", invoke, recovered, &before, nil, truth)
}
