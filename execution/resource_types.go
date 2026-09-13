package execution

import (
	"context"
	"time"
)

// ResourceRef identifies a control range, independently of Task ownership.
type ResourceRef struct{ Namespace, Kind, Key string }
type ResourceScope struct {
	Ref                                                 ResourceRef
	Aliases                                             []ResourceRef
	Authority, AuthorizationResource, Purpose, Location string
	Participants                                        []string
	MaxOperations, MaxChecks                            int
	PollInterval, Lease, Window                         time.Duration
}
type ControlQualification struct {
	Resource  ResourceRef
	Authority string
	Version   uint64
}
type ResourceObservation struct {
	Resource                        ResourceRef
	Authority                       string
	ControlVersion, ResourceVersion uint64
	Intent                          string
	Quiescent                       bool
}
type ResourceCommand struct {
	OperationID             string
	Resource                ResourceRef
	Authority               string
	Version                 uint64
	Intent                  string
	ExpectedResourceVersion uint64
}

// ResourceDriver must fence old requests at the actual target. Observe is pure.
// Applying the same command has stable identity; stale versions cannot change it.
type ResourceDriver interface {
	ObserveResource(context.Context, ResourceRef) (ResourceObservation, error)
	ApplyResourceControl(context.Context, ResourceCommand) (ResourceObservation, error)
}
type ResourceControlRequest struct {
	OperationID     string
	Resource        ResourceRef
	Intent          string
	ExpectedVersion uint64
}
type ResourceControlReceipt struct {
	OperationID string
	Resource    ResourceRef
	Version     uint64
}
type ResourceControl struct {
	Resource         ResourceRef
	Version          uint64
	Intent, Progress string
	Limitation       string
	ResourceVersion  uint64
	Blocking         int
	Checks           uint32
}
type controlBinding struct {
	scope  ResourceScope
	driver ResourceDriver
}
type controlState struct {
	Scope                   ResourceScope
	View                    ResourceControl
	OperationID             string
	Observation             ResourceObservation
	Sent                    bool
	LeaseUntil, Next, Until int64
	Generation              uint64
}
type controlOperation struct {
	Request ResourceControlRequest
	Subject string
	Receipt ResourceControlReceipt
}
type resourceInvocation struct {
	Descriptor, OperationID     string
	Resource                    ResourceRef
	Started, Resolved, Conflict bool
}
type resourceJournal struct {
	Scopes       map[string]controlState
	Aliases      map[string]string
	Participants map[string]string
	Operations   map[string]controlOperation
	Invocations  map[string]resourceInvocation
	CancelOwners map[string]string
	Cursors      map[string]string
}
