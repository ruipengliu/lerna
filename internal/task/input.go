package task

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
			for _, keyword := range []string{"$ref", "$dynamicRef", "$recursiveRef", "$id"} {
				if _, ok := value[keyword]; ok {
					return fmt.Errorf("answer schemas must be self-contained")
				}
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
	views, err := s.RequestViewsTx(ctx, tx, auth, []api.ObjectRef{ref})
	if err != nil {
		return InputRequestView{}, err
	}
	return views[0], nil
}

// RequestViewsTx仅供同库受信消费方使用；所有Task先于所有InputRequest锁定。
// 不可变birth与准确历史Task负责路由，不能由调用方自报锁集合。
func (s *Service) RequestViewsTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, refs []api.ObjectRef) ([]InputRequestView, error) {
	if len(refs) < 1 || len(refs) > 20 {
		return nil, invalid("request_batch_limit")
	}
	seen := map[string]bool{}
	births := make([]api.InputRequest, len(refs))
	paths := map[string][]string{}
	order := make([]int, len(refs))
	for i, ref := range refs {
		if err := runtime.CheckRef(tx.Scope(), ref); err != nil {
			return nil, err
		}
		if ref.OwnerID != tx.Scope().OwnerID {
			return nil, api.E("forbidden", "request_owner_mismatch")
		}
		if seen[ref.ObjectID] {
			return nil, invalid("duplicate_request")
		}
		seen[ref.ObjectID] = true
		if err := tx.GetVersion(ctx, inputs, ref.ObjectID, 1, &births[i]); err != nil {
			return nil, err
		}
		birth := births[i]
		if err := runtime.CheckRef(tx.Scope(), birth.TargetRef); err != nil {
			return nil, err
		}
		if birth.TargetRef.OwnerID != tx.Scope().OwnerID {
			return nil, api.E("forbidden", "input_target_owner_mismatch")
		}
		var routing taskState
		if err := tx.GetVersion(ctx, tasks, birth.TargetRef.ObjectID, birth.TargetRef.Revision, &routing); err != nil {
			return nil, err
		}
		if uint64(len(routing.Ancestors)) >= routing.Policy.MaxDepth || routing.Task.TaskID != birth.TargetRef.ObjectID {
			return nil, api.E("invalid_state", "input_task_lineage_incomplete")
		}
		path := append(append([]string{}, routing.Ancestors...), routing.Task.TaskID)
		pathIDs := map[string]bool{}
		for depth, id := range path {
			if pathIDs[id] {
				return nil, api.E("invalid_state", "input_task_lineage_cycle")
			}
			pathIDs[id] = true
			prefix := path[:depth+1]
			if existing, ok := paths[id]; ok && !api.Equal(existing, prefix) {
				return nil, api.E("invalid_state", "input_task_lineage_conflict")
			}
			paths[id] = prefix
		}
		order[i] = i
	}
	ids := make([]string, 0, len(paths))
	for id := range paths {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if len(paths[ids[i]]) == len(paths[ids[j]]) {
			return ids[i] < ids[j]
		}
		return len(paths[ids[i]]) < len(paths[ids[j]])
	})
	current := map[string]taskState{}
	for _, id := range ids {
		t, err := getTask(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		path := paths[id]
		if !api.Equal(t.Ancestors, path[:len(path)-1]) {
			return nil, api.E("revision_conflict", "input_task_lineage_changed")
		}
		current[id] = t
	}
	for _, birth := range births {
		if err := principal(auth, current[birth.TargetRef.ObjectID]); err != nil {
			return nil, err
		}
	}
	sort.Slice(order, func(i, j int) bool { return refs[order[i]].ObjectID < refs[order[j]].ObjectID })
	out := make([]InputRequestView, len(refs))
	for _, i := range order {
		ref := refs[i]
		var req api.InputRequest
		revision, err := tx.Get(ctx, inputs, ref.ObjectID, &req)
		if err != nil {
			return nil, err
		}
		if ref.Revision != revision {
			return nil, api.E("revision_conflict", "wrong_request_version")
		}
		if !api.Equal(req.TargetRef, births[i].TargetRef) || req.RequestID != ref.ObjectID || req.Revision != revision {
			return nil, api.E("invalid_state", "input_target_changed")
		}
		schema, ok := s.answerSchemas[componentKey(req.AnswerSchemaRef)]
		if !ok {
			return nil, api.E("unsupported", "answer_schema_not_registered")
		}
		out[i] = InputRequestView{RequestRef: tx.Scope().Ref(req.RequestID, req.Revision), Request: req, AnswerSchema: api.Raw(schema)}
	}
	return out, nil
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
func (s *Service) prepareInputTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in InputAnswer) (InputOutput, error) {
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
	var head inputHead
	headRev, e := tx.Get(ctx, inputHeads, key, &head)
	if e == nil {
		var previous pendingInput
		if _, e = tx.Get(ctx, pendingInputs, head.CommandID, &previous); e != nil {
			return InputOutput{}, e
		}
		if previous.State == "pending" {
			return InputOutput{}, api.E("invalid_state", "input_prepare_pending")
		}
		if previous.State == "consumed" {
			return InputOutput{}, api.E("invalid_state", "already_consumed")
		}
		head.CommandID = c.CommandID
		head.Revision++
		if e = tx.Put(ctx, inputHeads, key, headRev, head); e != nil {
			return InputOutput{}, e
		}
	} else if confirmedNotFound(e) {
		if e = tx.Create(ctx, inputHeads, key, in.TaskID, inputHead{Revision: 1, CommandID: c.CommandID}); e != nil {
			return InputOutput{}, e
		}
	} else {
		return InputOutput{}, e
	}

	pending := pendingInput{Revision: 1, CommandID: c.CommandID, UploadID: api.NewID("upload"), Input: in, Auth: auth, State: "pending"}
	if e = tx.Create(ctx, pendingInputs, c.CommandID, in.TaskID, pending); e != nil {
		return InputOutput{}, e
	}

	if e = queueJob(ctx, tx, JobInput, "input/"+c.CommandID, tx.Scope().Ref(c.CommandID, 1)); e != nil {
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
	var completeGoal *api.ContentRef
	if validationErr == nil && view.Request.Purpose == "clarify_goal" {
		var current taskState
		if _, err = store.Read(ctx, scope, tasks, pending.Input.TaskID, 0, &current); err != nil {
			return err
		}
		if len(current.Amendments) >= 100 {
			return api.E("invalid_request", "goal_amendment_limit")
		}
		document := api.GoalDocument{FormatVersion: 1, InitialGoalRef: current.InitialGoalRef, AmendmentRefs: append(append([]api.ContentRef{}, current.Amendments...), pending.Input.AnswerRef)}
		if e := s.preIO(ctx, store, scope, work); e != nil {
			return e
		}
		ref, e := s.ports.Content.Publish(ctx, scope, pending.UploadID, "application/json", api.Raw(document))
		if e != nil {
			if deferred(e) {
				return s.wait(ctx, store, scope, work)
			}
			return e
		}
		completeGoal = &ref
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		if _, e := tx.LoadCommand(ctx, pending.CommandID); e != nil {
			return e
		}
		if e := s.lockTaskTree(ctx, tx, pending.Input.TaskID); e != nil {
			return e
		}
		var current pendingInput
		rev, e := tx.Get(ctx, pendingInputs, pending.CommandID, &current)
		if e != nil {
			return e
		}
		if current.CommandID != pending.CommandID || current.UploadID != pending.UploadID || !api.Equal(current.Input, pending.Input) || !api.Equal(current.Auth, pending.Auth) {
			return api.E("idempotency_conflict", "original_input_preparation_changed")
		}
		if current.State != "pending" {
			return nil
		}
		var out InputOutput
		decisionErr := validationErr
		if decisionErr == nil {
			decisionErr = tx.Savepoint(ctx, func(inner runtime.Tx) error {
				var e error
				out, e = s.ConsumeInputTx(ctx, inner, pending.Auth, api.Command{TargetID: pending.Input.TaskID, CommandID: pending.CommandID}, pending.Input, completeGoal)
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

// 这里只固定已完整收束快照的首次来源证明；它不是当前启动许可。
// 每次查询仍先核当次 Task 与全部关系，迟到的新事实拥有自己的快照键。
type closedTaskProof struct {
	TaskRef        api.ObjectRef  `json:"task_ref"`
	SnapshotDigest string         `json:"snapshot_digest"`
	IssuedAt       string         `json:"issued_at"`
	ProofRef       api.ContentRef `json:"proof_ref"`
}

// 宿主可再次核原签封记录及出版拒绝，不能在此出站、重签或创建新责任。
type closureProofValidator interface {
	CheckClosureProofTx(context.Context, runtime.Tx, ClosureView) error
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
		if s.ports.ClosureProof == nil {
			return api.E("unsupported", "closure_proof_not_configured")
		}
		rows, e := s.closureRelationsTx(ctx, tx, t.Task.TaskID)
		if e != nil {
			return e
		}
		effects := true
		children := []api.Task{}
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
				children = append(children, child.Task)
			}
		}
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		digest, e := api.Digest(struct {
			Task      api.Task   `json:"task"`
			Relations []relation `json:"relations"`
			Children  []api.Task `json:"children"`
		}{t.Task, rows, children})
		if e != nil {
			return e
		}
		refs := []api.ObjectRef{taskRef(tx, t)}
		for _, r := range rows {
			refs = append(refs, r.Ref)
		}
		for _, child := range children {
			refs = append(refs, tx.Scope().Ref(child.TaskID, child.Revision))
		}
		out = ClosureView{TaskRef: taskRef(tx, t), GoalWorkClosed: terminal(t), EffectsClosed: effects, AccountingOpen: t.Task.AccountingOpen, IssuedAt: api.Time(now), SnapshotDigest: digest, EvidenceRefs: refs}
		closed := out.GoalWorkClosed && out.EffectsClosed && !out.AccountingOpen
		sealKey := t.Task.TaskID + "/" + digest
		if closed {
			var original closedTaskProof
			e = tx.GetVersion(ctx, "task.closed_snapshot_proofs", sealKey, 1, &original)
			if e == nil {
				if original.TaskRef != out.TaskRef || original.SnapshotDigest != out.SnapshotDigest {
					return api.E("idempotency_conflict", "original_closed_snapshot_changed")
				}
				if _, e = api.ParseTime(original.IssuedAt); e != nil {
					return e
				}
				out.IssuedAt, out.ProofRef = original.IssuedAt, original.ProofRef
				if e = s.checkSourceProof(tx.Scope(), out.ProofRef); e != nil {
					return e
				}
				if validator, ok := s.ports.ClosureProof.(closureProofValidator); ok {
					return validator.CheckClosureProofTx(ctx, tx, out)
				}
				return nil
			} else if !confirmedNotFound(e) {
				return e
			}
		}
		out.ProofRef, e = s.ports.ClosureProof.SealClosureTx(ctx, tx, out)
		if e != nil {
			return e
		}
		if e = s.checkSourceProof(tx.Scope(), out.ProofRef); e != nil {
			return e
		}
		if closed {
			return tx.Create(ctx, "task.closed_snapshot_proofs", sealKey, t.Task.TaskID, closedTaskProof{TaskRef: out.TaskRef, SnapshotDigest: out.SnapshotDigest, IssuedAt: out.IssuedAt, ProofRef: out.ProofRef})
		}
		return nil
	})
	return out, err
}

// 关闭判断必须含已终态孩子的真实未结效果；每一关系集和子树总量均有界。
func (s *Service) closureRelationsTx(ctx context.Context, tx runtime.Tx, taskID string) ([]relation, error) {
	queue := []string{taskID}
	seen := map[string]bool{}
	result := []relation{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			return nil, api.E("invalid_state", "task_relation_cycle")
		}
		seen[id] = true
		if uint64(len(seen)) > s.config.MaxActiveSubtree+1 {
			return nil, api.E("overloaded", "closure_subtree_incomplete")
		}
		rows, err := s.fullRelations(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			result = append(result, r)
			if r.Kind == "child" {
				queue = append(queue, r.Ref.ObjectID)
			}
		}
	}
	return result, nil
}

const inputHeads = "task.input_preparation_heads"

type inputHead struct {
	Revision  uint64 `json:"revision"`
	CommandID string `json:"command_id"`
}
