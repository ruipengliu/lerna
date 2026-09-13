// Package credentialexecution joins an admitted Execution call to a bound
// credential exit and a separate trusted effect observer.
package credentialexecution

import (
	"context"
	"encoding/json"
	"lerna/credentials"
	"lerna/execution"
)

type State struct {
	Applied        bool
	Value, Version uint64
}
type Observer interface {
	Target() credentials.Binding
	Observe(context.Context, string) (State, error)
}
type Driver struct {
	use                    *credentials.Driver
	observer               Observer
	token, ref, capability string
}

func New(use *credentials.Driver, observer Observer, token, ref, capability string) (*Driver, error) {
	if use == nil || observer == nil || token == "" || ref == "" || capability == "" {
		return nil, credentials.Invalid
	}
	if observer.Target() != use.Target() {
		return nil, credentials.Denied
	}
	return &Driver{use, observer, token, ref, capability}, nil
}
func (d *Driver) Start(ctx context.Context, c execution.Call) error {
	if c.Request.Capability != d.capability || c.Request.Qualification.Ref.Namespace != d.use.Target().Namespace {
		return credentials.Denied
	}
	_, e := d.use.Use(ctx, d.token, d.ref, credentials.Call{OperationID: c.Request.OperationID, Payload: c.Input})
	return e
}
func (d *Driver) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
	if c.Request.Capability != d.capability || c.Request.Qualification.Ref.Namespace != d.use.Target().Namespace {
		return execution.Observation{}, credentials.Denied
	}
	s, e := d.observer.Observe(ctx, c.Request.OperationID)
	if e != nil {
		return execution.Observation{}, credentials.TargetUnavailable
	}
	if !s.Applied {
		return execution.Observation{Phase: "FINISHED", Result: "FAILURE", Effect: "NOT_OCCURRED"}, nil
	}
	out, _ := json.Marshal(struct {
		Value   uint64 `json:"value"`
		Version uint64 `json:"version"`
	}{s.Value, s.Version})
	evidence, _ := json.Marshal(struct {
		Applied bool `json:"applied"`
	}{true})
	return execution.Observation{Phase: "FINISHED", Result: "SUCCESS", Effect: "CONFIRMED", Output: out, Evidence: evidence}, nil
}
