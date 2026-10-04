// Package interaction 保存应用输入和投递，不裁决目标效果、许可或任务成功。
package interaction

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const (
	JobDispatch   = "interaction.dispatch"
	JobTrigger    = "interaction.trigger"
	JobOccurrence = "interaction.occurrence"
	sessions      = "interaction.sessions"
	branches      = "interaction.branches"
	messages      = "interaction.messages"
	submissions   = "interaction.submissions"
	queues        = "interaction.queues"
	schedules     = "interaction.schedules"
	occurrences   = "interaction.occurrences"
	skips         = "interaction.skips"
	surfaces      = "interaction.surfaces"
	presentations = "interaction.presentations"
)

// CheckTx 只核同库当前门禁；Read 是事务外准确字节读取。
type ContentPort interface {
	CheckTx(context.Context, runtime.Tx, runtime.Auth, api.ContentRef, string) error
	Read(context.Context, runtime.Scope, runtime.Auth, api.ContentRef, string) ([]byte, error)
}
type DeliveryPort interface {
	Send(context.Context, runtime.Scope, runtime.Auth, api.Command) (api.Receipt, error)
	Lookup(context.Context, runtime.Scope, runtime.Auth, string, string) (api.Receipt, error)
}
type Closure struct {
	TaskRef        api.ObjectRef  `json:"task_ref"`
	GoalWorkClosed bool           `json:"goal_work_closed"`
	EffectsClosed  bool           `json:"effects_closed"`
	ClosureRef     api.ContentRef `json:"closure_ref"`
}
type ClosurePort interface {
	Closure(context.Context, runtime.Scope, runtime.Auth, api.ObjectRef) (Closure, error)
}
type RequestView struct {
	Request      api.InputRequest `json:"request"`
	AnswerSchema json.RawMessage  `json:"answer_schema"`
	Method       string           `json:"method"`
}
type RequestPort interface {
	CheckTx(context.Context, runtime.Tx, runtime.Auth, api.ObjectRef) (RequestView, error)
}

// 多个请求须由负责方一次确定全部上游锁；不能逐项追加 Task 锁。
type RequestBatchPort interface {
	CheckBatchTx(context.Context, runtime.Tx, runtime.Auth, []api.ObjectRef) ([]RequestView, error)
}
type ScheduleGate interface {
	CheckTx(context.Context, runtime.Tx, runtime.Auth, api.ComponentRef, api.ComponentRef, []api.Amount) error
}
type Calendar interface {
	Load(string, string) (*time.Location, error)
}

// SubjectGate 在原读取事务中核当前凭据与角色，不读取正文或出站。
// 缺少当前负责方时拒绝披露，不能以静态 Auth 代替当前授权。
type SubjectGate interface {
	CheckSubjectTx(context.Context, runtime.Tx, runtime.Auth) error
}
type Ports struct {
	SubjectGate  SubjectGate
	Content      ContentPort
	Delivery     DeliveryPort
	Closure      ClosurePort
	Requests     RequestPort
	ScheduleGate ScheduleGate
	Calendar     Calendar
}
type Config struct {
	DiscoveryOwnerID  string
	Participants      []string
	BranchQueueLimit  uint64
	SubjectQueueLimit uint64
	MaxBranches       uint64
	DeliveryTTL       time.Duration
	QueueTTL          time.Duration
	CursorKey         []byte
	EventBindings     []EventBinding
}
type Service struct {
	config   Config
	ports    Ports
	bindings map[string]EventBinding
}

func New(config Config, ports Ports) (*Service, error) {
	if config.BranchQueueLimit == 0 {
		config.BranchQueueLimit = 20
	}
	if config.SubjectQueueLimit == 0 {
		config.SubjectQueueLimit = 100
	}
	if config.BranchQueueLimit > 20 || config.SubjectQueueLimit > 100 {
		return nil, fmt.Errorf("unbounded interaction queue")
	}
	if config.MaxBranches == 0 {
		config.MaxBranches = 1000
	}
	if config.MaxBranches > 1000 {
		return nil, fmt.Errorf("unbounded branch collection")
	}
	if config.DeliveryTTL == 0 {
		config.DeliveryTTL = time.Minute
	}
	if config.QueueTTL == 0 {
		config.QueueTTL = 24 * time.Hour
	}
	if config.DeliveryTTL < time.Second || config.DeliveryTTL > time.Hour || config.QueueTTL < time.Second || config.QueueTTL > 30*24*time.Hour {
		return nil, fmt.Errorf("invalid interaction deadlines")
	}
	if len(config.Participants) == 0 {
		config.Participants = []string{"interaction", "content"}
	}
	config.Participants = append([]string(nil), config.Participants...)
	if len(config.CursorKey) == 0 {
		config.CursorKey = make([]byte, 32)
		if _, err := rand.Read(config.CursorKey); err != nil {
			return nil, err
		}
	}
	if len(config.CursorKey) < 32 {
		return nil, fmt.Errorf("cursor key requires at least 32 bytes")
	}
	config.CursorKey = append([]byte(nil), config.CursorKey...)
	bindings, err := validateBindings(config.EventBindings)
	if err != nil {
		return nil, err
	}
	s := &Service{config: config, ports: ports, bindings: bindings}
	return s, nil
}

type ReadInput struct {
	Revision uint64 `json:"revision"`
}
type CreateSessionInput struct {
	SessionID       string           `json:"session_id"`
	DefaultBranchID string           `json:"default_branch_id"`
	ConfigRef       api.ComponentRef `json:"config_ref"`
}
type SessionOutput struct {
	SessionRef api.ObjectRef `json:"session_ref"`
	BranchRef  api.ObjectRef `json:"branch_ref"`
}
type SessionView struct {
	Session                    api.Session  `json:"session"`
	Branches                   []api.Branch `json:"branches"`
	Sequence                   uint64       `json:"sequence"`
	BodyState                  string       `json:"body_state"`
	BranchesComplete           bool         `json:"branches_complete"`
	BranchesCursor             string       `json:"branches_cursor,omitempty"`
	BranchesCollectionRevision uint64       `json:"branches_collection_revision"`
}
type SessionControlInput struct {
	Reason string `json:"reason"`
}
type SelectBranchInput struct {
	BranchID string `json:"branch_id"`
}
type CreateBranchInput struct {
	BranchID               string           `json:"branch_id"`
	SourceBranchRef        api.ObjectRef    `json:"source_branch_ref"`
	ExpectedSourceRevision uint64           `json:"expected_source_revision"`
	SourceHead             string           `json:"source_head,omitempty"`
	ConfigRef              api.ComponentRef `json:"config_ref"`
}
type sessionRecord struct {
	Session         api.Session      `json:"session"`
	SubjectID       string           `json:"subject_id"`
	CreateCommandID string           `json:"create_command_id"`
	Sequence        uint64           `json:"sequence"`
	ConfigRef       api.ComponentRef `json:"config_ref"`
}
type branchRecord struct {
	Branch  api.Branch `json:"branch"`
	Queued  uint64     `json:"queued"`
	Pending []string   `json:"pending"`
}
type queueRecord struct {
	SubjectID string `json:"subject_id"`
	Revision  uint64 `json:"revision"`
	Count     uint64 `json:"count"`
}

type GoalInput struct {
	SessionRef             api.ObjectRef    `json:"session_ref"`
	BranchRef              api.ObjectRef    `json:"branch_ref"`
	ExpectedBranchRevision uint64           `json:"expected_branch_revision"`
	ContentRef             api.ContentRef   `json:"content_ref"`
	AttachmentRefs         []api.ContentRef `json:"attachment_refs"`
	PolicyRef              api.ComponentRef `json:"policy_ref"`
	Budget                 []api.Amount     `json:"budget"`
	TaskDeadline           string           `json:"task_deadline"`
	TargetTaskRef          *api.ObjectRef   `json:"target_task_ref,omitempty"`
	PredecessorTaskRef     *api.ObjectRef   `json:"predecessor_task_ref,omitempty"`
	ExpectedGoalRevision   *uint64          `json:"expected_goal_revision,omitempty"`
}
type InputInput struct {
	SessionRef             api.ObjectRef    `json:"session_ref"`
	BranchRef              api.ObjectRef    `json:"branch_ref"`
	ExpectedBranchRevision uint64           `json:"expected_branch_revision"`
	RequestRef             api.ObjectRef    `json:"request_ref"`
	AnswerRef              api.ContentRef   `json:"answer_ref"`
	PreviewRefs            []api.ContentRef `json:"preview_refs"`
}
type SubmissionOutput struct {
	SubmissionRef       api.ObjectRef `json:"submission_ref"`
	State               string        `json:"state"`
	WithdrawalRequested bool          `json:"withdrawal_requested"`
	QueryMethod         string        `json:"query_method"`
}
type SubmissionView struct {
	Submission     api.Submission   `json:"submission"`
	Command        *api.Command     `json:"command,omitempty"`
	Receipt        *api.Receipt     `json:"receipt,omitempty"`
	AttachmentRefs []api.ContentRef `json:"attachment_refs"`
	QueueDeadline  string           `json:"queue_deadline"`
	Error          *api.Error       `json:"error,omitempty"`
}
type submissionRecord struct {
	SubmissionView
	Auth             runtime.Auth    `json:"auth"`
	Goal             *GoalInput      `json:"goal,omitempty"`
	Input            *InputInput     `json:"input,omitempty"`
	InputView        *RequestView    `json:"input_view,omitempty"`
	ClosureRef       *api.ContentRef `json:"closure_ref,omitempty"`
	GoalQueue        bool            `json:"goal_queue"`
	OrderKey         string          `json:"order_key,omitempty"`
	CreatedCommandID string          `json:"created_command_id"`
}

// EventBinding 由受信配置登记；调用方不能提供 method、URL 或目标。
type EventRule struct {
	Name             string          `json:"name"`
	Schema           json.RawMessage `json:"schema"`
	OwnerID          string          `json:"owner_id"`
	Method           string          `json:"method"`
	TargetID         string          `json:"target_id"`
	AcceptForSeconds uint64          `json:"accept_for_seconds"`
	ExpectedRevision *uint64         `json:"expected_revision,omitempty"`
	RequiresRendered bool            `json:"requires_rendered"`
}
type EventBinding struct {
	BindingRef api.ObjectRef `json:"binding_ref"`
	Events     []EventRule   `json:"events"`
}
