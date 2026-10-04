package interaction_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// The registered modern default schema is the original request authority.
func TestModernDefaultGoalSchemaRendersOriginalInputRequest(t *testing.T) {
	fixture, err := os.ReadFile("../../sdk/ts/src/modern-goal-schema.fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema api.Schema
	if err = api.Decode(fixture, &schema); err != nil || !api.Equal(schema, brain.GoalSchema()) {
		t.Fatal("SDK modern fixture diverges from registered default GoalSchema", err)
	}
	f := newApplicationWithGoalSchema(t, brain.GoalSchema())
	goal, question, snapshot := f.upload(t, "需要补充目标"), f.upload(t, "请选择目标和已授权偏好引用"), f.upload(t, `{"title":"默认目标表单"}`)
	id := api.NewID("task")
	receipt := f.command(t, "task.submit", id, nil, task.SubmitInput{OrchestratorID: f.scope.OwnerID, GoalRef: goal, PolicyRef: f.policy, Deadline: api.Time(time.Now().Add(20 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
	var submitted task.TaskOutput
	if err := api.Decode(receipt.Output, &submitted); err != nil {
		t.Fatal(err)
	}
	actual, err := f.task.Read(f.ctx, f.store, f.scope, f.auth, id)
	if err != nil {
		t.Fatal(err)
	}
	revision := actual.GoalRevision
	request := api.InputRequest{RequestID: api.NewID("request"), TargetRef: submitted.TaskRef, GoalRevision: &revision, Purpose: "clarify_goal", QuestionRef: question, AnswerSchemaRef: f.goalAnswerSchema, PreviewRefs: []api.ContentRef{goal}, ExpiresAt: api.Time(time.Now().Add(time.Minute)), State: "pending"}
	trusted := f.auth
	trusted.Roles = append(append([]string{}, trusted.Roles...), "service")
	var requestRef api.ObjectRef
	status, err := f.store.Within(f.ctx, f.scope, []string{"task", "content", "memory", "interaction"}, func(tx runtime.Tx) error {
		var err error
		requestRef, err = f.task.CreateInputTx(f.ctx, tx, trusted, id, request)
		return err
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("original modern request: %s %v", status, err)
	}
	surface, idp := api.NewID("surface"), api.NewID("presentation")
	f.command(t, "surface.create", surface, nil, interaction.SurfaceInput{BindingRef: f.binding, SnapshotRef: snapshot, RequestRefs: []api.ObjectRef{requestRef}})
	receipt = f.command(t, "presentation.open", idp, nil, interaction.OpenPresentationInput{EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), SurfaceRef: f.scope.Ref(surface, 1)})
	var presentation interaction.Presentation
	if err = api.Decode(receipt.Output, &presentation); err != nil {
		t.Fatal(err)
	}
	r := presentation.Revision
	receipt = f.command(t, "presentation.begin", idp, &r, interaction.BeginPresentationInput{IntentRevision: presentation.IntentRevision})
	if err = api.Decode(receipt.Output, &presentation); err != nil {
		t.Fatal(err)
	}
	read := interaction.RenderReadInput{Generation: presentation.Generation, IntentRevision: presentation.IntentRevision}
	view, err := f.s.ReadPresentation(f.ctx, f.store, f.scope, f.auth, idp, read)
	if err != nil {
		t.Fatalf("registered modern default InputRequest must render: %v", err)
	}
	if len(view.Requests) != 1 || len(view.Bodies) != 3 || view.Requests[0].Request.AnswerSchemaRef != f.goalAnswerSchema || !api.Equal(view.Requests[0].AnswerSchema, api.Raw(brain.GoalSchema())) {
		t.Fatal("renderer changed original default schema or omitted verified bodies")
	}
	digest, err := api.Digest(brain.GoalSchema())
	if err != nil || digest != f.goalAnswerSchema.Digest {
		t.Fatal("original schema digest changed", err)
	}
	r = presentation.Revision
	f.command(t, "presentation.close", idp, &r, interaction.ClosePresentationInput{Reason: "关闭原表单"})
	if _, err = f.s.ReadPresentation(f.ctx, f.store, f.scope, f.auth, idp, read); !api.IsCode(err, "invalid_state") {
		t.Fatalf("old modern form callback survived close: %v", err)
	}
}

// Registration is a public, zero-I/O renderer admission seam.
func TestModernBoundedFormRegistrationRejectsUnsafeExtensions(t *testing.T) {
	reference := brain.GoalSchema()["oneOf"].([]any)[1].(api.Schema)["properties"].(map[string]any)["preference"].(api.Schema)["properties"].(map[string]any)["query_ref"].(api.Schema)
	register := func(schema api.Schema) error {
		owner, tenant := api.NewID("owner"), api.NewID("tenant")
		_, err := interaction.New(interaction.Config{EventBindings: []interaction.EventBinding{{BindingRef: api.ObjectRef{TenantID: tenant, OwnerID: owner, ObjectID: api.NewID("binding"), Revision: 1}, Events: []interaction.EventRule{{Name: "submit", Schema: api.Raw(schema), OwnerID: owner, Method: "task.submit", TargetID: api.NewID("task"), AcceptForSeconds: 60}}}}}, interaction.Ports{})
		return err
	}
	if err := register(brain.GoalSchema()); err != nil {
		t.Fatal("original modern schema must gain renderer admission", err)
	}
	cases := map[string]api.Schema{
		"union_uniqueness_string":       {"oneOf": []any{api.Schema{"const": true}, api.Schema{"const": false}}, "uniqueItems": "x"},
		"union_uniqueness_compound":     {"oneOf": []any{api.Object(map[string]any{}), api.Object(map[string]any{})}, "uniqueItems": true},
		"empty_enum":                    {"type": "string", "enum": []any{}},
		"remote_ref":                    {"$ref": "https://other/schema"},
		"script":                        {"type": "string", "maxLength": 10, "script": "run()"},
		"arbitrary_pattern":             {"type": "string", "maxLength": 100, "pattern": "(a+)+$"},
		"hash_without_original_binding": {"type": "string", "pattern": "^sha256:[a-f0-9]{64}$"},
		"unbounded_integer":             {"type": "integer", "minimum": 1},
		"open_object":                   {"type": "object", "properties": map[string]any{}},
		"unbounded_unique_array":        {"type": "array", "items": api.Enum("plain", "bullet"), "uniqueItems": true},
		"compound_unique_array":         {"type": "array", "maxItems": 2, "uniqueItems": true, "items": api.Object(map[string]any{"body": api.String()}, "body")},
		"nonchoice_unique_array":        {"type": "array", "maxItems": 2, "uniqueItems": true, "items": api.String()},
		"mixed_enum":                    {"enum": []any{"plain", true}},
		"compound_enum":                 {"enum": []any{api.Schema{"x": true}}},
		"unsafe_enum":                   {"type": "number", "enum": []any{float64(api.MaxSafeInteger) + 1}},
		"fractional_integer_enum":       {"type": "integer", "enum": []any{1.5}},
	}
	for name, change := range map[string]func(api.Schema){
		"different_hash_pattern": func(s api.Schema) { s["properties"].(map[string]any)["hash"].(map[string]any)["pattern"] = "^.*$" },
		"omitted_ref_owner":      func(s api.Schema) { delete(s["properties"].(map[string]any), "owner_id") },
		"additional_ref_field":   func(s api.Schema) { s["properties"].(map[string]any)["url"] = api.String() },
		"larger_ref_byte_limit": func(s api.Schema) {
			s["properties"].(map[string]any)["byte_length"].(map[string]any)["maximum"] = 32 << 20
		},
	} {
		var mutated api.Schema
		if err := api.Decode(api.Raw(reference), &mutated); err != nil {
			t.Fatal(err)
		}
		change(mutated)
		cases[name] = mutated
	}
	wide := map[string]any{}
	for i := 0; i < 40; i++ {
		wide[fmt.Sprintf("ref_%d", i)] = reference
	}
	cases["reference_node_budget"] = api.Object(wide)
	deep := reference
	for i := 0; i < 8; i++ {
		deep = api.Object(map[string]any{"ref": deep}, "ref")
	}
	cases["reference_depth_budget"] = deep
	for name, schema := range cases {
		t.Run(name, func(t *testing.T) {
			err := register(schema)
			if err == nil {
				t.Fatal("unsafe schema gained renderer admission")
			}
		})
	}
}
