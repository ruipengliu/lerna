package interaction_test

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 故障屏障只控制公开 owner 门禁的并发时序；资格和所有行锁均来自真实 Task 用例。
type requestPairBarrier struct {
	owner    requestBridge
	arrivals atomic.Int32
	both     chan struct{}
}

type requestBeforeCheck struct {
	owner            requestBridge
	once             sync.Once
	entered, proceed chan struct{}
}

func (b *requestBeforeCheck) CheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ObjectRef) (interaction.RequestView, error) {
	b.once.Do(func() { close(b.entered) })
	select {
	case <-b.proceed:
	case <-ctx.Done():
		return interaction.RequestView{}, ctx.Err()
	}
	return b.owner.CheckTx(ctx, tx, auth, ref)
}

func TestPostgresSurfaceUpdateRechecksPeekAfterTaskGateWithoutReverseLock(t *testing.T) {
	if os.Getenv("HARNESS_INTERACTION_STORE") != "postgres" {
		t.Skip("actual PostgreSQL fixture required")
	}
	f := newApplication(t)
	goal, question, snapshot := f.upload(t, "准确目标"), f.upload(t, "问题"), f.upload(t, `{"title":"当前快照"}`)
	id := api.NewID("task")
	r := f.command(t, "task.submit", id, nil, task.SubmitInput{OrchestratorID: f.scope.OwnerID, GoalRef: goal, PolicyRef: f.policy, Deadline: api.Time(time.Now().Add(10 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
	var taskOut task.TaskOutput
	if err := api.Decode(r.Output, &taskOut); err != nil {
		t.Fatal(err)
	}
	trusted := f.auth
	trusted.Roles = append(trusted.Roles, "service")
	goalVersion := uint64(1)
	request := api.InputRequest{RequestID: api.NewID("request"), TargetRef: taskOut.TaskRef, GoalRevision: &goalVersion, Purpose: "clarify_goal", QuestionRef: question, AnswerSchemaRef: f.answerSchema, PreviewRefs: []api.ContentRef{goal}, ExpiresAt: api.Time(time.Now().Add(time.Minute)), State: "pending"}
	var ref api.ObjectRef
	status, err := f.store.Within(f.ctx, f.scope, f.appConfig.Participants, func(tx runtime.Tx) error {
		var err error
		ref, err = f.task.CreateInputTx(f.ctx, tx, trusted, id, request)
		return err
	})
	if status != runtime.Committed || err != nil {
		t.Fatalf("create request %s %v", status, err)
	}
	surfaceID, pid := api.NewID("surface"), api.NewID("presentation")
	in := interaction.SurfaceInput{BindingRef: f.binding, SnapshotRef: snapshot, RequestRefs: []api.ObjectRef{ref}}
	f.command(t, "surface.create", surfaceID, nil, in)
	r = f.command(t, "presentation.open", pid, nil, interaction.OpenPresentationInput{EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), SurfaceRef: f.scope.Ref(surfaceID, 1)})
	var p interaction.Presentation
	if err = api.Decode(r.Output, &p); err != nil {
		t.Fatal(err)
	}
	r = f.command(t, "presentation.begin", pid, &p.Revision, interaction.BeginPresentationInput{IntentRevision: p.IntentRevision})
	if err = api.Decode(r.Output, &p); err != nil {
		t.Fatal(err)
	}
	other, err := f.openStore(f.scope.DatabaseID)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	gate := &requestBeforeCheck{owner: requestBridge{f.task}, entered: make(chan struct{}), proceed: make(chan struct{})}
	ports := f.appPorts
	ports.Requests = gate
	s, err := interaction.New(f.appConfig, ports)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	readDone := make(chan error, 1)
	go func() {
		_, err := s.ReadPresentation(ctx, f.store, f.scope, f.auth, pid, interaction.RenderReadInput{Generation: p.Generation, IntentRevision: p.IntentRevision})
		readDone <- err
	}()
	select {
	case <-gate.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	taskLocked := make(chan struct{})
	type result struct {
		status runtime.CommitStatus
		err    error
	}
	writeDone := make(chan result, 1)
	go func() {
		status, err := other.Within(ctx, f.scope, f.appConfig.Participants, func(tx runtime.Tx) error {
			if _, err := f.task.RequestViewTx(ctx, tx, f.auth, ref); err != nil {
				close(taskLocked)
				return err
			}
			close(taskLocked)
			one := uint64(1)
			_, err := f.s.UpdateSurfaceTx(ctx, tx, f.auth, api.Command{TargetID: surfaceID, ExpectedRevision: &one}, in)
			return err
		})
		writeDone <- result{status, err}
	}()
	select {
	case <-taskLocked:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(gate.proceed)
	w, readErr := <-writeDone, <-readDone
	if w.status != runtime.Committed || w.err != nil || !api.IsCode(readErr, "invalid_state") {
		t.Fatalf("route changed after Task gate: update=%s %v old-render=%v", w.status, w.err, readErr)
	}
	current, err := f.s.ReadSurface(ctx, f.store, f.scope, f.auth, surfaceID)
	if err != nil || current.Revision != 2 {
		t.Fatalf("current surface lost: %+v %v", current, err)
	}
}

func (b *requestPairBarrier) wait(ctx context.Context) error {
	n := b.arrivals.Add(1)
	if n == 2 {
		close(b.both)
	}
	if n <= 2 {
		select {
		case <-b.both:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (b *requestPairBarrier) CheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ObjectRef) (interaction.RequestView, error) {
	v, err := b.owner.CheckTx(ctx, tx, auth, ref)
	if err == nil {
		err = b.wait(ctx)
	}
	return v, err
}
func (b *requestPairBarrier) CheckBatchTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, refs []api.ObjectRef) ([]interaction.RequestView, error) {
	if err := b.wait(ctx); err != nil {
		return nil, err
	}
	views, err := b.owner.s.RequestViewsTx(ctx, tx, auth, refs)
	out := make([]interaction.RequestView, len(views))
	for i, v := range views {
		out[i] = interaction.RequestView{Request: v.Request, AnswerSchema: v.AnswerSchema, Method: "task.input"}
	}
	return out, err
}

func TestPostgresSurfaceRequestsInOppositeOrderUseOneOwnerBatch(t *testing.T) {
	if os.Getenv("HARNESS_INTERACTION_STORE") != "postgres" {
		t.Skip("actual PostgreSQL fixture required")
	}
	f := newApplication(t)
	goal, question, snapshot := f.upload(t, "两项准确目标"), f.upload(t, "回答问题"), f.upload(t, `{"title":"双请求表单"}`)
	trusted := f.auth
	trusted.Roles = append(trusted.Roles, "service")
	refs := make([]api.ObjectRef, 2)
	for i := range refs {
		id := api.NewID("task")
		r := f.command(t, "task.submit", id, nil, task.SubmitInput{OrchestratorID: f.scope.OwnerID, GoalRef: goal, PolicyRef: f.policy, Deadline: api.Time(time.Now().Add(10 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
		var out task.TaskOutput
		if err := api.Decode(r.Output, &out); err != nil {
			t.Fatal(err)
		}
		version := uint64(1)
		request := api.InputRequest{RequestID: api.NewID("request"), TargetRef: out.TaskRef, GoalRevision: &version, Purpose: "clarify_goal", QuestionRef: question, AnswerSchemaRef: f.answerSchema, PreviewRefs: []api.ContentRef{goal}, ExpiresAt: api.Time(time.Now().Add(time.Minute)), State: "pending"}
		status, err := f.store.Within(f.ctx, f.scope, f.appConfig.Participants, func(tx runtime.Tx) error {
			var err error
			refs[i], err = f.task.CreateInputTx(f.ctx, tx, trusted, id, request)
			return err
		})
		if status != runtime.Committed || err != nil {
			t.Fatalf("create request %s %v", status, err)
		}
	}
	presentations := make([]interaction.Presentation, 2)
	for i := range presentations {
		surfaceID, id := api.NewID("surface"), api.NewID("presentation")
		order := []api.ObjectRef{refs[i], refs[1-i]}
		f.command(t, "surface.create", surfaceID, nil, interaction.SurfaceInput{BindingRef: f.binding, SnapshotRef: snapshot, RequestRefs: order})
		r := f.command(t, "presentation.open", id, nil, interaction.OpenPresentationInput{EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), SurfaceRef: f.scope.Ref(surfaceID, 1)})
		var p interaction.Presentation
		if err := api.Decode(r.Output, &p); err != nil {
			t.Fatal(err)
		}
		r = f.command(t, "presentation.begin", id, &p.Revision, interaction.BeginPresentationInput{IntentRevision: p.IntentRevision})
		if err := api.Decode(r.Output, &presentations[i]); err != nil {
			t.Fatal(err)
		}
	}
	ports := f.appPorts
	ports.Requests = &requestPairBarrier{owner: requestBridge{f.task}, both: make(chan struct{})}
	s, err := interaction.New(f.appConfig, ports)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 2)
	for _, p := range presentations {
		go func(p interaction.Presentation) {
			v, err := s.ReadPresentation(ctx, f.store, f.scope, f.auth, p.PresentationID, interaction.RenderReadInput{Generation: p.Generation, IntentRevision: p.IntentRevision})
			if err == nil && len(v.Requests) != 2 {
				err = api.E("invalid_state", "incomplete_request_view")
			}
			done <- err
		}(p)
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("opposite original request order cannot form Task lock cycle: %v", err)
		}
	}
}
