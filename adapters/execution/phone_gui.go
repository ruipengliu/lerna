package execution

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

type PhonePoint struct {
	X uint64 `json:"x"`
	Y uint64 `json:"y"`
}

type PhoneRect struct {
	X      uint64 `json:"x"`
	Y      uint64 `json:"y"`
	Width  uint64 `json:"width"`
	Height uint64 `json:"height"`
}

type PhoneControl struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Bounds  PhoneRect `json:"bounds"`
	Text    string    `json:"text"`
	Enabled bool      `json:"enabled"`
	Focused bool      `json:"focused"`
}

type PhoneUI struct {
	Width        uint64         `json:"width"`
	Height       uint64         `json:"height"`
	ScrollOffset uint64         `json:"scroll_offset"`
	Controls     []PhoneControl `json:"controls"`
}

type PhoneGUIArguments struct {
	ResourceID    string      `json:"resource_id"`
	InstanceID    string      `json:"instance_id"`
	ControlEpoch  uint64      `json:"control_epoch"`
	ObservationID string      `json:"observation_id"`
	TargetVersion string      `json:"target_version"`
	ActionBefore  string      `json:"action_before"`
	Action        string      `json:"action"`
	Point         *PhonePoint `json:"point,omitempty"`
	From          *PhonePoint `json:"from,omitempty"`
	To            *PhonePoint `json:"to,omitempty"`
	Text          *string     `json:"text,omitempty"`
}

// PhoneGUIDriver 保留 v1 原驱动，v2 手势共享原生设备的入口锁及原 Attempt 日志。
type PhoneGUIDriver struct{ Phones *SimulatedPhones }

func PhoneGUICapability() domain.Capability {
	base := api.SchemaFor[PhoneGUIArguments]()
	properties := base["properties"].(map[string]any)
	properties["text"] = api.Schema{"type": "string", "maxLength": 4096}
	pointSchema := api.Schema{"type": "object", "properties": map[string]any{"x": api.Schema{"type": "integer", "minimum": 0, "maximum": 359}, "y": api.Schema{"type": "integer", "minimum": 0, "maximum": 639}}, "required": []string{"x", "y"}, "additionalProperties": false}
	for _, name := range []string{"point", "from", "to"} {
		properties[name] = pointSchema
	}
	common := []string{"resource_id", "instance_id", "control_epoch", "observation_id", "target_version", "action_before", "action"}
	branches := []any{}
	for _, variant := range []struct {
		action string
		fields []string
	}{{"click", []string{"point"}}, {"swipe", []string{"from", "to"}}, {"input", []string{"text"}}, {"back", nil}} {
		p := map[string]any{}
		required := append([]string{}, common...)
		for _, field := range common {
			p[field] = properties[field]
		}
		p["action"] = api.Schema{"const": variant.action, "type": "string"}
		for _, field := range variant.fields {
			p[field] = properties[field]
			required = append(required, field)
		}
		branches = append(branches, api.Schema{"type": "object", "properties": p, "required": required, "additionalProperties": false})
	}
	input := api.Schema{"oneOf": branches}
	output := api.SchemaFor[PhoneActionResult]()
	digest, _ := api.Digest([]any{"simulated_phone.action", "2", input, output, "atomic"})
	return domain.Capability{Ref: api.ComponentRef{ComponentID: domain.BuiltinComponentID("simulated_phone.action"), Version: "2", Digest: digest}, EffectClass: "no_idempotency_guarantee", MaxAttempts: 1, InputSchema: input, OutputSchema: output}
}

func (*PhoneGUIDriver) Capability() domain.Capability { return PhoneGUICapability() }

func (d *PhoneGUIDriver) Prepare(ctx context.Context, sc rt.Scope, auth rt.Auth, invoke domain.InvokeInput, intent domain.ExecutionIntent, body []byte) (domain.PreparedRequest, error) {
	if err := ctx.Err(); err != nil {
		return domain.PreparedRequest{}, err
	}
	validator, err := api.NewValidator(PhoneGUICapability().InputSchema)
	if err != nil {
		return domain.PreparedRequest{}, err
	}
	if err = validator.Validate(body); err != nil {
		return domain.PreparedRequest{}, err
	}
	var args PhoneGUIArguments
	if err = api.Decode(body, &args); err != nil {
		return domain.PreparedRequest{}, err
	}
	if !api.ValidID(args.ResourceID) || !api.ValidID(args.InstanceID) || !api.ValidID(args.ObservationID) || args.ControlEpoch == 0 {
		return domain.PreparedRequest{}, api.E("invalid_request", "invalid_device_arguments")
	}
	if _, err = api.ParseTime(args.ActionBefore); err != nil {
		return domain.PreparedRequest{}, api.E("invalid_request", "invalid_observation_window")
	}
	if args.Text != nil && len(*args.Text) > 4096 {
		return domain.PreparedRequest{}, api.E("invalid_request", "gui_text_limit")
	}
	for _, point := range []*PhonePoint{args.Point, args.From, args.To} {
		if point != nil && (point.X >= 360 || point.Y >= 640) {
			return domain.PreparedRequest{}, api.E("invalid_request", "gui_point_outside_viewport")
		}
	}
	raw := api.Raw(args)
	return domain.PreparedRequest{Encoded: raw, Digest: api.Hash(raw), ResourceID: args.ResourceID, ResourceEpoch: args.ControlEpoch, ObservationID: args.ObservationID, ObservationBefore: args.ActionBefore, ObservationTargetVersion: args.TargetVersion}, nil
}

func (d *PhoneGUIDriver) Start(ctx context.Context, q domain.AttemptRequest, barrier func(context.Context) error) (domain.Fact, error) {
	if d.Phones == nil {
		return domain.Fact{}, api.E("dependency_unavailable", "device_host_not_configured")
	}
	var args PhoneGUIArguments
	if err := api.Decode(q.Attempt.Prepared.Encoded, &args); err != nil {
		return domain.Fact{}, err
	}
	binding := PhoneActionArguments{ResourceID: args.ResourceID, InstanceID: args.InstanceID, ControlEpoch: args.ControlEpoch, ObservationID: args.ObservationID, TargetVersion: args.TargetVersion, ActionBefore: args.ActionBefore}
	return d.Phones.startAction(ctx, q, binding, barrier, func(state *PhoneState) error { return applyPhoneGesture(state, args) })
}

func (d *PhoneGUIDriver) Reconcile(ctx context.Context, q domain.AttemptRequest) (domain.Fact, error) {
	if d.Phones == nil {
		return domain.Fact{}, api.E("dependency_unavailable", "device_host_not_configured")
	}
	var args PhoneGUIArguments
	if err := api.Decode(q.Attempt.Prepared.Encoded, &args); err != nil {
		return domain.Fact{}, err
	}
	return d.Phones.reconcileAttempt(ctx, q, args.ResourceID)
}

func (d *PhoneGUIDriver) Stop(ctx context.Context, q domain.AttemptRequest) (domain.StopFact, error) {
	if d.Phones == nil {
		return domain.StopFact{}, api.E("dependency_unavailable", "device_host_not_configured")
	}
	return d.Phones.Stop(ctx, q)
}

func phoneUI(state PhoneState) *PhoneUI {
	ui := &PhoneUI{Width: 360, Height: 640, ScrollOffset: state.ScrollOffset, Controls: []PhoneControl{}}
	switch state.Screen {
	case "home":
		ui.Controls = []PhoneControl{
			{ID: "open_notes", Kind: "button", Bounds: PhoneRect{X: 40, Y: 120, Width: 280, Height: 80}, Text: "Notes", Enabled: true},
			{ID: "open_settings", Kind: "button", Bounds: PhoneRect{X: 40, Y: 230, Width: 280, Height: 80}, Text: "Settings", Enabled: true},
		}
	case "notes":
		ui.Controls = []PhoneControl{
			{ID: "note_editor", Kind: "text_input", Bounds: PhoneRect{X: 20, Y: 80, Width: 320, Height: 400}, Text: state.Note, Enabled: true, Focused: state.FocusedControl == "note_editor"},
			{ID: "done", Kind: "button", Bounds: PhoneRect{X: 260, Y: 560, Width: 80, Height: 60}, Text: "Done", Enabled: true},
		}
	case "settings":
		text := "Wi-Fi off"
		if state.WiFi {
			text = "Wi-Fi on"
		}
		ui.Controls = []PhoneControl{{ID: "wifi_toggle", Kind: "button", Bounds: PhoneRect{X: 40, Y: 140, Width: 280, Height: 80}, Text: text, Enabled: true}}
	}
	return ui
}

func containsPhonePoint(bounds PhoneRect, point *PhonePoint) bool {
	return point != nil && point.X >= bounds.X && point.X < bounds.X+bounds.Width && point.Y >= bounds.Y && point.Y < bounds.Y+bounds.Height
}

func navigatePhone(state *PhoneState, screen string) error {
	if len(state.BackStack) >= 8 {
		return api.E("overloaded", "gui_navigation_limit")
	}
	state.BackStack = append(state.BackStack, state.Screen)
	state.Screen = screen
	state.FocusedControl = ""
	state.ScrollOffset = 0
	return nil
}

func applyPhoneGesture(state *PhoneState, args PhoneGUIArguments) error {
	switch args.Action {
	case "click":
		for _, control := range phoneUI(*state).Controls {
			if !control.Enabled || !containsPhonePoint(control.Bounds, args.Point) {
				continue
			}
			switch control.ID {
			case "open_notes":
				return navigatePhone(state, "notes")
			case "open_settings":
				return navigatePhone(state, "settings")
			case "note_editor":
				state.FocusedControl = control.ID
			case "done":
				state.FocusedControl = ""
			case "wifi_toggle":
				state.WiFi = !state.WiFi
			}
			return nil
		}
		return api.E("invalid_state", "gui_control_not_found")
	case "swipe":
		bounds := PhoneRect{X: 20, Y: 80, Width: 320, Height: 400}
		if state.Screen != "notes" || !containsPhonePoint(bounds, args.From) || !containsPhonePoint(bounds, args.To) || args.From.X != args.To.X || args.From.Y == args.To.Y {
			return api.E("invalid_state", "gui_swipe_not_supported")
		}
		if args.From.Y > args.To.Y {
			state.ScrollOffset = min(uint64(4096), state.ScrollOffset+args.From.Y-args.To.Y)
		} else {
			distance := args.To.Y - args.From.Y
			state.ScrollOffset -= min(state.ScrollOffset, distance)
		}
	case "input":
		if state.Screen != "notes" || state.FocusedControl != "note_editor" || args.Text == nil {
			return api.E("invalid_state", "gui_input_not_focused")
		}
		if len(*args.Text) > 4096 {
			return api.E("invalid_request", "gui_text_limit")
		}
		state.Note = *args.Text
	case "back":
		if state.FocusedControl != "" {
			state.FocusedControl = ""
		} else if len(state.BackStack) > 0 {
			state.Screen = state.BackStack[len(state.BackStack)-1]
			state.BackStack = state.BackStack[:len(state.BackStack)-1]
			state.ScrollOffset = 0
		} else if state.Screen != "home" {
			state.Screen = "home"
			state.ScrollOffset = 0
		} else {
			return api.E("invalid_state", "gui_back_unavailable")
		}
	default:
		return api.E("unsupported", "device_action_not_supported")
	}
	return nil
}

var _ domain.Driver = (*PhoneGUIDriver)(nil)
