package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type contentBridge struct {
	service *memory.Service
	auth    runtime.Auth
	policy  memory.Policy
}

func (b *contentBridge) Read(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref api.ContentRef) ([]byte, error) {
	return b.service.ReadBytes(ctx, scope, auth, ref, "task.input", "local")
}
func derivedID(prefix, value string) string { return prefix + "_" + api.Hash([]byte(value))[7:39] }
func (b *contentBridge) Publish(ctx context.Context, scope runtime.Scope, id, media string, body []byte) (api.ContentRef, error) {
	ref := api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: derivedID("content", id), Version: 1, Hash: api.Hash(body), MediaType: media, ByteLength: uint64(len(body))}
	return b.service.Upload(ctx, scope, b.auth, memory.PublicationRequest{ContentRef: ref, TransferID: id, ReserveCommandID: derivedID("command", id+"/reserve"), PutCommandID: derivedID("command", id+"/put"), PolicyRef: b.policy.PolicyRef, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(time.Now().Add(20 * time.Minute)), TransferDeadline: api.Time(time.Now().Add(10 * time.Minute))}, body)
}
func configureContent(t *testing.T, h *harness, b *contentBridge) {
	t.Helper()
	objects, err := objectstore.OpenLocal(t.TempDir(), memory.MaxContentBytes)
	if err != nil {
		t.Fatal(err)
	}
	b.service = memory.New(h.store, objects)
	b.auth = h.auth
	b.auth.Roles = []string{"content_admin"}
	values := memory.PolicyValues{Subjects: []string{h.auth.SubjectID}, Purposes: []string{"content.write", "task.input"}, Locations: []string{"local"}, RetainUntil: api.Time(time.Now().Add(time.Hour)), Continuous: true}
	digest, _ := api.Digest(values)
	b.policy, err = memory.NewPolicy(api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1.0.0", Digest: digest}, values)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.service.InstallPolicy(context.Background(), h.scope, b.auth, b.policy); err != nil {
		t.Fatal(err)
	}
}
func drainKind(t *testing.T, h *harness, kind string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		work, status, err := h.store.Claim(context.Background(), h.scope, api.NewID("worker"), []string{kind}, 1, time.Minute)
		if err != nil || status != runtime.Committed {
			t.Fatalf("claim %+v %v", status, err)
		}
		if len(work) == 0 {
			return
		}
		fn, ok := h.dispatch.Registry.Job(kind)
		if !ok {
			t.Fatalf("missing %s", kind)
		}
		if err = fn(context.Background(), h.store, h.scope, work[0]); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("unbounded job churn")
}
func TestInvalidAnswerDoesNotConsumeRequestAndCorrectedAnswerPublishesCompleteGoal(t *testing.T) {
	content := &contentBridge{}
	h := newHarness(t, task.Ports{Content: content})
	configureContent(t, h, content)
	schema := api.Object(map[string]any{"path": api.String()}, "path")
	digest, _ := api.Digest(schema)
	schemaRef := api.ComponentRef{ComponentID: api.NewID("schema"), Version: "1.0.0", Digest: digest}
	s, err := task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, AnswerSchemas: []task.AnswerSchemaDefinition{{Ref: schemaRef, Schema: schema}}}, task.Ports{Content: content})
	if err != nil {
		t.Fatal(err)
	}
	h.service = s
	h.dispatch.Registry = runtime.NewRegistry()
	if err = s.Register(h.dispatch.Registry); err != nil {
		t.Fatal(err)
	}
	current := h.submit(t)
	goalRevision := current.GoalRevision
	req := api.InputRequest{RequestID: api.NewID("request"), TargetRef: h.scope.Ref(current.TaskID, current.Revision), GoalRevision: &goalRevision, Purpose: "clarify_goal", QuestionRef: h.content("provide exact save path"), AnswerSchemaRef: schemaRef, PreviewRefs: []api.ContentRef{current.GoalRef}, ExpiresAt: api.Time(time.Now().Add(10 * time.Minute)), State: "pending"}
	var requestRef api.ObjectRef
	status, err := h.store.Within(context.Background(), h.scope, []string{"task"}, func(tx runtime.Tx) error {
		var e error
		requestRef, e = h.service.CreateInputTx(context.Background(), tx, h.trusted(), current.TaskID, req)
		return e
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("create request: %s %v", status, err)
	}
	read, err := h.query("input_request.read", requestRef.ObjectID, task.ReadInput{})
	if err != nil {
		t.Fatal(err)
	}
	var view task.InputRequestView
	if err = api.Decode(read, &view); err != nil {
		t.Fatal(err)
	}
	if !api.Equal(view.AnswerSchema, api.Raw(schema)) {
		t.Fatal("renderer received guessed schema")
	}
	invalid, err := content.Publish(context.Background(), h.scope, api.NewID("upload"), "application/json", []byte(`{"path":"/report.md","unapproved":true}`))
	if err != nil {
		t.Fatal(err)
	}
	badCommand := h.command("task.input", current.TaskID, nil, task.InputAnswer{TaskID: current.TaskID, RequestRef: requestRef, GoalRevision: goalRevision, AnswerRef: invalid})
	accepted, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(badCommand))
	if err != nil || accepted.Stage != "accepted" {
		t.Fatalf("bad input prepare: %+v %v", accepted, err)
	}
	drainKind(t, h, task.JobInput)
	rejected, err := h.dispatch.Lookup(context.Background(), h.auth, badCommand.CommandID)
	if err != nil || rejected.Stage != "rejected" || rejected.Error.Reason != "invalid_answer" {
		t.Fatalf("schema rejection %+v %v", rejected, err)
	}
	valid, err := content.Publish(context.Background(), h.scope, api.NewID("upload"), "application/json", []byte(`{"path":"/report.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	goodCommand := h.command("task.input", current.TaskID, nil, task.InputAnswer{TaskID: current.TaskID, RequestRef: requestRef, GoalRevision: goalRevision, AnswerRef: valid})
	accepted, err = h.dispatch.Command(context.Background(), h.auth, api.Raw(goodCommand))
	if err != nil || accepted.Stage != "accepted" {
		t.Fatalf("corrected input prepare: %+v %v", accepted, err)
	}
	drainKind(t, h, task.JobInput)
	applied, err := h.dispatch.Lookup(context.Background(), h.auth, goodCommand.CommandID)
	if err != nil || applied.Stage != "applied" {
		t.Fatalf("answer not consumed: %+v %v", applied, err)
	}
	after, err := h.service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if after.GoalRevision != 2 || after.GoalRef.MediaType != "application/json" || after.RequirementsState != "collecting" {
		t.Fatalf("answer overwrote or failed to rebuild goal: %+v", after)
	}
	body, err := content.Read(context.Background(), h.scope, h.auth, after.GoalRef)
	if err != nil {
		t.Fatal(err)
	}
	var document api.GoalDocument
	if err = api.Decode(body, &document); err != nil {
		t.Fatal(err)
	}
	if !api.Equal(document.InitialGoalRef, current.GoalRef) || len(document.AmendmentRefs) != 1 || !api.Equal(document.AmendmentRefs[0], valid) {
		t.Fatalf("goal lost original or exact answer: %+v", document)
	}
	old, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(h.command("task.input", current.TaskID, nil, task.InputAnswer{TaskID: current.TaskID, RequestRef: requestRef, GoalRevision: goalRevision, AnswerRef: valid})))
	if err != nil || old.Stage != "rejected" {
		t.Fatalf("old answer consumed twice: %+v %v", old, err)
	}
}
