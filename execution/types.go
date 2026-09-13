// Package execution coordinates durable capability operations independently of
// drivers, transport and task decision strategy.
package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/schema"
	"lerna/tasks"
	"time"
)

type Capability struct {
	Async                                                                             *AsyncPolicy `json:",omitempty"`
	Name, Version, Implementation, ImplementationVersion, Resource, Purpose, Location string
	Input, Output                                                                     schema.Resource
	Exclusive, Synchronous                                                            bool
}

func (c Capability) Digest() string {
	b, _ := json.Marshal(c)
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:])
}

type Request struct {
	ControlVersion                                                               uint64 `json:",omitempty"`
	OperationID                                                                  string
	Qualification                                                                tasks.Qualification
	Capability, Version, Implementation, ImplementationVersion, DescriptorSHA256 string
	InputRef                                                                     string
	ResourceVersion                                                              uint64
}

func (r Request) Fingerprint() string {
	r.OperationID = ""
	b, _ := json.Marshal(r)
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:])
}

type Binding struct{ Token, Subject, Namespace, Audience, Presenter, CertificateSHA256, Worker string }

func (b Binding) Presentation(r Request) authorization.GrantPresentation {
	return authorization.GrantPresentation{Namespace: b.Namespace, Subject: b.Subject, Audience: b.Audience, Presenter: b.Presenter, CertificateSHA256: b.CertificateSHA256, OperationID: r.OperationID, SemanticSHA256: r.Fingerprint()}
}

type Receipt struct {
	OperationID string
	Revision    uint64
}
type Record struct {
	ConflictingEvidence              bool
	CancelID, TaskCancelOperation    string
	Async                            *AsyncState
	Request                          Request
	Binding                          Binding // Token is never persisted.
	Permit                           authorization.UsePermit
	Receipt                          Receipt
	OutputOperation                  string
	Revision                         uint64
	Started                          bool
	Phase, Result, Effect, Reference string
	Checks                           uint32
	Reports                          []tasks.ExecutionReport
	Applied                          uint64
}
type Call struct {
	Control *ControlQualification
	Request Request
	Input   []byte
}
type Observation struct {
	Phase, Result, Effect string
	Output, Evidence      []byte
}
type Driver interface {
	Start(context.Context, Call) error
	Inspect(context.Context, Call) (Observation, error)
}
type Content interface {
	Read(context.Context, string, string, Capability) ([]byte, error)
	Save(context.Context, string, string, string, Capability, []byte, []byte) (string, error)
}

// RequestContent optionally receives the original invocation binding for task
// accounting. The request is never replaced with a newly loaded worker scope;
// recovery retains the persisted request. SaveFor receives the save operation
// selected by the existing execution path without replacing its identity.
// Implementations must separately enforce current authority and any permitted
// recovery accounting. Receiving this request grants no additional rights.
type RequestContent interface {
	ReadFor(context.Context, string, Request, Capability) ([]byte, error)
	SaveFor(context.Context, string, Request, string, Capability, []byte, []byte) (string, error)
}
type Core interface {
	GuardExecution(authorization.RuntimeTransaction, tasks.Qualification, string, bool) error
	ConsumeExecution(context.Context, tasks.ExecutionReport) error
}
type Authority interface {
	LookupUse(context.Context, authorization.GrantPresentation) (authorization.UsePermit, error)
	UpdateExecution(context.Context, func(authorization.ExecutionTransaction) error) error
	ReserveUse(context.Context, string, authorization.GrantPresentation, *wire.AuthorizationAction, uint64) (authorization.UsePermit, error)
}
type Config struct {
	MaxOperations, MaxReports, MaxChecks, MaxOutbox, MaxInput, MaxOutput int
	IOTimeout, DriverTimeout                                             time.Duration
}

func (c Config) valid() bool {
	return c.MaxOperations > 0 && c.MaxOperations <= 64 && c.MaxReports > 0 && c.MaxReports <= 16 && c.MaxChecks > 0 && c.MaxChecks <= 16 && c.MaxOutbox >= c.MaxOperations && c.MaxOutbox <= 1024 && c.MaxInput > 0 && c.MaxInput <= 32768 && c.MaxOutput > 0 && c.MaxOutput <= 16384 && c.IOTimeout >= time.Millisecond && c.IOTimeout <= time.Second && c.DriverTimeout >= time.Millisecond && c.DriverTimeout <= 5*time.Second
}
func failure(code authorization.Code) error { return &authorization.Error{Code: code} }

func ValidRecord(r Record) bool {
	if r.Request.OperationID == "" || r.Request.Qualification.Ref.TaskID == "" || r.Request.Qualification.Version == 0 || r.Request.InputRef == "" || r.Revision < 1 || r.Revision > 32 || r.Checks > 16 || len(r.Reference) > 256 {
		return false
	}
	switch r.Phase {
	case "NOT_STARTED", "UNKNOWN", "FINISHED", "IN_PROGRESS":
	default:
		return false
	}
	switch r.Result {
	case "UNKNOWN", "SUCCESS", "FAILURE":
	default:
		return false
	}
	switch r.Effect {
	case "UNKNOWN", "CONFIRMED", "NOT_OCCURRED":
	default:
		return false
	}
	if r.Effect == "CONFIRMED" && (r.Phase != "FINISHED" || !r.Started) {
		return false
	}
	if r.Result == "SUCCESS" && (r.Effect != "CONFIRMED" || r.Reference == "") {
		return false
	}
	return true
}
