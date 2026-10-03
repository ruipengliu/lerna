package execution_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

func TestThreeSimulatedPhonesAllGUIGesturesObserveAndIndependentTruth(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ids := []string{api.NewID("resource"), api.NewID("resource"), api.NewID("resource")}
	phones, err := target.NewSimulatedPhones(root, ids)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := phones.Close(); err != nil {
			t.Error(err)
		}
	})
	driver := &target.PhoneGUIDriver{Phones: phones}
	scope := rt.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("executor")}
	barriers := 0
	for index, id := range ids {
		lease := domain.ResourceLease{ResourceID: id, HolderID: api.NewID("holder"), InstanceID: api.NewID("instance"), ControlEpoch: 1, State: "held", LeaseUntil: api.Time(time.Now().Add(time.Minute))}
		if _, err = phones.Fence(ctx, scope, lease); err != nil {
			t.Fatal(err)
		}
		text := fmt.Sprintf("独立设备 %d 的准确笔记\nsecond line", index)
		steps := []struct {
			action, screen, note, focus string
			point, from, to             *target.PhonePoint
			text                        *string
			scroll                      uint64
		}{
			{action: "click", point: &target.PhonePoint{X: 180, Y: 160}, screen: "notes"},
			{action: "click", point: &target.PhonePoint{X: 180, Y: 240}, screen: "notes", focus: "note_editor"},
			{action: "input", text: &text, screen: "notes", note: text, focus: "note_editor"},
			{action: "swipe", from: &target.PhonePoint{X: 180, Y: 400}, to: &target.PhonePoint{X: 180, Y: 140}, screen: "notes", note: text, focus: "note_editor", scroll: 260},
			{action: "back", screen: "notes", note: text, scroll: 260},
			{action: "back", screen: "home", note: text},
		}
		for stepIndex, step := range steps {
			before, err := phones.Observe(ctx, scope, lease, api.NewID("observation"))
			if err != nil {
				t.Fatal(err)
			}
			var ui struct {
				UI struct {
					Width, Height uint64
					Controls      []struct {
						ID, Kind string
					}
				}
			}
			if err = json.Unmarshal(before.Data, &ui); err != nil || ui.UI.Width != 360 || ui.UI.Height != 640 || len(ui.UI.Controls) == 0 {
				t.Fatalf("device %d step %d lacks usable current UI tree: %s %v", index, stepIndex, before.Data, err)
			}
			args := target.PhoneGUIArguments{ResourceID: id, InstanceID: lease.InstanceID, ControlEpoch: lease.ControlEpoch, ObservationID: before.ObservationID, TargetVersion: before.TargetVersion, ActionBefore: before.ActionBefore, Action: step.action, Point: step.point, From: step.from, To: step.to, Text: step.text}
			prepared, err := driver.Prepare(ctx, scope, rt.Auth{}, domain.InvokeInput{}, domain.ExecutionIntent{}, api.Raw(args))
			if err != nil {
				t.Fatal(err)
			}
			original := domain.AttemptRequest{Scope: scope, Invoke: domain.InvokeInput{OperationID: api.NewID("operation")}, Attempt: domain.Attempt{AttemptID: api.NewID("attempt"), Prepared: prepared}}
			fact, err := driver.Start(ctx, original, func(context.Context) error { barriers++; return nil })
			if err != nil || fact.Effect != "applied" || fact.MayApplyLater != false {
				t.Fatalf("device %d step %d gesture %s: %+v %v", index, stepIndex, step.action, fact, err)
			}
			after, err := phones.Observe(ctx, scope, lease, api.NewID("observation"))
			if err != nil {
				t.Fatal(err)
			}
			var observed struct {
				Screen         string `json:"screen"`
				Note           string `json:"note"`
				FocusedControl string `json:"focused_control"`
				ScrollOffset   uint64 `json:"scroll_offset"`
				Version        uint64 `json:"version"`
			}
			if err = json.Unmarshal(after.Data, &observed); err != nil || observed.Screen != step.screen || observed.Note != step.note || observed.FocusedControl != step.focus || observed.ScrollOffset != step.scroll || observed.Version != uint64(stepIndex+2) {
				t.Fatalf("device %d step %d independent expected UI: %+v %v", index, stepIndex, observed, err)
			}
			bytes, err := os.ReadFile(filepath.Join(root, id+".json"))
			if err != nil {
				t.Fatal(err)
			}
			physical := struct {
				State    any `json:"state"`
				Attempts []struct {
					AttemptID   string `json:"attempt_id"`
					OperationID string `json:"operation_id"`
				} `json:"attempts"`
			}{State: &observed}
			if err = json.Unmarshal(bytes, &physical); err != nil || observed.Screen != step.screen || observed.Note != step.note || observed.FocusedControl != step.focus || observed.ScrollOffset != step.scroll || observed.Version != uint64(stepIndex+2) || len(physical.Attempts) != stepIndex+1 || physical.Attempts[stepIndex].AttemptID != original.Attempt.AttemptID || physical.Attempts[stepIndex].OperationID != original.Invoke.OperationID {
				t.Fatalf("device %d step %d native target differs: %+v %v", index, stepIndex, observed, err)
			}
		}
	}
	if barriers != 18 {
		t.Fatalf("physical action count %d, expected 18", barriers)
	}
}

func TestPhoneGUISchemaRejectsWrongShapeCoordinatesAndText(t *testing.T) {
	base := target.PhoneGUIArguments{ResourceID: api.NewID("resource"), InstanceID: api.NewID("instance"), ControlEpoch: 1, ObservationID: api.NewID("observation"), TargetVersion: "1", ActionBefore: api.Time(time.Now().Add(time.Minute))}
	driver := &target.PhoneGUIDriver{}
	cases := []struct {
		name string
		edit func(*target.PhoneGUIArguments)
	}{
		{"click_missing_point", func(p *target.PhoneGUIArguments) { p.Action = "click" }},
		{"click_extra_text", func(p *target.PhoneGUIArguments) {
			p.Action = "click"
			p.Point = &target.PhonePoint{X: 180, Y: 160}
			s := "extra"
			p.Text = &s
		}},
		{"back_extra_point", func(p *target.PhoneGUIArguments) { p.Action = "back"; p.Point = &target.PhonePoint{} }},
		{"outside_viewport", func(p *target.PhoneGUIArguments) { p.Action = "click"; p.Point = &target.PhonePoint{X: 360, Y: 160} }},
		{"swipe_missing_end", func(p *target.PhoneGUIArguments) { p.Action = "swipe"; p.From = &target.PhonePoint{X: 180, Y: 400} }},
		{"input_missing_text", func(p *target.PhoneGUIArguments) { p.Action = "input" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.edit(&p)
			if _, err := driver.Prepare(context.Background(), rt.Scope{}, rt.Auth{}, domain.InvokeInput{}, domain.ExecutionIntent{}, api.Raw(p)); !api.IsCode(err, "invalid_request") {
				t.Fatalf("invalid shape reached prepared action: %v", err)
			}
		})
	}
}

func TestPhoneGUIAtomicGestureAndIndependentHumanChangeShareTargetEntry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	id := api.NewID("resource")
	phones, err := target.NewSimulatedPhones(root, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := phones.Close(); err != nil {
			t.Error(err)
		}
	})
	scope := rt.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("executor")}
	lease := domain.ResourceLease{ResourceID: id, HolderID: api.NewID("holder"), InstanceID: api.NewID("instance"), ControlEpoch: 1, State: "held", LeaseUntil: api.Time(time.Now().Add(time.Minute))}
	if _, err = phones.Fence(ctx, scope, lease); err != nil {
		t.Fatal(err)
	}
	observation, err := phones.Observe(ctx, scope, lease, api.NewID("observation"))
	if err != nil {
		t.Fatal(err)
	}
	driver := &target.PhoneGUIDriver{Phones: phones}
	args := target.PhoneGUIArguments{ResourceID: id, InstanceID: lease.InstanceID, ControlEpoch: 1, ObservationID: observation.ObservationID, TargetVersion: observation.TargetVersion, ActionBefore: observation.ActionBefore, Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}}
	prepared, err := driver.Prepare(ctx, scope, rt.Auth{}, domain.InvokeInput{}, domain.ExecutionIntent{}, api.Raw(args))
	if err != nil {
		t.Fatal(err)
	}
	original := domain.AttemptRequest{Scope: scope, Invoke: domain.InvokeInput{OperationID: api.NewID("operation")}, Attempt: domain.Attempt{AttemptID: api.NewID("attempt"), Prepared: prepared}}
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	completed := make(chan error, 1)
	go func() {
		fact, err := driver.Start(ctx, original, func(ctx context.Context) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if err == nil && fact.Effect != "applied" {
			err = fmt.Errorf("valid atomic click has effect %s", fact.Effect)
		}
		completed <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	humanStarted := make(chan struct{})
	humanResult := make(chan error, 1)
	go func() {
		close(humanStarted)
		humanResult <- phones.HumanChange(ctx, id, "settings")
	}()
	<-humanStarted
	select {
	case err := <-humanResult:
		t.Fatalf("human target mutation escaped held atomic entry: %v", err)
	default:
	}
	releaseOnce.Do(func() { close(release) })
	for _, result := range []<-chan error{completed, humanResult} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	body, err := os.ReadFile(filepath.Join(root, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var physical struct {
		State struct {
			Screen  string `json:"screen"`
			Version uint64 `json:"version"`
		} `json:"state"`
		Attempts []struct {
			AttemptID string `json:"attempt_id"`
		} `json:"attempts"`
	}
	if err = json.Unmarshal(body, &physical); err != nil || physical.State.Screen != "settings" || physical.State.Version != 3 || len(physical.Attempts) != 1 || physical.Attempts[0].AttemptID != original.Attempt.AttemptID {
		t.Fatalf("atomic gesture and human change lost serial target truth: %+v %v", physical, err)
	}
}
