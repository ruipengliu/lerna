package sessions_test

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

var ctx = context.Background()
var caller = &v1.Caller{UserId: "alice", IssuerId: "cli"}

func header(id string) *v1.CommandHeader {
	return &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "cli", TargetDomainId: "local", CommandId: id}, ContractVersion: 1, SchemaId: "lerna.v1.AdmissionCommands", FingerprintVersion: 1}
}
func open(t *testing.T) *assembly.Harness {
	t.Helper()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "test.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { h.Close() })
	return h
}
func accepted(t *testing.T, r *v1.CommandReceipt, e error) *v1.CommandReceipt {
	t.Helper()
	if e != nil || r.GetDecision() != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("accepted: %v %v", r, e)
	}
	return r
}

// 规则：G3、G12
func TestEmptySessionCreationHasStableReceiptWithoutTask(t *testing.T) {
	h := open(t)
	c := &v1.CreateSessionCommand{Header: header("empty")}
	r, e := h.Sessions.CreateSession(ctx, caller, c)
	first := accepted(t, r, e)
	session, e := h.Sessions.QuerySession(ctx, caller, first.ResultRef.Name)
	if e != nil || session.LastCommittedSeq != 0 || len(session.TaskRefs) != 0 || len(session.Inputs) != 0 {
		t.Fatalf("empty session: %v %v", session, e)
	}
	r, e = h.Sessions.CreateSession(ctx, caller, c)
	duplicate := accepted(t, r, e)
	if !proto.Equal(first, duplicate) {
		t.Fatal("replay changed receipt")
	}
}

func stage(t *testing.T, h *assembly.Harness, id, text string) *v1.Ref {
	t.Helper()
	r, e := h.Content.Stage(ctx, caller, &v1.SubmitGoalCommand{Identity: header(id).Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: text})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func session(t *testing.T, h *assembly.Harness) *v1.GlobalName {
	t.Helper()
	r, e := h.Sessions.CreateSession(ctx, caller, &v1.CreateSessionCommand{Header: header("session")})
	return accepted(t, r, e).ResultRef.Name
}

// 规则：G3、G4、G12
func TestRecordedInputNeverTargetsImplicitTaskAndSessionSupportsMultipleGoals(t *testing.T) {
	h := open(t)
	id := session(t, h)
	for _, name := range []string{"first", "second", "record"} {
		kind := "GOAL"
		if name == "record" {
			kind = "RECORD"
		}
		c := &v1.SubmitInputCommand{Header: header(name), SessionId: id, InputKind: kind, ContentRef: stage(t, h, name+"-content", name)}
		r, e := h.Sessions.SubmitInput(ctx, caller, c)
		accepted(t, r, e)
	}
	s, e := h.Sessions.QuerySession(ctx, caller, id)
	if e != nil {
		t.Fatal(e)
	}
	if s.LastCommittedSeq != 3 || len(s.TaskRefs) != 2 || len(s.Inputs) != 3 {
		t.Fatalf("session: %v", s)
	}
	if s.Inputs[0].TaskInputSeq != 1 || s.Inputs[1].TaskInputSeq != 1 {
		t.Fatal("initial goals missing task input sequence")
	}
	if s.Inputs[2].TaskId != nil || s.Inputs[2].RoutingStatus != "RECORDED" {
		t.Fatal("record implicitly routed")
	}
	if proto.Equal(s.TaskRefs[0].Name, s.TaskRefs[1].Name) {
		t.Fatal("goals shared task")
	}
}

func goal(t *testing.T, h *assembly.Harness, id *v1.GlobalName) *v1.Task {
	t.Helper()
	r, e := h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("goal"), SessionId: id, InputKind: "GOAL", ContentRef: stage(t, h, "goal-text", "write A")})
	accepted(t, r, e)
	s, e := h.Sessions.QuerySession(ctx, caller, id)
	if e != nil {
		t.Fatal(e)
	}
	task, e := h.Tasks.QueryTask(ctx, caller, s.TaskRefs[0].Name)
	if e != nil {
		t.Fatal(e)
	}
	return task
}
func bind(t *testing.T, h *assembly.Harness, task *v1.Task) *v1.Ref {
	t.Helper()
	hdr := header("bind")
	hdr.Identity.IssuerId = "host"
	r, e := h.Tasks.AcceptRequirements(ctx, &v1.Caller{UserId: "alice", IssuerId: "host"}, &v1.AcceptRequirementsCommand{Header: hdr, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "record", DescriptionRef: task.GoalRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}})
	return accepted(t, r, e).ResultRef
}

// 规则：G2、G3、G4、G6、准入-3
func TestModificationInvalidatesBasisBeforeTrustedProcessingAndPreservesOldRefs(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	oldRef := bind(t, h, task)
	oldReq, e := h.Tasks.QueryRequirements(ctx, caller, oldRef)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: header("request"), TaskId: task.TaskId})
	if e != nil {
		t.Fatal(e)
	}
	task, e = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if e != nil {
		t.Fatal(e)
	}
	c := &v1.SubmitInputCommand{Header: header("modify"), SessionId: id, TaskId: task.TaskId, InputKind: "MODIFY", ContentRef: stage(t, h, "modify-text", "write B"), ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1}
	r, e := h.Sessions.SubmitInput(ctx, caller, c)
	accepted(t, r, e)
	current, e := h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if e != nil {
		t.Fatal(e)
	}
	if current.InputVersion != 2 || current.ControlGeneration != task.ControlGeneration+1 || current.BoundInputVersion != 1 {
		t.Fatalf("basis not invalidated: %v", current)
	}
	p, e := h.Tasks.QueryPlanning(ctx, caller, task.TaskId)
	if e != nil || p.Snapshot != nil || p.Proposal != nil {
		t.Fatalf("current request survived: %v %v", p, e)
	}
	historical, e := h.Tasks.QuerySnapshot(ctx, caller, snapshot.Ref)
	if e != nil || !proto.Equal(snapshot, historical) {
		t.Fatalf("old snapshot changed: %v", e)
	}
	historicalReq, e := h.Tasks.QueryRequirements(ctx, caller, oldRef)
	if e != nil || !proto.Equal(oldReq, historicalReq) {
		t.Fatalf("old requirements changed: %v", e)
	}
	r, e = h.Sessions.SubmitInput(ctx, caller, c)
	accepted(t, r, e)
	current, e = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if e != nil || current.InputVersion != 2 {
		t.Fatal("replay applied twice")
	}
}

// 规则：G3、G4、G12
func TestQuestionAnswersBindExactRequestAndNonBasisAnswerDoesNotChangeControl(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	bind(t, h, task)
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	hdr := header("question")
	hdr.Identity.IssuerId = "host"
	r, e := h.Sessions.PublishQuestion(ctx, &v1.Caller{UserId: "alice", IssuerId: "host"}, &v1.PublishQuestionCommand{Header: hdr, SessionId: id, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ContentRef: task.GoalRef, ExpiresAtUnixMs: 4102444800000, ChangesBasis: false})
	q := accepted(t, r, e).ResultRef
	c := &v1.SubmitInputCommand{Header: header("answer"), SessionId: id, TaskId: task.TaskId, InputKind: "ANSWER", ContentRef: stage(t, h, "answer-text", "yes"), ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1, RequestRef: q}
	r, e = h.Sessions.SubmitInput(ctx, caller, c)
	accepted(t, r, e)
	after, e := h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if e != nil || after.InputVersion != 2 || after.BoundInputVersion != 2 || after.ControlGeneration != task.ControlGeneration {
		t.Fatalf("nonbasis: %v %v", after, e)
	}
	p, e := h.Tasks.QueryPlanning(ctx, caller, task.TaskId)
	if e != nil || p.Requirements.BoundInputVersion != 2 {
		t.Fatalf("current binding snapshot: %v %v", p, e)
	}
	question, e := h.Sessions.QueryQuestion(ctx, caller, q)
	if e != nil || question.Status != "ANSWERED" || question.ResponseInputRef == nil {
		t.Fatalf("question: %v %v", question, e)
	}
	c.Header = header("late-answer")
	c.ExpectedInputVersion = 2
	r, e = h.Sessions.SubmitInput(ctx, caller, c)
	if e != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED || r.Error.Code != "REQUEST_INVALID" {
		t.Fatalf("late answer: %v %v", r, e)
	}
}

// 规则：G3、G4、G12
func TestConcurrentModificationsHaveOneWinnerAndDependenciesNeverMisroute(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	bind(t, h, task)
	text := stage(t, h, "changes", "change")
	results := make(chan *v1.CommandReceipt, 2)
	errors := make(chan error, 2)
	for _, name := range []string{"one", "two"} {
		go func(name string) {
			r, e := h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header(name), SessionId: id, TaskId: task.TaskId, InputKind: "MODIFY", ContentRef: text, ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1})
			results <- r
			errors <- e
		}(name)
	}
	wins := 0
	for range 2 {
		r := <-results
		e := <-errors
		if e != nil {
			t.Fatal(e)
		}
		if r.Decision == v1.Decision_DECISION_ACCEPTED {
			wins++
		} else if r.Error.Code != "STALE_INPUT" {
			t.Fatalf("wrong conflict: %v", r)
		}
	}
	if wins != 1 {
		t.Fatalf("winners: %d", wins)
	}
	s, _ := h.Sessions.QuerySession(ctx, caller, id)
	if s.LastCommittedSeq != 2 {
		t.Fatalf("rejected input consumed sequence: %v", s)
	}
	c := &v1.SubmitInputCommand{Header: header("dependent"), SessionId: id, InputKind: "RECORD", ContentRef: text, DependsOn: []*v1.CommandIdentity{header("absent").Identity}}
	r, e := h.Sessions.SubmitInput(ctx, caller, c)
	accepted(t, r, e)
	s, _ = h.Sessions.QuerySession(ctx, caller, id)
	if s.Inputs[2].RoutingStatus != "WAITING_DEPENDENCY" {
		t.Fatal("unmet dependency ignored")
	}
}

// 规则：G2、G3、G4、G6、准入-3
func TestTrustedProcessingCannotSkipInputsOrMutateHistoricalRequirements(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	old := bind(t, h, task)
	for n := uint64(1); n <= 2; n++ {
		r, e := h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header(fmt.Sprintf("modify-%d", n)), SessionId: id, TaskId: task.TaskId, InputKind: "MODIFY", ContentRef: stage(t, h, fmt.Sprintf("text-%d", n), "change"), ExpectedInputVersion: n, ExpectedRequirementsVersion: 1})
		accepted(t, r, e)
	}
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	hdr := header("skip")
	hdr.Identity.IssuerId = "host"
	c := &v1.ProcessInputCommand{Header: hdr, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: 3, Outcome: "UNCHANGED", Source: "TRUSTED_TEMPLATE"}
	r, e := h.Tasks.ProcessInput(ctx, &v1.Caller{UserId: "alice", IssuerId: "host"}, c)
	if e != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED || r.Error.Code != "INPUT_ORDER" {
		t.Fatalf("skip: %v %v", r, e)
	}
	for _, n := range []uint64{2, 3} {
		task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
		c.Header = header(fmt.Sprintf("process-%d", n))
		c.Header.Identity.IssuerId = "host"
		c.TaskRef.Revision = task.Revision
		c.InputVersion = n
		r, e = h.Tasks.ProcessInput(ctx, &v1.Caller{UserId: "alice", IssuerId: "host"}, c)
		accepted(t, r, e)
	}
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if task.BoundInputVersion != 3 {
		t.Fatalf("not bound: %v", task)
	}
	historical, e := h.Tasks.QueryRequirements(ctx, caller, old)
	if e != nil || historical.BoundInputVersion != 1 {
		t.Fatal("historical requirements overwritten")
	}
	inputs, e := h.Tasks.QueryInputs(ctx, caller, task.TaskId)
	if e != nil || len(inputs.Inputs) != 3 || inputs.Inputs[0].ProcessingStatus != "PROCESSED" || inputs.Inputs[2].ProcessingDecision == nil {
		t.Fatalf("processing evidence: %v %v", inputs, e)
	}
}

// 规则：G2、G4、G6
func TestExplicitUserConditionsAreAcceptedAndUnsupportedHistoryNeverOverwrites(t *testing.T) {
	h := open(t)
	id := session(t, h)
	text := stage(t, h, "conditions", "must have target record")
	c := &v1.SubmitInputCommand{Header: header("explicit-goal"), SessionId: id, InputKind: "GOAL", ContentRef: text, ExplicitConditions: []*v1.Requirement{{ConditionId: "user-condition", DescriptionRef: text, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}}
	r, e := h.Sessions.SubmitInput(ctx, caller, c)
	accepted(t, r, e)
	session, _ := h.Sessions.QuerySession(ctx, caller, id)
	task, _ := h.Tasks.QueryTask(ctx, caller, session.TaskRefs[0].Name)
	planning, _ := h.Tasks.QueryPlanning(ctx, caller, task.TaskId)
	if task.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED || task.BoundInputVersion != 1 || planning.Requirements.Source != "USER_EXPLICIT" {
		t.Fatalf("explicit requirements: %v %v", task, planning)
	}
	for _, kind := range []string{"BRANCH", "EDIT_HISTORY"} {
		r, e = h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header(kind), SessionId: id, InputKind: kind, ContentRef: text})
		if e != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED || r.Error.Code != "UNSUPPORTED_FEATURE" {
			t.Fatalf("history: %v %v", r, e)
		}
	}
	after, _ := h.Sessions.QuerySession(ctx, caller, id)
	if !proto.Equal(session, after) {
		t.Fatal("unsupported history changed session")
	}
}

// 规则：G3、G4、G11、开始-2
func TestControlReplayCannotResumeLaterPauseOrChangeInputVersion(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	bind(t, h, task)
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	original := task.ControlGeneration
	var resume *v1.SubmitInputCommand
	for i, control := range []string{"PAUSE", "RESUME", "PAUSE", "CANCEL"} {
		if i == 3 {
			break
		}
		c := &v1.SubmitInputCommand{Header: header(fmt.Sprintf("control-%d", i)), SessionId: id, TaskId: task.TaskId, InputKind: "CONTROL", Control: control, ExpectedControlGeneration: original + uint64(i)}
		r, e := h.Sessions.SubmitInput(ctx, caller, c)
		accepted(t, r, e)
		if control == "RESUME" {
			resume = c
		}
	}
	r, e := h.Sessions.SubmitInput(ctx, caller, resume)
	accepted(t, r, e)
	after, _ := h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if after.Control != v1.TaskControl_TASK_CONTROL_PAUSED || after.InputVersion != 1 || after.BoundInputVersion != 1 || after.ControlGeneration != original+3 {
		t.Fatalf("old resume applied: %v", after)
	}
	r, e = h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("cancel"), SessionId: id, TaskId: task.TaskId, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: after.ControlGeneration})
	accepted(t, r, e)
	after, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if after.Control != v1.TaskControl_TASK_CONTROL_CANCELLING || after.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN {
		t.Fatalf("cancel closed responsibility: %v", after)
	}
}

// 规则：G2、G4、G6、准入-3
func TestRequirementsAcceptanceCannotSkipAcceptedChanges(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	bind(t, h, task)
	for n := uint64(1); n <= 2; n++ {
		r, e := h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header(fmt.Sprintf("skip-modify-%d", n)), SessionId: id, TaskId: task.TaskId, InputKind: "MODIFY", ContentRef: task.GoalRef, ExpectedInputVersion: n, ExpectedRequirementsVersion: 1})
		accepted(t, r, e)
	}
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	hdr := header("skip-accept")
	hdr.Identity.IssuerId = "host"
	r, e := h.Tasks.AcceptRequirements(ctx, &v1.Caller{UserId: "alice", IssuerId: "host"}, &v1.AcceptRequirementsCommand{Header: hdr, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: 3, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "c", DescriptionRef: task.GoalRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}})
	if e != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED || r.Error.Code != "INPUT_ORDER" {
		t.Fatalf("skipped accepted inputs: %v %v", r, e)
	}
}

// 规则：G3、G4
func TestCLICreatesEmptySessionAndRoutesVersionedInput(t *testing.T) {
	h := open(t)
	cli := interaction.CLI{Sessions: h.Sessions, Tasks: h.Tasks, Durable: h.Durable, Content: h.Content, Caller: caller, Domain: "local"}
	var out bytes.Buffer
	if e := cli.Run(ctx, []string{"session-create", "--command", "cli-session"}, &out); e != nil {
		t.Fatal(e)
	}
	r := new(v1.CommandReceipt)
	if e := protojson.Unmarshal(out.Bytes(), r); e != nil {
		t.Fatal(e)
	}
	id := r.ResultRef.Name.LocalId
	out.Reset()
	if e := cli.Run(ctx, []string{"input", "--command", "cli-goal", "--session", id, "--kind", "GOAL", "--text", "write target"}, &out); e != nil {
		t.Fatal(e)
	}
	if e := protojson.Unmarshal(out.Bytes(), r); e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("CLI input: %v %v", r, e)
	}
	s, _ := h.Sessions.QuerySession(ctx, caller, &v1.GlobalName{UserId: "alice", AuthorityDomainId: "local", ObjectKind: "session", LocalId: id})
	if len(s.TaskRefs) != 1 {
		t.Fatal("CLI did not create task")
	}
	out.Reset()
	if e := cli.Run(ctx, []string{"input", "--command", "cli-modify", "--session", id, "--kind", "MODIFY", "--task", s.TaskRefs[0].Name.LocalId, "--text", "change target", "--input-version", "1", "--requirements-version", "1"}, &out); e != nil {
		t.Fatal(e)
	}
	if e := protojson.Unmarshal(out.Bytes(), r); e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("CLI modification: %v %v", r, e)
	}
}

// 规则：G3、G4、G12
func TestWaitingDependencyRoutesOriginalInputOnceAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dependencies.db")
	h, e := assembly.Open(path, "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	id := session(t, h)
	text := stage(t, h, "pending-text", "goal")
	original := &v1.SubmitInputCommand{Header: header("dependent-goal"), SessionId: id, InputKind: "GOAL", ContentRef: text, DependsOn: []*v1.CommandIdentity{header("prerequisite").Identity}}
	r, e := h.Sessions.SubmitInput(ctx, caller, original)
	ref := accepted(t, r, e).ResultRef
	h.Close()
	h, e = assembly.Open(path, "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	before, _ := h.Sessions.QuerySession(ctx, caller, id)
	if len(before.TaskRefs) != 0 || before.Inputs[0].RoutingStatus != "WAITING_DEPENDENCY" {
		t.Fatal("pending goal was routed early")
	}
	r, e = h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("prerequisite"), SessionId: id, InputKind: "RECORD", ContentRef: text})
	accepted(t, r, e)
	route := &v1.RouteInputCommand{Header: header("route"), InputRef: ref}
	r, e = h.Sessions.RouteInput(ctx, caller, route)
	first := accepted(t, r, e)
	r, e = h.Sessions.RouteInput(ctx, caller, route)
	if !proto.Equal(accepted(t, r, e), first) {
		t.Fatal("route replay changed")
	}
	after, _ := h.Sessions.QuerySession(ctx, caller, id)
	if len(after.TaskRefs) != 1 || len(after.Inputs) != 2 || after.Inputs[0].SessionSeq != 1 || after.Inputs[1].SessionSeq != 2 {
		t.Fatalf("routing changed history: %v", after)
	}
	delivery, e := h.Sessions.QueryInput(ctx, caller, ref)
	if e != nil || !proto.Equal(delivery.OriginalCommand, original) || delivery.Input.RoutingStatus != "TASK_ACCEPTED" {
		t.Fatalf("lost original route: %v %v", delivery, e)
	}
}

// 规则：G2、G4、G6
func TestOutsideTemplateClarificationDoesNotAcceptConditions(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	hdr := header("clarify")
	hdr.Identity.IssuerId = "host"
	host := &v1.Caller{UserId: "alice", IssuerId: "host"}
	r, e := h.Tasks.ProcessInput(ctx, host, &v1.ProcessInputCommand{Header: hdr, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: 1, Outcome: "CLARIFY"})
	accepted(t, r, e)
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if task.BoundInputVersion != 0 || task.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_DRAFT || len(task.WaitingOn) != 1 || task.WaitingOn[0] != "USER_CLARIFICATION" {
		t.Fatalf("clarification accepted conditions: %v", task)
	}
	hdr = header("clarification-question")
	hdr.Identity.IssuerId = "host"
	r, e = h.Sessions.PublishQuestion(ctx, host, &v1.PublishQuestionCommand{Header: hdr, SessionId: id, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ContentRef: stage(t, h, "question-text", "Please provide explicit conditions"), ChangesBasis: true, ExpiresAtUnixMs: 4102444800000})
	accepted(t, r, e)
}

func propose(t *testing.T, h *assembly.Harness, task *v1.Task, id string) *v1.Ref {
	t.Helper()
	snap, e := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: header(id + "-request"), TaskId: task.TaskId})
	if e != nil {
		t.Fatal(e)
	}
	r, e := h.Tasks.ReceiveProposal(ctx, caller, &v1.ReceiveProposalCommand{Header: header(id + "-receive"), Proposal: &v1.Proposal{TaskId: task.TaskId, ContextSnapshotRef: snap.Ref, RequestRef: snap.RequestRef, RequirementsVersion: snap.RequirementsVersion, InputVersion: snap.InputVersion, ControlGeneration: snap.ControlGeneration, PlanningGeneration: snap.PlanningGeneration, Kind: "ACTION", ReasonerRef: &v1.Ref{Name: &v1.GlobalName{UserId: "alice", AuthorityDomainId: "local", ObjectKind: "reasoner", LocalId: "scripted"}, Revision: 1, SchemaId: "lerna.v1.Reasoner"}, Step: &v1.ActionStep{StepId: "step", ParametersRef: task.GoalRef}}})
	return accepted(t, r, e).ResultRef
}

// 规则：G2、G4、G6、准入-1、准入-3
func TestBothOldProposalAndFreshProposalCannotBypassUnprocessedChange(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	bind(t, h, task)
	old := propose(t, h, task, "old")
	r, e := h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("change"), SessionId: id, TaskId: task.TaskId, InputKind: "MODIFY", ContentRef: task.GoalRef, ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1})
	accepted(t, r, e)
	r, e = h.Tasks.Admit(ctx, caller, &v1.AdmitCommand{Header: header("old-admit"), TaskId: task.TaskId, ProposalRef: old})
	if e != nil || r.GetError().GetCode() != "STALE_PROPOSAL" {
		t.Fatalf("old proposal: %v %v", r, e)
	}
	historical, e := h.Tasks.QueryProposal(ctx, caller, old)
	if e != nil || historical.InputVersion != 1 {
		t.Fatal("old proposal lost")
	}
	fresh := propose(t, h, task, "fresh")
	r, e = h.Tasks.Admit(ctx, caller, &v1.AdmitCommand{Header: header("fresh-admit"), TaskId: task.TaskId, ProposalRef: fresh})
	if e != nil || r.GetError().GetCode() != "REQUIREMENTS_NOT_ACCEPTED" {
		t.Fatalf("fresh proposal bypassed processing: %v %v", r, e)
	}
	p, e := h.Tasks.QueryPlanning(ctx, caller, task.TaskId)
	if e != nil || len(p.AdmissionRefs) != 0 {
		t.Fatal("rejected input produced admission")
	}
}

// 规则：G3、G4、G6、G12
func TestUnsupportedAndAmbiguousInputsDoNotBecomePendingWork(t *testing.T) {
	h := open(t)
	id := session(t, h)
	text := stage(t, h, "ambiguous-text", "hello")
	for _, tc := range []struct {
		name string
		c    *v1.SubmitInputCommand
		code string
	}{
		{"branch", &v1.SubmitInputCommand{InputKind: "BRANCH", DependsOn: []*v1.CommandIdentity{header("missing").Identity}}, "UNSUPPORTED_FEATURE"},
		{"history", &v1.SubmitInputCommand{InputKind: "EDIT_HISTORY"}, "UNSUPPORTED_FEATURE"},
		{"record-control", &v1.SubmitInputCommand{InputKind: "RECORD", ContentRef: text, Control: "CANCEL"}, "INVALID_INPUT"},
		{"future-watermark", &v1.SubmitInputCommand{InputKind: "RECORD", ContentRef: text, ObservedSessionSeq: 100}, "STALE_SESSION"},
	} {
		c := tc.c
		c.Header = header(tc.name)
		c.SessionId = id
		r, e := h.Sessions.SubmitInput(ctx, caller, c)
		if e != nil || r.GetError().GetCode() != tc.code {
			t.Fatalf("%s: %v %v", tc.name, r, e)
		}
	}
	s, _ := h.Sessions.QuerySession(ctx, caller, id)
	if len(s.Inputs) != 0 || s.LastCommittedSeq != 0 {
		t.Fatal("invalid input consumed sequence")
	}
}

// 规则：G2、G4、G6
func TestNewSnapshotContainsAcceptedInputInTaskOrder(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	bind(t, h, task)
	old, e := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: header("context-old"), TaskId: task.TaskId})
	if e != nil {
		t.Fatal(e)
	}
	changed := stage(t, h, "new-recipient", "send to B")
	r, e := h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("context-change"), SessionId: id, TaskId: task.TaskId, InputKind: "MODIFY", ContentRef: changed, ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1})
	accepted(t, r, e)
	snap, e := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: header("context-new"), TaskId: task.TaskId})
	if e != nil {
		t.Fatal(e)
	}
	if len(snap.ContentRefs) != 2 || !proto.Equal(snap.ContentRefs[0], task.GoalRef) || !proto.Equal(snap.ContentRefs[1], changed) {
		t.Fatalf("new context omitted accepted input: %v", snap)
	}
	historical, e := h.Tasks.QuerySnapshot(ctx, caller, old.Ref)
	if e != nil || !proto.Equal(historical, old) || len(historical.ContentRefs) != 1 {
		t.Fatal("historical context changed")
	}
}

// 规则：G3、G11
func TestLegacyGoalInputRemainsQueryableBySnapshotReference(t *testing.T) {
	h := open(t)
	cmd := &v1.SubmitGoalCommand{Identity: header("legacy").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "legacy goal"}
	if _, e := h.Sessions.SubmitGoal(ctx, caller, cmd); e != nil {
		t.Fatal(e)
	}
	if e := h.Sessions.ProcessPending(ctx, caller); e != nil {
		t.Fatal(e)
	}
	q, e := h.Durable.QueryReceipt(ctx, caller, cmd.Identity)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: header("legacy-snapshot"), TaskId: q.Receipt.TaskRef.Name})
	if e != nil {
		t.Fatal(e)
	}
	if len(snap.InputRefs) != 1 {
		t.Fatal("legacy goal lost input provenance")
	}
	input, e := h.Sessions.QueryInput(ctx, caller, snap.InputRefs[0])
	if e != nil || input == nil {
		t.Fatalf("legacy input missing: %v %v", input, e)
	}
}

// 规则：G3、G4、G12
func TestIndependentConnectionsAcceptOnlyOneAnswerAndUseCoreOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "two-connections.db")
	h, e := assembly.Open(path, "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	id := session(t, h)
	task := goal(t, h, id)
	bind(t, h, task)
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	hdr := header("race-question")
	hdr.Identity.IssuerId = "host"
	r, e := h.Sessions.PublishQuestion(ctx, &v1.Caller{UserId: "alice", IssuerId: "host"}, &v1.PublishQuestionCommand{Header: hdr, SessionId: id, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ContentRef: task.GoalRef, ExpiresAtUnixMs: 4102444800000, ChangesBasis: true})
	question := accepted(t, r, e).ResultRef
	other, e := assembly.Open(path, "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	type result struct {
		r *v1.CommandReceipt
		e error
	}
	results := make(chan result, 2)
	for i, harness := range []*assembly.Harness{h, other} {
		go func(i int, harness *assembly.Harness) {
			hdr := header(fmt.Sprintf("answer-%d", i))
			hdr.Identity.IssuerId = fmt.Sprintf("endpoint-%d", i)
			r, e := harness.Sessions.SubmitInput(ctx, &v1.Caller{UserId: "alice", IssuerId: hdr.Identity.IssuerId}, &v1.SubmitInputCommand{Header: hdr, SessionId: id, InputKind: "ANSWER", TaskId: task.TaskId, RequestRef: question, ContentRef: task.GoalRef, ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1})
			results <- result{r, e}
		}(i, harness)
	}
	wins := 0
	for range 2 {
		out := <-results
		if out.e != nil {
			t.Fatal(out.e)
		}
		if out.r.Decision == v1.Decision_DECISION_ACCEPTED {
			wins++
		} else if out.r.Error.Code != "REQUEST_INVALID" {
			t.Fatalf("wrong rejection: %v", out.r)
		}
	}
	after, _ := h.Tasks.QueryTask(ctx, caller, task.TaskId)
	s, _ := other.Sessions.QuerySession(ctx, caller, id)
	if wins != 1 || after.InputVersion != 2 || after.ControlGeneration != task.ControlGeneration+1 || after.BoundInputVersion != 1 || s.LastCommittedSeq != 2 || s.Inputs[1].TaskInputSeq != 2 {
		t.Fatalf("answer race: wins=%d task=%v session=%v", wins, after, s)
	}
}

// 规则：G2、G4、G6
func TestExplicitReplacementMustMatchSavedUserConditions(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	old := bind(t, h, task)
	conditions := []*v1.Requirement{{ConditionId: "new-condition", DescriptionRef: task.GoalRef, Necessary: true, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}
	r, e := h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("explicit-change"), SessionId: id, InputKind: "MODIFY", TaskId: task.TaskId, ContentRef: task.GoalRef, ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1, ExplicitConditions: conditions})
	accepted(t, r, e)
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	hdr := header("altered-conditions")
	hdr.Identity.IssuerId = "host"
	host := &v1.Caller{UserId: "alice", IssuerId: "host"}
	command := &v1.ProcessInputCommand{Header: hdr, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: 2, Outcome: "REPLACE", Source: "USER_EXPLICIT", Conditions: []*v1.Requirement{{ConditionId: "weakened", DescriptionRef: task.GoalRef, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}}
	r, e = h.Tasks.ProcessInput(ctx, host, command)
	if e != nil || r.GetError().GetCode() != "INVALID_REQUIREMENTS" {
		t.Fatalf("user conditions weakened: %v %v", r, e)
	}
	command.Header = header("exact-conditions")
	command.Header.Identity.IssuerId = "host"
	command.Conditions = conditions
	r, e = h.Tasks.ProcessInput(ctx, host, command)
	accepted(t, r, e)
	p, _ := h.Tasks.QueryPlanning(ctx, caller, task.TaskId)
	if p.Requirements.Source != "USER_EXPLICIT" || p.Requirements.RequirementsVersion != 2 || p.Requirements.Conditions[0].VerificationRule != "USER_EVALUATION" {
		t.Fatalf("wrong replacement: %v", p)
	}
	historical, e := h.Tasks.QueryRequirements(ctx, caller, old)
	if e != nil || historical.RequirementsVersion != 1 || historical.Conditions[0].ConditionId != "record" {
		t.Fatal("old conditions changed")
	}
}

// 规则：G2、G4、G6
func TestProcessedAnswerWaitsForEarlierBasisWithoutStrandingBinding(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	hdr := header("early-question")
	hdr.Identity.IssuerId = "host"
	host := &v1.Caller{UserId: "alice", IssuerId: "host"}
	r, e := h.Sessions.PublishQuestion(ctx, host, &v1.PublishQuestionCommand{Header: hdr, SessionId: id, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ContentRef: task.GoalRef, ChangesBasis: false, ExpiresAtUnixMs: 4102444800000})
	question := accepted(t, r, e).ResultRef
	r, e = h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("early-answer"), SessionId: id, TaskId: task.TaskId, InputKind: "ANSWER", ContentRef: task.GoalRef, ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1, RequestRef: question})
	accepted(t, r, e)
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if task.InputVersion != 2 || task.BoundInputVersion != 0 {
		t.Fatal("answer bypassed original unaccepted goal")
	}
	hdr = header("accept-original")
	hdr.Identity.IssuerId = "host"
	r, e = h.Tasks.AcceptRequirements(ctx, host, &v1.AcceptRequirementsCommand{Header: hdr, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: 1, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "c", DescriptionRef: task.GoalRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}})
	accepted(t, r, e)
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if task.BoundInputVersion != 2 {
		t.Fatalf("processed answer stranded: %v", task)
	}
}

// 规则：G3、G4
func TestQuestionResponseRequiresEntireOriginalReference(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	hdr := header("exact-question")
	hdr.Identity.IssuerId = "host"
	r, e := h.Sessions.PublishQuestion(ctx, &v1.Caller{UserId: "alice", IssuerId: "host"}, &v1.PublishQuestionCommand{Header: hdr, SessionId: id, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ContentRef: task.GoalRef, ChangesBasis: true, ExpiresAtUnixMs: 4102444800000})
	ref := proto.Clone(accepted(t, r, e).ResultRef).(*v1.Ref)
	digest := "wrong-binding"
	ref.Digest = &digest
	r, e = h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("wrong-reference"), SessionId: id, InputKind: "ANSWER", TaskId: task.TaskId, RequestRef: ref, ContentRef: task.GoalRef, ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1})
	if e != nil || r.GetError().GetCode() != "STALE_REFERENCE" {
		t.Fatalf("partial request reference accepted: %v %v", r, e)
	}
}

// 规则：G2、G3、G4、G6、准入-3
func TestExplicitClarificationCompletesOriginalGoalInInputOrder(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	host := &v1.Caller{UserId: "alice", IssuerId: "host"}
	hdr := header("ask-for-conditions")
	hdr.Identity.IssuerId = "host"
	r, e := h.Tasks.ProcessInput(ctx, host, &v1.ProcessInputCommand{Header: hdr, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: 1, Outcome: "CLARIFY"})
	accepted(t, r, e)
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	hdr = header("explicit-question")
	hdr.Identity.IssuerId = "host"
	r, e = h.Sessions.PublishQuestion(ctx, host, &v1.PublishQuestionCommand{Header: hdr, SessionId: id, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ContentRef: task.GoalRef, ChangesBasis: true, ExpiresAtUnixMs: 4102444800000})
	question := accepted(t, r, e).ResultRef
	conditions := []*v1.Requirement{{ConditionId: "clarified", DescriptionRef: task.GoalRef, Necessary: true, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}
	r, e = h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("explicit-answer"), SessionId: id, InputKind: "ANSWER", TaskId: task.TaskId, RequestRef: question, ContentRef: task.GoalRef, ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1, ExplicitConditions: conditions})
	answer := accepted(t, r, e).ResultRef
	for _, version := range []uint64{1, 2} {
		task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
		hdr = header(fmt.Sprintf("bind-clarification-%d", version))
		hdr.Identity.IssuerId = "host"
		r, e = h.Tasks.ProcessInput(ctx, host, &v1.ProcessInputCommand{Header: hdr, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: version, Outcome: "REPLACE", Source: "USER_EXPLICIT", Conditions: conditions, SourceInputRef: answer})
		accepted(t, r, e)
		task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
		if task.BoundInputVersion != version {
			t.Fatalf("skipped processing order: %v", task)
		}
		if version == 1 {
			proposal := propose(t, h, task, "clarifying")
			r, e = h.Tasks.Admit(ctx, caller, &v1.AdmitCommand{Header: header("clarification-intermediate-admit"), TaskId: task.TaskId, ProposalRef: proposal})
			if e != nil || r.GetError().GetCode() != "REQUIREMENTS_NOT_ACCEPTED" {
				t.Fatalf("intermediate input bypass: %v %v", r, e)
			}
		}
	}
	p, e := h.Tasks.QueryPlanning(ctx, caller, task.TaskId)
	if e != nil || p.Requirements.Source != "USER_EXPLICIT" || !proto.Equal(p.Requirements.SourceInputRef, answer) || task.BoundInputVersion != 2 || len(task.WaitingOn) != 0 {
		t.Fatalf("clarification unresolved: %v %v %v", task, p, e)
	}
}

// 规则：G2、G4、G6、G12
func TestExplicitConditionSourceCannotCrossTaskOrReferenceVersion(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	conditions := []*v1.Requirement{{ConditionId: "c", DescriptionRef: task.GoalRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}
	r, e := h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("other-explicit-task"), SessionId: id, InputKind: "GOAL", ContentRef: task.GoalRef, ExplicitConditions: conditions})
	otherInput := accepted(t, r, e).ResultRef
	r, e = h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("own-explicit-input"), SessionId: id, InputKind: "MODIFY", TaskId: task.TaskId, ContentRef: task.GoalRef, ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1, ExplicitConditions: conditions})
	ownInput := accepted(t, r, e).ResultRef
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	wrongVersion := proto.Clone(ownInput).(*v1.Ref)
	wrongVersion.Revision = 2
	for i, source := range []*v1.Ref{otherInput, wrongVersion} {
		hdr := header(fmt.Sprintf("bad-source-%d", i))
		hdr.Identity.IssuerId = "host"
		r, e = h.Tasks.ProcessInput(ctx, &v1.Caller{UserId: "alice", IssuerId: "host"}, &v1.ProcessInputCommand{Header: hdr, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: 1, Outcome: "REPLACE", Source: "USER_EXPLICIT", Conditions: conditions, SourceInputRef: source})
		if e != nil || r.GetError().GetCode() != "INVALID_REQUIREMENTS" {
			t.Fatalf("foreign/version source accepted: %v %v", r, e)
		}
	}
	after, _ := h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if after.BoundInputVersion != 0 || after.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_DRAFT {
		t.Fatal("rejected provenance changed basis")
	}
}

// 规则：G3、G4、G11
func TestInputProcessingPreservesExistingCancellationResponsibility(t *testing.T) {
	h := open(t)
	id := session(t, h)
	task := goal(t, h, id)
	bind(t, h, task)
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	r, e := h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("keep-cancel"), SessionId: id, InputKind: "CONTROL", TaskId: task.TaskId, Control: "CANCEL", ExpectedControlGeneration: task.ControlGeneration})
	accepted(t, r, e)
	r, e = h.Sessions.SubmitInput(ctx, caller, &v1.SubmitInputCommand{Header: header("change-after-cancel"), SessionId: id, InputKind: "MODIFY", TaskId: task.TaskId, ContentRef: task.GoalRef, ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1})
	accepted(t, r, e)
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	hdr := header("process-cancelled-change")
	hdr.Identity.IssuerId = "host"
	r, e = h.Tasks.ProcessInput(ctx, &v1.Caller{UserId: "alice", IssuerId: "host"}, &v1.ProcessInputCommand{Header: hdr, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: 2, Outcome: "UNCHANGED", Source: "TRUSTED_TEMPLATE"})
	accepted(t, r, e)
	task, _ = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if task.Control != v1.TaskControl_TASK_CONTROL_CANCELLING || task.Progress != v1.TaskProgress_TASK_PROGRESS_WAITING || len(task.WaitingOn) != 1 || task.WaitingOn[0] != "CANCELLATION_CLOSURE" {
		t.Fatalf("processing erased unrelated responsibility: %v", task)
	}
}
