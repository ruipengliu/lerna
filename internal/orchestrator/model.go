package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/contracts"
	"github.com/ruipengliu/lerna/internal/durable"
)

var Kinds = []string{"control", "decide", "dispatch", "extract", "poll", "settle", "verify"}

type Limits struct {
	Scan, Page, Tree, ActivePerUser, Children, Depth, Effects, Checks, Executors, Facts, Bytes int
	Backoff, TrustedReview                                                                     time.Duration
}

func DefaultLimits() Limits {
	return Limits{100, 32, 100, 32, 8, 4, 100, 100, 4, 32, 1 << 20, time.Second, time.Minute}
}
func (l Limits) Valid() bool {
	return l.Scan >= 1 && l.Scan <= 100 && l.Page >= 1 && l.Page <= l.Scan && l.Tree >= 1 && l.Tree <= 100 && l.ActivePerUser >= 1 && l.ActivePerUser <= l.Tree && l.Children >= 1 && l.Children <= 8 && l.Depth >= 1 && l.Depth <= 4 && l.Effects >= 1 && l.Effects <= 100 && l.Checks >= 1 && l.Checks <= 100 && l.Executors >= 1 && l.Executors <= 100 && l.ActivePerUser*l.Executors <= 128 && l.Facts >= 1 && l.Facts <= 100 && l.Bytes >= 1024 && l.Bytes <= 1<<20 && l.Backoff >= time.Millisecond && l.TrustedReview >= l.Backoff && l.TrustedReview <= time.Hour
}

type Config struct {
	Scope     durable.Scope
	ServiceID string
	Limits    Limits
}
type TaskState struct {
	Task                                                              api.Task
	SubjectID, ParentID                                               string
	Depth                                                             int
	CreatedAt                                                         int64
	Policy                                                            api.TaskPolicy
	Constraints                                                       []string
	ContinuationCount, RepairCount, NoProgressCount, SnapshotRevision int64
	LastInputDigest, PendingDecision, FailureReason, AllocationID     string
	ProjectionComplete                                                bool
}
type Record struct {
	Kind, ID, TaskID, State, CurrentKey string
	Revision                            int64
	Immutable                           bool
	Data                                json.RawMessage
}
type Filter struct {
	TaskID, Kind, State, Current, After string
	Limit                               int
}

// Repository owns adapter capabilities. Domain code receives only the current
// restricted Tx; no engine, session or SQL handle crosses this port.
type Repository interface {
	GetMany(*durable.Tx, string, []string) ([]Record, error)
	PutMany(*durable.Tx, *TaskState, []Record) error
	DeselectKeys(*durable.Tx, *TaskState, string, []string) error
	Routes(*durable.Tx, *TaskState, []WorkRef) error
	Read(*durable.Tx) error
	Task(*durable.Tx, string) (*TaskState, error)
	Chain(*durable.Tx, string) ([]*TaskState, error)
	Tree(*durable.Tx, string, int) ([]*TaskState, error)
	LockTasks(*durable.Tx, []*TaskState) ([]*TaskState, error)
	SaveTask(*durable.Tx, *TaskState) error
	LockCapacity(*durable.Tx, []string) error
	Capacity(*durable.Tx, string, int64, int) error
	LockBalances(*durable.Tx, []string) (map[string][]api.BudgetBalance, error)
	Balances(*durable.Tx, string) ([]api.BudgetBalance, error)
	SaveBalance(*durable.Tx, string, api.BudgetBalance) error
	SaveBalances(*durable.Tx, string, []api.BudgetBalance) error
	Get(*durable.Tx, string, string) (*Record, error)
	Records(*durable.Tx, Filter) ([]Record, error)
	OpenRecords(*durable.Tx, string, string, int) ([]Record, error)
	Put(*durable.Tx, *TaskState, Record) error
	Route(*durable.Tx, *TaskState, WorkRef) error
	Deselect(*durable.Tx, *TaskState, string, string) error
	Gate(*durable.Tx, string, bool, bool) (int64, error)
	AdvanceGate(*durable.Tx, string) error
	PutGlobal(*durable.Tx, string, Record) error
	Receiver(*durable.Tx, string) (*ReceiverState, error)
	SaveReceiver(*durable.Tx, *ReceiverState) error
	List(*durable.Tx, int64, int64, string, int) ([]*TaskState, error)
	Recovery(*durable.Tx, string, int) ([]*TaskState, error)
}

// Unit is constructed by trusted assembly around Work.Within and registered
// participants. It preserves the original claim and context.
type Unit interface {
	Claim() durable.Claim
	Step(context.Context, func() error) error
	Within(context.Context, func(*durable.Tx) error) durable.Result
}
type Snapshot struct {
	TaskID                                  string
	Revision, GoalRevision, ControlRevision int64
	InputDigest                             string
	Request                                 api.DecisionRequest
	Call                                    api.OriginalCall
	ReservationID                           string
	Inputs                                  []api.ContentRef
}
type Intent struct {
	Invoke                              api.Invoke
	Call                                api.OriginalCall
	SourceKind, SourceID, ReservationID string
	Preparation                         api.ActionPreparation
	RequirementIDs                      []string
}
type Reservation struct {
	ID, TaskID, ObjectOwnerID, ObjectKind, ObjectID string
	Source                                          *api.SourceKey
	UpperBound, Usage                               []api.Amount
	UsageRevision                                   int64
	UsageDigest                                     string
	Final, Allocation                               bool
}
type AllocationState struct {
	Allocation                             api.RuntimeBudgetAllocation
	CommandID, ReservationID, DelegationID string
	UsageDigest                            string
	Settlement                             *api.OriginalCall
}
type ReceiverState struct {
	Receiver            api.RuntimeBudgetReceiver
	AllocationCommandID string
	ParentTaskID        string
	Limits              []api.BudgetLimit
	ClosedUsageRevision int64
	Publication         *Publication
	PendingClosure      *api.RuntimeBudgetClosure
	Outbox              *Outbox
}
type ControlState struct {
	TaskID, ExecutorID            string
	GoalRevision, ControlRevision int64
	Call                          *api.OriginalCall
	Snapshot                      *api.ControlSnapshot
	EnforcedGoal, EnforcedControl int64
	Attempts                      int64
}
type InputState struct {
	View                   api.InputRequestView
	GoalRevision           int64
	ConfirmationRef        *api.ObjectRef
	AcceptanceRequirements []string
}
type Consumption struct {
	DecisionID, Digest, Conclusion string
	GoalRevision, SnapshotRevision int64
}
type Fact struct {
	OwnerID, ObjectKind, ObjectID, Digest string
	Revision                              int64
	Value                                 json.RawMessage
}
type Candidate struct {
	Artifacts    []api.ContentRef
	GoalRevision int64
	Requirements []string
}
type Delegation struct {
	ID, TaskID, ChildID, ReceiverID, AllocationID string
	Request                                       api.DelegationRequest
	Call                                          api.OriginalCall
	Internal                                      bool
	Observation                                   *api.DelegationObservation
	Control                                       *api.OriginalCall
}
type WorkRef struct{ TaskID, Kind, ObjectID, ObjectKind, SubjectID, ProviderID, ResourceID string }
type Outbox struct {
	TaskID, ReceiverID, AllocationID, Digest string
	UsageRevision                            int64
	Call                                     api.OriginalCall
	Previous                                 []api.OriginalCall
}
type Defect = api.EvidenceDefect
type DefectImpact struct {
	Defect                    Defect
	UpperBound, LastCreatedAt int64
	LastTaskID                string
}
type QueryScope struct{ ActorID, Generation, FilterDigest, UpperBound string }

type Publication struct {
	ID, TaskID, MediaType, Digest string
	Body                          json.RawMessage
	Ref                           *api.ContentRef
}
type InputPreparation struct {
	SourceID               string
	GoalRevision           int64
	RequestID              string
	Proposal               api.Proposal
	PublicationID          string
	Deadline               string
	Candidate              *api.ContentRef
	AcceptanceRequirements []string
}
type AttemptState struct {
	Count               int64
	Observed            int64
	LastTrustedRevision int64
}

type SnapshotPreparation struct {
	Snapshot      Snapshot
	PublicationID string
}
type EffectState struct {
	OwnerID      string
	Operation    api.Operation
	GoalRevision int64
}
type ExtractionState struct {
	Call   api.OriginalCall
	Source api.ContentRef
	Ack    string
}
type Attachment struct {
	OperationID string
	Refs        []api.ContentRef
	Call        api.OriginalCall
}

func ID(prefix string, values ...string) string {
	h := sha256.New()
	for _, v := range values {
		fmt.Fprintf(h, "%d:%s", len(v), v)
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:32]
}
func Hash(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	canonical, err := durable.CanonicalJSON(b)
	if err != nil {
		panic(err)
	}
	h := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(h[:])
}
func Raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func validateValue(definition string, v any) error {
	nodes := 100000
	var walk func(reflect.Value) bool
	walk = func(v reflect.Value) bool {
		nodes--
		if nodes < 0 {
			return false
		}
		switch v.Kind() {
		case reflect.Interface, reflect.Pointer:
			if !v.IsNil() {
				return walk(v.Elem())
			}
		case reflect.String:
			return utf8.ValidString(v.String())
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if !walk(v.Field(i)) {
					return false
				}
			}
		case reflect.Slice, reflect.Array:
			if v.Type().Elem().Kind() == reflect.Uint8 {
				return v.Len() <= 1<<20
			}
			for i := 0; i < v.Len(); i++ {
				if !walk(v.Index(i)) {
					return false
				}
			}
		case reflect.Map:
			it := v.MapRange()
			for it.Next() {
				if !walk(it.Key()) || !walk(it.Value()) {
					return false
				}
			}
		}
		return true
	}
	if !walk(reflect.ValueOf(v)) {
		return failure("invalid_output", "value exceeds finite Unicode/structure scope")
	}
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if _, e = durable.CanonicalJSON(b); e != nil {
		return e
	}
	if definition == "" {
		return nil
	}
	return contracts.Validate(definition, b)
}
func Decode[T any](r *Record) (T, error) {
	var v T
	if r == nil {
		return v, failure("not_found", "original record is absent")
	}
	err := json.Unmarshal(r.Data, &v)
	return v, err
}
func rec(kind, id, task string, revision int64, state string, immutable bool, value any) Record {
	return Record{Kind: kind, ID: id, TaskID: task, Revision: revision, State: state, Immutable: immutable, Data: Raw(value)}
}
func failure(code, message string) *api.Failure {
	retry := "after_change"
	if code == "dependency_unavailable" || code == "overloaded" {
		retry = "same_command"
	}
	if code == "not_found" || code == "gone" {
		retry = "none"
	}
	return &api.Failure{Detail: api.Error{Code: code, Message: message, Retry: retry}}
}
func stamp(t time.Time) string   { return t.UTC().Format(time.RFC3339Nano) }
func terminal(t *TaskState) bool { return t.Task.Status != "active" }
func sortedTasks(tasks []*TaskState) []*TaskState {
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Depth != tasks[j].Depth {
			return tasks[i].Depth < tasks[j].Depth
		}
		return tasks[i].Task.TaskID < tasks[j].Task.TaskID
	})
	return tasks
}
func workKey(w WorkRef) durable.JobKey {
	return durable.JobKey{Kind: w.Kind, Responsibility: ID("work", w.TaskID, w.Kind, w.ObjectKind, w.ObjectID)}
}
func WorkKey(w WorkRef) durable.JobKey    { return workKey(w) }
func gateKey(ref api.ComponentRef) string { return ID("evaluator", ref.ID, ref.Version, ref.Digest) }
