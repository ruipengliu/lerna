package interaction

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type taskSubmit struct {
	OrchestratorID      string           `json:"orchestrator_id"`
	GoalRef             api.ContentRef   `json:"goal_ref"`
	PolicyRef           api.ComponentRef `json:"policy_ref"`
	Deadline            string           `json:"deadline"`
	Budget              []api.Amount     `json:"budget"`
	SourceSubmissionRef *api.ObjectRef   `json:"source_submission_ref,omitempty"`
}
type taskSteer struct {
	TaskID              string         `json:"task_id"`
	BaseGoalRevision    uint64         `json:"base_goal_revision"`
	AmendmentRef        api.ContentRef `json:"amendment_ref"`
	SourceSubmissionRef api.ObjectRef  `json:"source_submission_ref"`
	PrepareDeadline     string         `json:"prepare_deadline"`
}
type taskAnswer struct {
	TaskID       string         `json:"task_id"`
	RequestRef   api.ObjectRef  `json:"request_ref"`
	GoalRevision uint64         `json:"goal_revision"`
	AnswerRef    api.ContentRef `json:"answer_ref"`
}
type orderRecord struct {
	Revision uint64   `json:"revision"`
	Pending  []string `json:"pending"`
}
type WithdrawInput struct {
	Reason string `json:"reason"`
}
type ReplyInput struct {
	SubmissionRef api.ObjectRef  `json:"submission_ref"`
	ContentRef    api.ContentRef `json:"content_ref"`
	Role          string         `json:"role"`
}
type ReplyOutput struct {
	MessageRef api.ObjectRef `json:"message_ref"`
	BranchRef  api.ObjectRef `json:"branch_ref"`
}

func (s *Service) content(ctx context.Context, tx runtime.Tx, a runtime.Auth, ref api.ContentRef, purpose string) error {
	if s.ports.Content == nil {
		return api.E("unsupported", "content_gate_unconfigured")
	}
	if err := api.ValidateRecord("ContentRef", ref); err != nil {
		return err
	}
	return s.ports.Content.CheckTx(ctx, tx, a, ref, purpose)
}
func (s *Service) inputBranch(ctx context.Context, tx runtime.Tx, a runtime.Auth, target string, sessionRef, branchRef api.ObjectRef, expected uint64) (sessionRecord, branchRecord, error) {
	if target != sessionRef.ObjectID {
		return sessionRecord{}, branchRecord{}, invalid("target_mismatch")
	}
	if err := exactScope(tx.Scope(), sessionRef); err != nil {
		return sessionRecord{}, branchRecord{}, err
	}
	if err := exactScope(tx.Scope(), branchRef); err != nil {
		return sessionRecord{}, branchRecord{}, err
	}
	session, err := getSession(ctx, tx, a, target)
	if err != nil {
		return sessionRecord{}, branchRecord{}, err
	}
	if session.Session.State != "open" {
		return sessionRecord{}, branchRecord{}, api.E("invalid_state", "session_not_open")
	}
	branch, err := getBranch(ctx, tx, target, branchRef.ObjectID)
	if err != nil {
		return sessionRecord{}, branchRecord{}, err
	}
	if branch.Branch.Revision != expected || branchRef.Revision != expected {
		return sessionRecord{}, branchRecord{}, api.E("revision_conflict", "branch_changed")
	}
	return session, branch, nil
}
func (s *Service) SubmitGoalTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in GoalInput) (SubmissionOutput, error) {
	if s.ports.Delivery == nil || !api.ValidID(s.config.DiscoveryOwnerID) {
		return SubmissionOutput{}, api.E("unsupported", "delivery_unconfigured")
	}
	session, branch, err := s.inputBranch(ctx, tx, a, c.TargetID, in.SessionRef, in.BranchRef, in.ExpectedBranchRevision)
	if err != nil {
		return SubmissionOutput{}, err
	}
	if len(in.AttachmentRefs) > 20 {
		return SubmissionOutput{}, invalid("attachment_limit")
	}
	if err = s.content(ctx, tx, a, in.ContentRef, "task.goal"); err != nil {
		return SubmissionOutput{}, err
	}
	for _, ref := range in.AttachmentRefs {
		if err = s.content(ctx, tx, a, ref, "task.goal"); err != nil {
			return SubmissionOutput{}, err
		}
	}
	if err = api.ValidateRecord("ComponentRef", in.PolicyRef); err != nil {
		return SubmissionOutput{}, err
	}
	if err = api.ValidateAmounts(in.Budget); err != nil {
		return SubmissionOutput{}, err
	}
	if len(in.Budget) == 0 {
		return SubmissionOutput{}, invalid("budget_required")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return SubmissionOutput{}, err
	}
	deadline, err := api.ParseTime(in.TaskDeadline)
	if err != nil || !deadline.After(now) {
		return SubmissionOutput{}, api.E("expired", "task_deadline_elapsed")
	}
	kind := "goal"
	newGoal := true
	orderKey := ""
	switch c.Method {
	case "session.submit_goal":
		if in.TargetTaskRef != nil || in.PredecessorTaskRef != nil || in.ExpectedGoalRevision != nil {
			return SubmissionOutput{}, invalid("unexpected_goal_target")
		}
	case "session.enqueue_goal_after":
		kind = "follow_up"
		if in.PredecessorTaskRef == nil || in.TargetTaskRef != nil || in.ExpectedGoalRevision != nil || s.ports.Closure == nil {
			return SubmissionOutput{}, invalid("predecessor_required")
		}
		if err = runtime.CheckRef(tx.Scope(), *in.PredecessorTaskRef); err != nil {
			return SubmissionOutput{}, err
		}
	case "session.steer":
		kind = "steer"
		newGoal = false
		if in.TargetTaskRef == nil || in.ExpectedGoalRevision == nil || *in.ExpectedGoalRevision == 0 || in.PredecessorTaskRef != nil {
			return SubmissionOutput{}, invalid("request_target_mismatch")
		}
		if err = runtime.CheckRef(tx.Scope(), *in.TargetTaskRef); err != nil {
			return SubmissionOutput{}, err
		}
		orderKey = "steer:" + in.TargetTaskRef.OwnerID + ":" + in.TargetTaskRef.ObjectID
	default:
		return SubmissionOutput{}, invalid("unknown_submission_kind")
	}
	id := api.NewID("submission")
	ref := tx.Scope().Ref(id, 1)
	record := submissionRecord{SubmissionView: SubmissionView{Submission: api.Submission{SubmissionID: id, TenantID: tx.Scope().TenantID, OwnerID: tx.Scope().OwnerID, Revision: 1, SessionRef: in.SessionRef, BranchID: in.BranchRef.ObjectID, Kind: kind, ContentRef: in.ContentRef, TargetTaskRef: in.TargetTaskRef, PredecessorTaskRef: in.PredecessorTaskRef, ExpectedGoalRevision: in.ExpectedGoalRevision, State: "queued", CreatedAt: api.Time(now)}, AttachmentRefs: append([]api.ContentRef{}, in.AttachmentRefs...), QueueDeadline: api.Time(now.Add(s.config.QueueTTL))}, Auth: a, Goal: &in, GoalQueue: newGoal, OrderKey: orderKey, CreatedCommandID: c.CommandID}
	if deadline.Before(now.Add(s.config.QueueTTL)) {
		record.QueueDeadline = api.Time(deadline)
	}
	if newGoal {
		if branch.Queued >= s.config.BranchQueueLimit {
			return SubmissionOutput{}, api.E("overloaded", "queue_full")
		}
		if err = s.addSubjectQueue(ctx, tx, a.SubjectID); err != nil {
			return SubmissionOutput{}, err
		}
		branch.Queued++
		branch.Pending = append(branch.Pending, id)
	} else {
		if err = s.addOrder(ctx, tx, orderKey, id); err != nil {
			return SubmissionOutput{}, err
		}
	}
	if c.Method != "session.enqueue_goal_after" {
		record.Command = s.goalCommand(tx.Scope(), ref, record, now)
		record.Submission.DispatchCommandRef = &api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: record.Command.LogicalServiceID, ObjectID: record.Command.CommandID, Revision: 1}
	}
	if err = s.saveInput(ctx, tx, &session, &branch, &record, now); err != nil {
		return SubmissionOutput{}, err
	}
	return submissionOutput(tx.Scope(), record), nil
}
func (s *Service) goalCommand(scope runtime.Scope, source api.ObjectRef, r submissionRecord, now time.Time) *api.Command {
	owner := s.config.DiscoveryOwnerID
	target := api.NewID("task")
	method := "task.submit"
	expires := now.Add(s.config.DeliveryTTL)
	deadline, _ := api.ParseTime(r.Goal.TaskDeadline)
	if deadline.Before(expires) {
		expires = deadline
	}
	var payload any = taskSubmit{OrchestratorID: owner, GoalRef: r.Submission.ContentRef, PolicyRef: r.Goal.PolicyRef, Deadline: r.Goal.TaskDeadline, Budget: r.Goal.Budget, SourceSubmissionRef: &source}
	if r.Submission.Kind == "steer" {
		owner = r.Submission.TargetTaskRef.OwnerID
		target = r.Submission.TargetTaskRef.ObjectID
		method = "task.steer"
		payload = taskSteer{TaskID: target, BaseGoalRevision: *r.Submission.ExpectedGoalRevision, AmendmentRef: r.Submission.ContentRef, SourceSubmissionRef: source, PrepareDeadline: r.Goal.TaskDeadline}
	}
	return &api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: owner, CommandID: api.NewID("command"), Method: method, TargetID: target, ExpiresAt: api.Time(expires), Payload: api.Raw(payload)}
}
func (s *Service) addSubjectQueue(ctx context.Context, tx runtime.Tx, subject string) error {
	var q queueRecord
	_, err := tx.Get(ctx, queues, subject, &q)
	if errors.Is(err, runtime.ErrNotFound) {
		return tx.Create(ctx, queues, subject, subject, queueRecord{SubjectID: subject, Revision: 1, Count: 1})
	}
	if err != nil {
		return err
	}
	if q.Count >= s.config.SubjectQueueLimit {
		return api.E("overloaded", "queue_full")
	}
	old := q.Revision
	q.Revision++
	q.Count++
	return tx.Put(ctx, queues, subject, old, q)
}
func (s *Service) addOrder(ctx context.Context, tx runtime.Tx, key, id string) error {
	var q orderRecord
	_, err := tx.Get(ctx, queues, key, &q)
	if errors.Is(err, runtime.ErrNotFound) {
		return tx.Create(ctx, queues, key, "", orderRecord{Revision: 1, Pending: []string{id}})
	}
	if err != nil {
		return err
	}
	if len(q.Pending) >= 20 {
		return api.E("overloaded", "queue_full")
	}
	old := q.Revision
	q.Revision++
	q.Pending = append(q.Pending, id)
	return tx.Put(ctx, queues, key, old, q)
}
func (s *Service) saveInput(ctx context.Context, tx runtime.Tx, session *sessionRecord, branch *branchRecord, r *submissionRecord, now time.Time) error {
	if session.Sequence >= api.MaxSafeInteger {
		return api.E("overloaded", "session_sequence_exhausted")
	}
	session.Sequence++
	r.Submission.Seq = session.Sequence
	r.Submission.HistoryCutoff = branch.Branch.HistoryCutoff
	r.Submission.MessageID = api.NewID("message")
	ref := tx.Scope().Ref(r.Submission.SubmissionID, 1)
	message := api.Message{MessageID: r.Submission.MessageID, SessionRef: r.Submission.SessionRef, BranchID: branch.Branch.BranchID, Seq: session.Sequence, ParentMessageID: branch.Branch.HeadMessageID, Role: "user", ContentRef: r.Submission.ContentRef, SubmissionRef: &ref, CreatedAt: api.Time(now)}
	if err := tx.Create(ctx, messages, message.MessageID, session.Session.SessionID, message); err != nil {
		return err
	}
	if err := tx.Create(ctx, submissions, r.Submission.SubmissionID, session.Session.SessionID, *r); err != nil {
		return err
	}
	branch.Branch.HeadMessageID = message.MessageID
	branch.Branch.HistoryCutoff = message.Seq
	if err := saveBranch(ctx, tx, branch); err != nil {
		return err
	}
	if err := saveSession(ctx, tx, session); err != nil {
		return err
	}
	_, err := tx.Raise(ctx, JobDispatch, r.Submission.SubmissionID, ref, now)
	return err
}
func submissionOutput(scope runtime.Scope, r submissionRecord) SubmissionOutput {
	return SubmissionOutput{SubmissionRef: scope.Ref(r.Submission.SubmissionID, r.Submission.Revision), State: r.Submission.State, WithdrawalRequested: r.Submission.WithdrawalRequested, QueryMethod: "submission.read"}
}
func (s *Service) ReadSubmission(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, id string) (SubmissionView, error) {
	var r submissionRecord
	if _, err := store.Read(ctx, scope, submissions, id, 0, &r); err != nil {
		return SubmissionView{}, err
	}
	if err := access(a, r.Auth.SubjectID); err != nil {
		return SubmissionView{}, api.E("forbidden", "submission_redacted")
	}
	return r.SubmissionView, nil
}
func saveSubmission(ctx context.Context, tx runtime.Tx, r *submissionRecord) error {
	old := r.Submission.Revision
	r.Submission.Revision++
	return tx.Put(ctx, submissions, r.Submission.SubmissionID, old, *r)
}
func removeID(ids []string, id string) []string {
	out := make([]string, 0, len(ids))
	for _, v := range ids {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}
func (s *Service) releaseSubmission(ctx context.Context, tx runtime.Tx, r *submissionRecord, branch *branchRecord) error {
	if r.GoalQueue {
		branch.Pending = removeID(branch.Pending, r.Submission.SubmissionID)
		if branch.Queued == 0 {
			return api.E("invalid_state", "queue_accounting_mismatch")
		}
		branch.Queued--
		if err := saveBranch(ctx, tx, branch); err != nil {
			return err
		}
		var q queueRecord
		if _, err := tx.Get(ctx, queues, r.Auth.SubjectID, &q); err != nil {
			return err
		}
		if q.Count == 0 {
			return api.E("invalid_state", "queue_accounting_mismatch")
		}
		old := q.Revision
		q.Revision++
		q.Count--
		if err := tx.Put(ctx, queues, r.Auth.SubjectID, old, q); err != nil {
			return err
		}
		if len(branch.Pending) > 0 {
			if err := s.wakeSubmission(ctx, tx, branch.Pending[0]); err != nil {
				return err
			}
		}
		r.GoalQueue = false
	}
	if r.OrderKey != "" {
		var q orderRecord
		if _, err := tx.Get(ctx, queues, r.OrderKey, &q); err != nil {
			return err
		}
		old := q.Revision
		q.Revision++
		q.Pending = removeID(q.Pending, r.Submission.SubmissionID)
		if err := tx.Put(ctx, queues, r.OrderKey, old, q); err != nil {
			return err
		}
		if len(q.Pending) > 0 {
			if err := s.wakeSubmission(ctx, tx, q.Pending[0]); err != nil {
				return err
			}
		}
		r.OrderKey = ""
	}
	return nil
}
func (s *Service) wakeSubmission(ctx context.Context, tx runtime.Tx, id string) error {
	var r submissionRecord
	if _, err := tx.Get(ctx, submissions, id, &r); err != nil {
		return err
	}
	if err := saveSubmission(ctx, tx, &r); err != nil {
		return err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	_, err = tx.Raise(ctx, JobDispatch, id, tx.Scope().Ref(id, r.Submission.Revision), now)
	return err
}
func (s *Service) WithdrawTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in WithdrawInput) (SubmissionOutput, error) {
	var peek submissionRecord
	if _, err := tx.Get(ctx, submissions, c.TargetID, &peek); err != nil {
		return SubmissionOutput{}, err
	}
	if err := access(a, peek.Auth.SubjectID); err != nil {
		return SubmissionOutput{}, err
	}
	branch, err := getBranch(ctx, tx, peek.Submission.SessionRef.ObjectID, peek.Submission.BranchID)
	if err != nil {
		return SubmissionOutput{}, err
	}
	r := peek
	if c.ExpectedRevision == nil || *c.ExpectedRevision != r.Submission.Revision {
		return SubmissionOutput{}, api.E("revision_conflict", "submission_changed")
	}
	switch r.Submission.State {
	case "queued":
		r.Submission.State = "withdrawn"
		if err = s.releaseSubmission(ctx, tx, &r, &branch); err != nil {
			return SubmissionOutput{}, err
		}
	case "sending":
		r.Submission.WithdrawalRequested = true
	case "withdrawn":
		return submissionOutput(tx.Scope(), r), nil
	default:
		return SubmissionOutput{}, api.E("invalid_state", "submission_already_decided")
	}
	if err = saveSubmission(ctx, tx, &r); err != nil {
		return SubmissionOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return SubmissionOutput{}, err
	}
	if _, err = tx.Raise(ctx, JobDispatch, c.TargetID, tx.Scope().Ref(c.TargetID, r.Submission.Revision), now); err != nil {
		return SubmissionOutput{}, err
	}
	return submissionOutput(tx.Scope(), r), nil
}
func (s *Service) ForwardInputTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in InputInput) (SubmissionOutput, error) {
	if s.ports.Requests == nil || s.ports.Delivery == nil {
		return SubmissionOutput{}, api.E("unsupported", "request_forwarding_unconfigured")
	}
	session, branch, err := s.inputBranch(ctx, tx, a, c.TargetID, in.SessionRef, in.BranchRef, in.ExpectedBranchRevision)
	if err != nil {
		return SubmissionOutput{}, err
	}
	if err = runtime.CheckRef(tx.Scope(), in.RequestRef); err != nil {
		return SubmissionOutput{}, err
	}
	view, err := s.ports.Requests.CheckTx(ctx, tx, a, in.RequestRef)
	if err != nil {
		return SubmissionOutput{}, err
	}
	request := view.Request
	if request.RequestID != in.RequestRef.ObjectID || request.Revision != in.RequestRef.Revision || request.OwnerID != in.RequestRef.OwnerID || request.TenantID != in.RequestRef.TenantID || view.Method == "" || request.State != "pending" {
		return SubmissionOutput{}, api.E("invalid_state", "request_target_mismatch")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return SubmissionOutput{}, err
	}
	expires, err := api.ParseTime(request.ExpiresAt)
	if err != nil || !now.Before(expires) {
		return SubmissionOutput{}, api.E("expired", "input_request_expired")
	}
	if !api.Equal(in.PreviewRefs, request.PreviewRefs) {
		return SubmissionOutput{}, invalid("preview_refs_mismatch")
	}
	if err = s.content(ctx, tx, a, in.AnswerRef, "interaction.input"); err != nil {
		return SubmissionOutput{}, err
	}
	for _, ref := range in.PreviewRefs {
		if err = s.content(ctx, tx, a, ref, "interaction.preview"); err != nil {
			return SubmissionOutput{}, err
		}
	}
	id := api.NewID("submission")
	ref := tx.Scope().Ref(id, 1)
	r := submissionRecord{SubmissionView: SubmissionView{Submission: api.Submission{SubmissionID: id, TenantID: tx.Scope().TenantID, OwnerID: tx.Scope().OwnerID, Revision: 1, SessionRef: in.SessionRef, BranchID: in.BranchRef.ObjectID, Kind: "input", ContentRef: in.AnswerRef, RequestRef: &in.RequestRef, State: "queued", CreatedAt: api.Time(now)}, AttachmentRefs: append([]api.ContentRef{}, in.PreviewRefs...), QueueDeadline: request.ExpiresAt}, Auth: a, Input: &in, InputView: &view, CreatedCommandID: c.CommandID}
	commandExpiry := now.Add(s.config.DeliveryTTL)
	if expires.Before(commandExpiry) {
		commandExpiry = expires
	}
	var payload any = struct {
		RequestRef  api.ObjectRef    `json:"request_ref"`
		AnswerRef   api.ContentRef   `json:"answer_ref"`
		PreviewRefs []api.ContentRef `json:"preview_refs"`
	}{in.RequestRef, in.AnswerRef, in.PreviewRefs}
	if view.Method == "task.input" {
		if request.GoalRevision == nil {
			return SubmissionOutput{}, invalid("request_target_mismatch")
		}
		payload = taskAnswer{TaskID: request.TargetRef.ObjectID, RequestRef: in.RequestRef, GoalRevision: *request.GoalRevision, AnswerRef: in.AnswerRef}
		r.Submission.TargetTaskRef = &request.TargetRef
	}
	r.Command = &api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: request.OwnerID, CommandID: api.NewID("command"), Method: view.Method, TargetID: request.TargetRef.ObjectID, ExpiresAt: api.Time(commandExpiry), Payload: api.Raw(payload)}
	r.Submission.DispatchCommandRef = &api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: request.OwnerID, ObjectID: r.Command.CommandID, Revision: 1}
	if _, err = api.ParseJSON(view.AnswerSchema); err != nil {
		return SubmissionOutput{}, invalid("answer_schema_invalid")
	}
	if err = s.saveInput(ctx, tx, &session, &branch, &r, now); err != nil {
		return SubmissionOutput{}, err
	}
	_ = ref
	return submissionOutput(tx.Scope(), r), nil
}
func (s *Service) ReplyTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ReplyInput) (ReplyOutput, error) {
	if !a.HasRole("interaction_reply") {
		return ReplyOutput{}, api.E("forbidden", "reply_principal_required")
	}
	if err := exactScope(tx.Scope(), in.SubmissionRef); err != nil {
		return ReplyOutput{}, err
	}
	var r submissionRecord
	if _, err := tx.Get(ctx, submissions, in.SubmissionRef.ObjectID, &r); err != nil {
		return ReplyOutput{}, err
	}
	if c.TargetID != r.Submission.SessionRef.ObjectID {
		return ReplyOutput{}, invalid("target_mismatch")
	}
	session, err := getSession(ctx, tx, r.Auth, c.TargetID)
	if err != nil {
		return ReplyOutput{}, err
	}
	branch, err := getBranch(ctx, tx, c.TargetID, r.Submission.BranchID)
	if err != nil {
		return ReplyOutput{}, err
	}
	if err = s.content(ctx, tx, r.Auth, in.ContentRef, "interaction.snapshot"); err != nil {
		return ReplyOutput{}, err
	}
	if in.Role != "assistant" && in.Role != "system" {
		return ReplyOutput{}, invalid("reply_role_invalid")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return ReplyOutput{}, err
	}
	session.Sequence++
	message := api.Message{MessageID: api.NewID("message"), SessionRef: r.Submission.SessionRef, BranchID: r.Submission.BranchID, Seq: session.Sequence, ParentMessageID: r.Submission.MessageID, Role: in.Role, ContentRef: in.ContentRef, SubmissionRef: &in.SubmissionRef, CreatedAt: api.Time(now)}
	if err = tx.Create(ctx, messages, message.MessageID, c.TargetID, message); err != nil {
		return ReplyOutput{}, err
	}
	if branch.Branch.HeadMessageID == r.Submission.MessageID {
		branch.Branch.HeadMessageID = message.MessageID
		branch.Branch.HistoryCutoff = message.Seq
		if err = saveBranch(ctx, tx, &branch); err != nil {
			return ReplyOutput{}, err
		}
	}
	if err = saveSession(ctx, tx, &session); err != nil {
		return ReplyOutput{}, err
	}
	return ReplyOutput{MessageRef: tx.Scope().Ref(message.MessageID, 1), BranchRef: tx.Scope().Ref(branch.Branch.BranchID, branch.Branch.Revision)}, nil
}

// validateAnswer 从准确内容解码；Schema 由原请求 owner 的受信登记提供。
func validateAnswer(schema json.RawMessage, body []byte) error {
	var doc api.Schema
	if err := api.Decode(schema, &doc); err != nil {
		return err
	}
	v, err := api.NewValidator(doc)
	if err != nil {
		return invalid("answer_schema_invalid")
	}
	return v.Validate(body)
}
