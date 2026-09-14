// Package tasks provides local durable task admission and the trusted RunStore seam.
package tasks

import (
	"bytes"
	"context"
	"encoding/gob"
	"lerna/authorization"
	"lerna/internal/randomid"
	"reflect"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

type Ref struct{ Namespace, TaskID string }
type Constraints struct {
	ModelRequests uint32
	ModelTokens   uint64
	MaxSteps      uint32
	DeadlineUnix  int64
}
type Submission struct {
	Namespace, OperationID, Goal string
	InputRefs                    []string
	Constraints                  Constraints
}
type Task struct {
	DelegatedSteps, DelegatedRequests        uint32
	DelegatedTokens                          uint64
	InputFacts                               []InputFact
	ModelUsedRequests, ModelReservedRequests uint32
	ModelUsedTokens, ModelReservedTokens     uint64
	Ref                                      Ref
	Goal                                     string
	GoalRef, GuidanceRef                     string
	InputRefs                                []string
	Constraints                              Constraints
	State, Subject, Resource, Owner          string
	OwnerEpoch, Version                      uint64
	Attempts                                 uint32
	Result, StopReason                       string
	Control                                  ControlState
	WaitingReasons                           []string
}
type Work struct {
	ExecutionOperation string
	ID, Kind           string
	Worker             string
	Generation         uint64
	LeaseUntil         int64
	Done               bool
	InFlight           bool
	DecisionVersion    uint64
	EffectAware        bool
}
type Record struct {
	Kind    string
	Version uint64
}
type CommitReceipt struct {
	Ref      Ref
	ChangeID string
	Version  uint64
}
type RunSnapshot struct {
	ChildReport                   *ChildReport
	ChildReportChanges            uint32
	ChildDispositionReportChanges uint32
	ParentCancelOperation         string
	Delegations                   *DelegationState
	Parent                        *ChildIntent
	Actions                       *ActionState
	ExecutionReportVersions       map[string]uint64
	ExecutionReports              map[string]ExecutionReport
	Interactions                  []Interaction
	UpdateVersion                 uint64
	UpdateLimits                  UpdateLimits
	Generations                   []GenerationReservation
	ControlLimits                 ControlLimits
	CheckAlreadyReserved          bool
	DispositionChecks             int
	CheckedAt                     int64
	Limits                        RunLimits
	LastCommit                    CommitReceipt
	Task                          Task
	Work                          []Work
	Records                       []Record
}
type RunChange struct {
	ChangeID        string
	MustNotExist    bool
	ExpectedVersion uint64
	Task            Task
	Work            []Work
	Records         []Record
}
type RecoveryQuery struct {
	Namespace, After string
	Limit            int
}
type RunPage struct {
	Runs []RunSnapshot
	Next string
}
type Config struct {
	Namespace, Resource, Owner string
	MaxTasks, MaxPage          int
}
type RuntimeStore interface {
	UpdateRuntime(context.Context, func(authorization.RuntimeTransaction) error) error
}
type RunStore interface {
	Load(context.Context, Ref) (RunSnapshot, error)
	Commit(context.Context, RunChange) (CommitReceipt, error)
	LookupCommit(context.Context, Ref, string) (CommitReceipt, error)
	ListRecoverable(context.Context, RecoveryQuery) (RunPage, error)
}
type Service struct {
	childPolicy ChildPolicy
	store       RuntimeStore
	config      Config
}
type commit struct {
	WaitRequest      *WaitRequest
	InputOperation   string
	Change           RunChange
	Receipt          CommitReceipt
	WorkerID         string
	WorkLimits       RunLimits
	ProposalDigest   [32]byte
	DispositionCheck Ref
	ControlChange    *ControlRequest
	Observation      *Observation
	WorkChange       *WorkChange
	Snapshot         RunSnapshot
}
type operation struct {
	Subject string
	Input   Submission
	Ref     Ref
}
type journal struct {
	ClosedDelegations map[string]ChildReference
	InputChanges      map[string]inputRecord
	Format            int
	Config            Config
	Runs              map[string]RunSnapshot
	Commits           map[string]map[string]commit
	Controls          map[string]controlRecord
	ControlLimits     ControlLimits
	Operations        map[string]operation
}

func failure(code authorization.Code) error { return &authorization.Error{Code: code} }
func New(store RuntimeStore, c Config) (*Service, error) {
	if store == nil || !name(c.Namespace) || !name(c.Resource) || !name(c.Owner) || c.MaxTasks < 1 || c.MaxTasks > 10000 || c.MaxPage < 1 || c.MaxPage > 1000 {
		return nil, failure(authorization.Invalid)
	}
	return &Service{store: store, config: c}, nil
}
func name(s string) bool {
	return len(s) > 0 && len(s) <= 128 && !strings.ContainsAny(s, "\x00\r\n") && utf8.ValidString(s)
}
func (s *Service) transaction(ctx context.Context, fn func(*journal, authorization.RuntimeTransaction) error) error {
	return s.store.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error { return s.runtime(tx, fn) })
}
func (s *Service) runtime(tx authorization.RuntimeTransaction, fn func(*journal, authorization.RuntimeTransaction) error) error {
	if tx.Namespace() != s.config.Namespace {
		return failure(authorization.Denied)
	}
	j := journal{Format: 1, Config: s.config, Runs: map[string]RunSnapshot{}, Commits: map[string]map[string]commit{}, Operations: map[string]operation{}}
	if len(tx.Data()) > 0 {
		if err := gob.NewDecoder(bytes.NewReader(tx.Data())).Decode(&j); err != nil {
			return failure(authorization.Unavailable)
		}
	}
	if j.Format != 1 || j.Config != s.config {
		return failure(authorization.Invalid)
	}
	if j.ClosedDelegations == nil {
		j.ClosedDelegations = map[string]ChildReference{}
	}
	if j.InputChanges == nil {
		j.InputChanges = map[string]inputRecord{}
	}
	if j.Controls == nil {
		j.Controls = map[string]controlRecord{}
	}
	if err := fn(&j, tx); err != nil {
		return err
	}
	for _, r := range j.Runs {
		if e := delegationInvariant(r); e != nil {
			return e
		}
	}
	var data bytes.Buffer
	if err := gob.NewEncoder(&data).Encode(j); err != nil {
		return failure(authorization.Invalid)
	}
	if data.Len() > 8<<20 {
		return failure(authorization.Unavailable)
	}
	tx.SetData(data.Bytes())
	return nil
}

func validInput(in Submission) bool {
	if (in.Constraints.ModelRequests == 0) != (in.Constraints.ModelTokens == 0) || in.Constraints.ModelRequests > 64 || in.Constraints.ModelTokens > 1048576 {
		return false
	}
	if !name(in.Namespace) || in.OperationID == "" || len(in.Goal) == 0 || len(in.Goal) > 65536 || !utf8.ValidString(in.Goal) || in.Constraints.MaxSteps == 0 || in.Constraints.MaxSteps > 10000 || in.Constraints.DeadlineUnix <= 0 || len(in.InputRefs) > 64 {
		return false
	}
	for _, ref := range in.InputRefs {
		if !name(ref) {
			return false
		}
	}
	return true
}
func (s *Service) Submit(ctx context.Context, token string, in Submission) (Task, error) {
	if !validInput(in) {
		return Task{}, failure(authorization.Invalid)
	}
	in.InputRefs = append([]string(nil), in.InputRefs...)
	taskID, err := randomid.New()
	if err != nil {
		return Task{}, err
	}
	changeID, err := randomid.New()
	if err != nil {
		return Task{}, err
	}
	var out Task
	err = s.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		identity, err := tx.Authorize(token, s.config.Resource, "task.submit")
		if err != nil {
			return err
		}
		if in.Namespace != identity.Namespace {
			return failure(authorization.Denied)
		}
		if err := tx.Operation(in.OperationID, identity.Subject, false); err != nil {
			return err
		}
		if delegationOperation(j, in.OperationID) {
			return failure(authorization.IdentityConflict)
		}
		if _, ok := j.InputChanges[in.OperationID]; ok {
			return failure(authorization.IdentityConflict)
		}
		if _, ok := j.Controls[in.OperationID]; ok {
			return failure(authorization.IdentityConflict)
		}
		if old, ok := j.Operations[in.OperationID]; ok {
			if old.Subject != identity.Subject {
				return failure(authorization.Denied)
			}
			if !reflect.DeepEqual(old.Input, in) {
				return failure(authorization.IdentityConflict)
			}
			if _, err := tx.Authorize(token, s.config.Resource, "task.read"); err != nil {
				return err
			}
			out = j.Runs[old.Ref.TaskID].Task
			return nil
		}
		if in.Constraints.DeadlineUnix <= tx.Now().Unix() {
			return failure(authorization.Expired)
		}
		if err := tx.Operation(in.OperationID, identity.Subject, true); err != nil {
			return err
		}
		task := Task{Ref: Ref{in.Namespace, taskID}, Goal: in.Goal, InputRefs: in.InputRefs, Constraints: in.Constraints, State: "QUEUED", Subject: identity.Subject, Resource: s.config.Resource, Owner: s.config.Owner, OwnerEpoch: 1, Version: 1}
		change := RunChange{ChangeID: changeID, MustNotExist: true, Task: task, Work: []Work{{ID: taskID + "/initial", Kind: "decide"}}, Records: []Record{{Kind: "submitted", Version: 1}}}
		if _, err := s.commit(j, change); err != nil {
			return err
		}
		j.Operations[in.OperationID] = operation{identity.Subject, in, task.Ref}
		out = task
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}
func (s *Service) Get(ctx context.Context, token string, ref Ref) (Task, error) {
	var out Task
	err := s.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		identity, err := tx.Authorize(token, s.config.Resource, "task.read")
		if err != nil {
			return err
		}
		if ref.Namespace != identity.Namespace {
			return failure(authorization.Denied)
		}
		run, ok := j.Runs[ref.TaskID]
		if !ok {
			return failure(authorization.NotFound)
		}
		if run.Task.Subject != identity.Subject {
			return failure(authorization.Denied)
		}
		out = run.Task
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}
func (s *Service) LookupOperation(ctx context.Context, token, namespace, id string) (Task, error) {
	var out Task
	err := s.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		identity, err := tx.Authorize(token, s.config.Resource, "task.read")
		if err != nil {
			return err
		}
		if namespace != identity.Namespace {
			return failure(authorization.Denied)
		}
		if err := tx.Operation(id, identity.Subject, false); err != nil {
			return err
		}
		op, ok := j.Operations[id]
		if !ok {
			return failure(authorization.NotFound)
		}
		if op.Subject != identity.Subject {
			return failure(authorization.Denied)
		}
		out = j.Runs[op.Ref.TaskID].Task
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}

// The remaining methods are trusted in-process RunStore operations, not business RPCs.
func (s *Service) commit(j *journal, c RunChange) (CommitReceipt, error) {
	c.Task.InputRefs = append([]string(nil), c.Task.InputRefs...)
	if c.Task.Ref.Namespace != s.config.Namespace || !name(c.Task.Ref.TaskID) || !name(c.ChangeID) {
		return CommitReceipt{}, failure(authorization.Invalid)
	}
	if old, ok := j.Commits[c.Task.Ref.TaskID][c.ChangeID]; ok {
		if old.WaitRequest != nil || old.InputOperation != "" || old.WorkChange != nil || old.ControlChange != nil || old.Observation != nil || old.DispositionCheck != (Ref{}) || !reflect.DeepEqual(old.Change, c) {
			return CommitReceipt{}, failure(authorization.IdentityConflict)
		}
		return old.Receipt, nil
	}
	old, exists := j.Runs[c.Task.Ref.TaskID]
	if exists {
		if c.MustNotExist || c.ExpectedVersion != old.Task.Version {
			return CommitReceipt{}, failure(authorization.Conflict)
		}
		if c.Task.Owner != old.Task.Owner || c.Task.OwnerEpoch != old.Task.OwnerEpoch {
			return CommitReceipt{}, failure(authorization.Denied)
		}
		return CommitReceipt{}, failure(authorization.Unsupported)
	}
	if !validInput(Submission{Namespace: c.Task.Ref.Namespace, OperationID: "internal", Goal: c.Task.Goal, InputRefs: c.Task.InputRefs, Constraints: c.Task.Constraints}) {
		return CommitReceipt{}, failure(authorization.Invalid)
	}
	if !c.MustNotExist || c.ExpectedVersion != 0 {
		return CommitReceipt{}, failure(authorization.Conflict)
	}
	if len(c.Task.InputFacts) != 0 || c.Task.GoalRef != "" || c.Task.GuidanceRef != "" || c.Task.ModelUsedRequests != 0 || c.Task.ModelReservedRequests != 0 || c.Task.ModelUsedTokens != 0 || c.Task.ModelReservedTokens != 0 || c.Task.Control != (ControlState{}) || len(c.Task.WaitingReasons) != 0 || c.Task.Attempts != 0 || c.Task.Result != "" || c.Task.StopReason != "" || c.Task.State != "QUEUED" || c.Task.Version != 1 || c.Task.Owner != s.config.Owner || c.Task.OwnerEpoch != 1 || c.Task.Resource != s.config.Resource || !name(c.Task.Subject) || len(c.Work) != 1 || c.Work[0].ID != c.Task.Ref.TaskID+"/initial" || c.Work[0] != (Work{ID: c.Task.Ref.TaskID + "/initial", Kind: "decide"}) || len(c.Records) != 1 || c.Records[0] != (Record{Kind: "submitted", Version: 1}) {
		return CommitReceipt{}, failure(authorization.Unsupported)
	}
	if len(j.Runs) >= s.config.MaxTasks {
		return CommitReceipt{}, failure(authorization.Unavailable)
	}
	receipt := CommitReceipt{c.Task.Ref, c.ChangeID, 1}
	j.Runs[c.Task.Ref.TaskID] = RunSnapshot{Task: c.Task, Work: c.Work, Records: c.Records, LastCommit: receipt}
	j.Commits[c.Task.Ref.TaskID] = map[string]commit{c.ChangeID: {Change: c, Receipt: receipt}}
	return receipt, nil
}
func (s *Service) Commit(ctx context.Context, c RunChange) (CommitReceipt, error) {
	var out CommitReceipt
	err := s.transaction(ctx, func(j *journal, _ authorization.RuntimeTransaction) error {
		var err error
		out, err = s.commit(j, c)
		return err
	})
	if err != nil {
		return CommitReceipt{}, err
	}
	return out, nil
}
func (s *Service) Load(ctx context.Context, ref Ref) (RunSnapshot, error) {
	var out RunSnapshot
	err := s.transaction(ctx, func(j *journal, _ authorization.RuntimeTransaction) error {
		if ref.Namespace != s.config.Namespace {
			return failure(authorization.Denied)
		}
		var ok bool
		out, ok = j.Runs[ref.TaskID]
		if !ok {
			return failure(authorization.NotFound)
		}
		return nil
	})
	if err != nil {
		return RunSnapshot{}, err
	}
	return out, nil
}
func (s *Service) LookupCommit(ctx context.Context, ref Ref, id string) (CommitReceipt, error) {
	var out CommitReceipt
	err := s.transaction(ctx, func(j *journal, _ authorization.RuntimeTransaction) error {
		if ref.Namespace != s.config.Namespace {
			return failure(authorization.Denied)
		}
		c, ok := j.Commits[ref.TaskID][id]
		if !ok {
			return failure(authorization.NotFound)
		}
		out = c.Receipt
		return nil
	})
	if err != nil {
		return CommitReceipt{}, err
	}
	return out, nil
}
func (s *Service) ListRecoverable(ctx context.Context, q RecoveryQuery) (RunPage, error) {
	if q.Limit < 1 || q.Limit > s.config.MaxPage || len(q.After) > 128 {
		return RunPage{}, failure(authorization.Invalid)
	}
	var out RunPage
	err := s.transaction(ctx, func(j *journal, _ authorization.RuntimeTransaction) error {
		if q.Namespace != s.config.Namespace {
			return failure(authorization.Denied)
		}
		ids := []string{}
		for id, run := range j.Runs {
			if id > q.After && controlIntent(run.Task) == "RUN" && (run.Task.State == "QUEUED" || run.Task.State == "RUNNING") {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		out = RunPage{}
		for i, id := range ids {
			if i == q.Limit {
				out.Next = ids[i-1]
				break
			}
			out.Runs = append(out.Runs, j.Runs[id])
		}
		return nil
	})
	if err != nil {
		return RunPage{}, err
	}
	return out, nil
}

// ValidSnapshot checks the public read shape; it never confers write authority.
func ValidSnapshot(t Task) bool {
	if len(t.InputRefs) > 64 || len(t.InputFacts) > 1024 {
		return false
	}
	for _, ref := range []string{t.GoalRef, t.GuidanceRef} {
		if ref != "" && !slices.Contains(t.InputRefs, ref) {
			return false
		}
	}
	for _, f := range t.InputFacts {
		if !validFactAssociation(f) || (f.Kind == "reply" && !slices.Contains(t.InputRefs, f.QuestionRef)) {
			return false
		}
		if f.OperationID == "" || !name(f.Subject) || !slices.Contains(t.InputRefs, f.Reference) || (f.Kind != "append" && f.Kind != "revise" && f.Kind != "reply") {
			return false
		}
	}
	if t.ModelUsedRequests > t.Constraints.ModelRequests || t.ModelReservedRequests > t.Constraints.ModelRequests-t.ModelUsedRequests || t.ModelUsedTokens > t.Constraints.ModelTokens || t.ModelReservedTokens > t.Constraints.ModelTokens-t.ModelUsedTokens {
		return false
	}
	if t.Control != (ControlState{}) {
		if controlAction(t.Control.Intent) == "" || t.Control.OperationID == "" || t.Control.AcceptedAt <= 0 {
			return false
		}
		switch t.Control.Progress {
		case "ACCEPTED", "APPLIED", "NOT_PREVENTED":
		default:
			return false
		}
	}
	if len(t.WaitingReasons) > 16 {
		return false
	}
	seen := map[string]bool{}
	for _, reason := range t.WaitingReasons {
		if !name(reason) || seen[reason] {
			return false
		}
		seen[reason] = true
	}

	if t.Version == 0 || t.Attempts > t.Constraints.MaxSteps || len(t.Result) > 65536 || !utf8.ValidString(t.Result) {
		return false
	}
	switch t.State {
	case "QUEUED", "RUNNING":
		return t.Result == ""
	case "COMPLETED":
		return t.Result != "" && t.StopReason == "" && t.Attempts > 0
	case "CANCELLED":
		return t.Result == "" && t.Control.Intent == "CANCEL" && t.Control.Progress == "APPLIED"
	case "WAITING", "FAILED":
		return t.Result == "" && t.StopReason != ""
	default:
		return false
	}
}
