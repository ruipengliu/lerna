package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const JobInput = "task.input"
const pendingInputs = "task.input_preparations"

func closedAnswerSchema(schema api.Schema) error {
	var inspect func(any) error
	inspect = func(v any) error {
		switch value := v.(type) {
		case map[string]any:
			if _, ok := value["$ref"]; ok {
				return fmt.Errorf("answer schemas must be self-contained")
			}
			if value["type"] == "object" && value["additionalProperties"] != false {
				return fmt.Errorf("answer object schema must be closed")
			}
			for _, x := range value {
				if e := inspect(x); e != nil {
					return e
				}
			}
		case []any:
			for _, x := range value {
				if e := inspect(x); e != nil {
					return e
				}
			}
		}
		return nil
	}
	return inspect(schema)
}
func (s *Service) RequestViewTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ObjectRef) (InputRequestView, error) {
	if err := runtime.CheckRef(tx.Scope(), ref); err != nil {
		return InputRequestView{}, err
	}
	if ref.OwnerID != tx.Scope().OwnerID {
		return InputRequestView{}, api.E("forbidden", "request_owner_mismatch")
	}
	var req api.InputRequest
	rev, e := tx.Get(ctx, inputs, ref.ObjectID, &req)
	if e != nil {
		return InputRequestView{}, e
	}
	if ref.Revision != rev {
		return InputRequestView{}, api.E("revision_conflict", "wrong_request_version")
	}
	t, e := getTask(ctx, tx, req.TargetRef.ObjectID)
	if e != nil {
		return InputRequestView{}, e
	}
	if e = principal(auth, t); e != nil {
		return InputRequestView{}, e
	}
	schema, ok := s.answerSchemas[componentKey(req.AnswerSchemaRef)]
	if !ok {
		return InputRequestView{}, api.E("unsupported", "answer_schema_not_registered")
	}
	return InputRequestView{RequestRef: tx.Scope().Ref(req.RequestID, req.Revision), Request: req, AnswerSchema: api.Raw(schema)}, nil
}
func (s *Service) InputRequestRead(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, id string, revision uint64) (InputRequestView, error) {
	var req api.InputRequest
	rev, e := store.Read(ctx, scope, inputs, id, revision, &req)
	if e != nil {
		return InputRequestView{}, e
	}
	if _, e = s.readState(ctx, store, scope, auth, req.TargetRef.ObjectID, 0); e != nil {
		return InputRequestView{}, e
	}
	schema, ok := s.answerSchemas[componentKey(req.AnswerSchemaRef)]
	if !ok {
		return InputRequestView{}, api.E("unsupported", "answer_schema_not_registered")
	}
	return InputRequestView{RequestRef: scope.Ref(id, rev), Request: req, AnswerSchema: api.Raw(schema)}, nil
}
func (s *Service) InputRequestList(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, in InputRequestListInput) (api.Page[InputRequestView], error) {
	if in.Limit < 1 || in.Limit > 100 {
		return api.Page[InputRequestView]{}, invalid("invalid_list")
	}
	t, e := s.readState(ctx, store, scope, auth, in.TaskID, 0)
	if e != nil {
		return api.Page[InputRequestView]{}, e
	}
	last, e := parseCursor(scope, auth, in.Cursor, "input_request", t.Task.Revision, in.TaskID)
	if e != nil {
		return api.Page[InputRequestView]{}, e
	}
	rows, e := store.List(ctx, scope, inputs, in.TaskID, last, int(in.Limit)+1)
	if e != nil {
		return api.Page[InputRequestView]{}, e
	}
	out := api.Page[InputRequestView]{Items: []InputRequestView{}, CollectionRevision: t.Task.Revision, Exhausted: len(rows) <= int(in.Limit), Gaps: []string{}}
	if len(rows) > int(in.Limit) {
		rows = rows[:in.Limit]
	}
	for _, row := range rows {
		var req api.InputRequest
		if e = row.Decode(&req); e != nil {
			return out, e
		}
		schema, ok := s.answerSchemas[componentKey(req.AnswerSchemaRef)]
		if !ok {
			return out, api.E("unsupported", "answer_schema_not_registered")
		}
		out.Items = append(out.Items, InputRequestView{RequestRef: scope.Ref(row.ID, row.Revision), Request: req, AnswerSchema: api.Raw(schema)})
		last = row.ID
	}
	if !out.Exhausted {
		out.NextCursor = makeCursor(cursor{Owner: scope.OwnerID, Tenant: scope.TenantID, Subject: auth.SubjectID, Kind: "input_request", Revision: t.Task.Revision, Last: last, Filter: in.TaskID})
	}
	return out, nil
}
func (s *Service) PrepareInputTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in InputAnswer) (InputOutput, error) {
	if e := target(c, in.TaskID); e != nil {
		return InputOutput{}, e
	}
	view, e := s.RequestViewTx(ctx, tx, auth, in.RequestRef)
	if e != nil {
		return InputOutput{}, e
	}
	req := view.Request
	if req.TargetRef.ObjectID != in.TaskID || req.GoalRevision == nil || *req.GoalRevision != in.GoalRevision {
		return InputOutput{}, api.E("revision_conflict", "wrong_goal")
	}
	if req.State != "pending" {
		return InputOutput{}, api.E("invalid_state", "already_consumed")
	}
	if req.Purpose == "accept_quality" {
		return InputOutput{}, api.E("invalid_state", "acceptance_method_required")
	}
	t, e := getTask(ctx, tx, in.TaskID)
	if e != nil {
		return InputOutput{}, e
	}
	if terminal(t) {
		return InputOutput{}, api.E("invalid_state", "target_terminal")
	}
	if t.Task.GoalRevision != in.GoalRevision {
		return InputOutput{}, api.E("revision_conflict", "wrong_goal")
	}
	if s.ports.Content == nil {
		return InputOutput{}, api.E("dependency_unavailable", "content_unavailable")
	}
	if e = s.authorize(ctx, tx, auth, "task.input", []api.ContentRef{in.AnswerRef}, []api.ObjectRef{in.RequestRef}); e != nil {
		return InputOutput{}, e
	}
	key := refKey(in.RequestRef) + fmt.Sprintf("/%020d", in.RequestRef.Revision)
	if _, e = tx.LookupKey(ctx, pendingInputs, key); e == nil {
		return InputOutput{}, api.E("invalid_state", "input_prepare_pending")
	}
	if !confirmedNotFound(e) {
		return InputOutput{}, e
	}
	pending := pendingInput{Revision: 1, CommandID: c.CommandID, Input: in, Auth: auth, State: "pending"}
	if e = tx.Create(ctx, pendingInputs, c.CommandID, in.TaskID, pending); e != nil {
		return InputOutput{}, e
	}
	digest, e := api.Digest(in)
	if e != nil {
		return InputOutput{}, e
	}
	if e = tx.Bind(ctx, pendingInputs, key, c.CommandID, digest); e != nil {
		return InputOutput{}, e
	}
	if _, e = raise(ctx, tx, JobInput, "input/"+c.CommandID, tx.Scope().Ref(c.CommandID, 1)); e != nil {
		return InputOutput{}, e
	}
	return InputOutput{TaskRef: taskRef(tx, t), RequestRef: in.RequestRef, State: "validating"}, nil
}
func (s *Service) inputJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var pending pendingInput
	if _, e := store.Read(ctx, scope, pendingInputs, work.Job.SourceRef.ObjectID, 0, &pending); e != nil {
		return e
	}
	if pending.State != "pending" {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if s.ports.Content == nil {
		return s.wait(ctx, store, scope, work)
	}
	view, err := s.InputRequestRead(ctx, store, scope, pending.Auth, pending.Input.RequestRef.ObjectID, 0)
	if err != nil {
		return err
	}
	var validationErr error
	if view.Request.Revision != pending.Input.RequestRef.Revision || view.Request.State != "pending" {
		validationErr = api.E("revision_conflict", "wrong_request_version")
	}
	if validationErr == nil {
		if e := s.preIO(ctx, store, scope, work); e != nil {
			return e
		}
		body, e := s.ports.Content.Read(ctx, scope, pending.Auth, pending.Input.AnswerRef)
		if e != nil {
			if deferred(e) {
				return s.wait(ctx, store, scope, work)
			}
			validationErr = e
		} else {
			schema := api.Schema{}
			if e = api.Decode(view.AnswerSchema, &schema); e != nil {
				return e
			}
			validator, e := api.NewValidator(schema)
			if e != nil {
				return e
			}
			if strings.HasPrefix(pending.Input.AnswerRef.MediaType, "text/") {
				body = api.Raw(string(body))
			}
			if e = validator.Validate(body); e != nil {
				validationErr = api.E("invalid_request", "invalid_answer")
			}
		}
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		var current pendingInput
		rev, e := tx.Get(ctx, pendingInputs, pending.CommandID, &current)
		if e != nil {
			return e
		}
		if current.State != "pending" {
			return nil
		}
		var out InputOutput
		decisionErr := validationErr
		if decisionErr == nil {
			decisionErr = tx.Savepoint(ctx, func(inner runtime.Tx) error {
				var e error
				out, e = s.ConsumeInputTx(ctx, inner, pending.Auth, api.Command{TargetID: pending.Input.TaskID, CommandID: pending.CommandID}, pending.Input)
				return e
			})
		}
		if decisionErr != nil {
			var business *api.Error
			if !errors.As(decisionErr, &business) {
				return decisionErr
			}
			if deferred(decisionErr) {
				return decisionErr
			}
			current.State = "rejected"
			if e = runtime.Decide(ctx, tx, pending.CommandID, nil, business); e != nil {
				return e
			}
		} else {
			current.State = "consumed"
			if e = runtime.Decide(ctx, tx, pending.CommandID, out, nil); e != nil {
				return e
			}
		}
		current.Revision++
		return tx.Put(ctx, pendingInputs, pending.CommandID, rev, current)
	})
}
func (s *Service) Closure(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, ref api.ObjectRef) (ClosureView, error) {
	if e := runtime.CheckRef(scope, ref); e != nil {
		return ClosureView{}, e
	}
	if ref.OwnerID != scope.OwnerID {
		return ClosureView{}, api.E("forbidden", "task_owner_mismatch")
	}
	var out ClosureView
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		t, e := getTask(ctx, tx, ref.ObjectID)
		if e != nil {
			return e
		}
		if e = principal(auth, t); e != nil {
			return e
		}
		rows, e := s.fullRelations(ctx, tx, t.Task.TaskID)
		if e != nil {
			return e
		}
		effects := true
		for _, r := range rows {
			if r.Kind == "operation" && (!r.Closed || r.MayApplyLater || r.Effect == "unknown") {
				effects = false
			}
			if r.Kind == "delegation" && !r.Closed {
				effects = false
			}
			if r.Kind == "child" {
				child, e := getTask(ctx, tx, r.Ref.ObjectID)
				if e != nil {
					return e
				}
				if !terminal(child) {
					effects = false
				}
			}
		}
		out = ClosureView{TaskRef: taskRef(tx, t), GoalWorkClosed: terminal(t), EffectsClosed: effects, AccountingOpen: t.Task.AccountingOpen, ProofRef: t.Task.GoalRef}
		return nil
	})
	return out, err
}
