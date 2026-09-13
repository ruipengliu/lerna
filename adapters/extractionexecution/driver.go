// Package extractionexecution connects local extraction to Execution's trusted
// driver seam. Only Execution may call it after checking Core qualification.
package extractionexecution

import (
	"context"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/proto"
	"lerna/execution"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"time"
)

type Authority interface {
	Check(context.Context, memory.Binding, string) error
}

// Sources must check source-specific current read policy before releasing any
// material and return exact, authenticated provenance and current restrictions.
// Validate checks the same revisions and all proposed restrictions again; it
// must reject source correction/deletion/tightening, not silently substitute.
type Sources interface {
	Read(context.Context, []*wire.ContentSource, string, string) ([]extraction.Material, []extraction.Restrictions, error)
	Validate(context.Context, []*wire.ContentSource, extraction.Restrictions) error
}
type Outcomes interface {
	extraction.CandidateLifecycle
	extraction.DeclineStore
}
type Driver struct {
	store       Outcomes
	auth        Authority
	sources     Sources
	rules       extraction.Extractor
	clock       memory.Clock
	binding     memory.Binding
	purpose     string
	retainUntil int64
}

var _ execution.Driver = (*Driver)(nil)

func New(store Outcomes, auth Authority, sources Sources, rules extraction.Extractor, clock memory.Clock, b memory.Binding, purpose string) (*Driver, error) {
	if store == nil || auth == nil || sources == nil || rules == nil || clock == nil || b.Token == "" || b.Namespace == "" || b.Subject == "" || b.Location == "" || b.Recipient != b.Location || purpose == "" {
		return nil, memory.Invalid
	}
	return &Driver{store: store, auth: auth, sources: sources, rules: rules, clock: clock, binding: b, purpose: purpose}, nil
}
func (d *Driver) identity(c execution.Call) error {
	if c.Request.OperationID == "" || c.Request.Qualification.Ref.Namespace != d.binding.Namespace || c.Request.Qualification.Ref.TaskID == "" || c.Request.InputRef == "" {
		return memory.Invalid
	}
	return nil
}
func allowed(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func (d *Driver) Start(ctx context.Context, c execution.Call) error {
	if err := d.identity(c); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	old, err := d.store.Inspect(ctx, d.binding.Namespace, c.Request.OperationID)
	if err == nil {
		if old.Subject != d.binding.Subject || old.InvocationSHA256 != c.Request.Fingerprint() {
			return memory.IdentityConflict
		}
		if old.Committed || old.State == "unsupported" {
			return nil
		}
		return memory.ReplayUnavailable
	}
	if !errors.Is(err, memory.Missing) {
		return err
	}
	if d.retainUntil != 0 {
		now, err := d.clock.Now()
		if err != nil {
			return memory.Unavailable
		}
		if now.Unix() >= d.retainUntil {
			return memory.Denied
		}
	}
	refs, err := sourceInput(c.Input)
	if err != nil {
		return err
	}
	if err = d.auth.Check(ctx, d.binding, "extract"); err != nil {
		return err
	}
	readRefs := make([]*wire.ContentSource, len(refs))
	for i, r := range refs {
		readRefs[i] = proto.Clone(r).(*wire.ContentSource)
	}
	materials, restrictions, err := d.sources.Read(ctx, readRefs, d.binding.Location, d.purpose)
	if err != nil {
		return err
	}
	if len(materials) != len(refs) || len(restrictions) != len(materials) {
		return memory.Denied
	}
	for i, m := range materials {
		if m.Source == nil || !proto.Equal(m.Source.Ref, refs[i]) {
			return memory.Denied
		}
	}
	now, err := d.clock.Now()
	if err != nil {
		return memory.Unavailable
	}
	bounds, err := extraction.IntersectRestrictions(now.Unix(), restrictions)
	if err != nil {
		return err
	}
	if d.retainUntil != 0 && d.retainUntil < bounds.RetainUntil {
		bounds.RetainUntil = d.retainUntil
	}
	if bounds.RetainUntil <= now.Unix() {
		return memory.Denied
	}
	if !allowed(bounds.Processing, d.binding.Location) || !allowed(bounds.Purposes, d.purpose) {
		return memory.Denied
	}
	if err = d.auth.Check(ctx, d.binding, "extract"); err != nil {
		return err
	}
	expectedSources := make([]*wire.MemorySource, len(materials))
	for i, m := range materials {
		expectedSources[i] = proto.Clone(m.Source).(*wire.MemorySource)
	}
	result, err := d.rules.Extract(ctx, extraction.Input{About: d.binding.Subject, Materials: materials})
	if err != nil {
		return err
	}
	if len(result.Candidates) != 1 {
		if len(result.Candidates) != 0 {
			return memory.Unavailable
		}
		if err = d.sources.Validate(ctx, refs, bounds); err != nil {
			return err
		}
		if err = d.auth.Check(ctx, d.binding, "extract"); err != nil {
			return err
		}
		return d.store.Decline(ctx, extraction.CandidateState{Namespace: d.binding.Namespace, Subject: d.binding.Subject, OperationID: c.Request.OperationID, InvocationSHA256: c.Request.Fingerprint(), State: "unsupported", Reason: result.Reason})
	}
	candidate := result.Candidates[0]
	if len(candidate.Sources) != len(materials) || candidate.About != d.binding.Subject {
		return memory.Denied
	}
	for i, source := range candidate.Sources {
		if !proto.Equal(source, expectedSources[i]) {
			return memory.Denied
		}
	}
	if !allowed(bounds.Storage, d.binding.Location) {
		return memory.Denied
	}
	if err = d.sources.Validate(ctx, refs, bounds); err != nil {
		return err
	}
	if err = d.auth.Check(ctx, d.binding, "retain"); err != nil {
		return err
	}
	if err = d.auth.Check(ctx, d.binding, "extract"); err != nil {
		return err
	}
	now, err = d.clock.Now()
	if err != nil {
		return memory.Unavailable
	}
	if now.Unix() >= bounds.RetainUntil {
		return memory.Denied
	}
	return d.store.Commit(ctx, extraction.CandidateRecord{Namespace: d.binding.Namespace, Subject: d.binding.Subject, OperationID: c.Request.OperationID, InvocationSHA256: c.Request.Fingerprint(), Candidate: candidate, Restrictions: bounds})
}
func (d *Driver) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
	if err := d.identity(c); err != nil {
		return execution.Observation{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	state, err := d.store.Inspect(ctx, d.binding.Namespace, c.Request.OperationID)
	if errors.Is(err, memory.Missing) {
		return execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}, nil
	}
	if err != nil {
		return execution.Observation{}, err
	}
	if state.Subject != d.binding.Subject || state.InvocationSHA256 != c.Request.Fingerprint() {
		return execution.Observation{}, memory.IdentityConflict
	}
	if !state.Committed {
		if state.State == "unsupported" {
			out := execution.Observation{Phase: "NOT_STARTED", Result: "FAILURE", Effect: "NOT_OCCURRED"}
			out.Evidence, _ = json.Marshal(struct {
				OperationID, Fingerprint string
				Committed                bool
			}{c.Request.OperationID, state.InvocationSHA256, false})
			if d.auth.Check(ctx, d.binding, "disclose") == nil {
				out.Evidence, _ = json.Marshal(struct {
					OperationID, Fingerprint, State, Reason string
					Committed                               bool
				}{c.Request.OperationID, state.InvocationSHA256, state.State, state.Reason, false})
				out.Output, _ = json.Marshal(struct{ Namespace, OperationID string }{d.binding.Namespace, c.Request.OperationID})
			}
			return out, nil
		}
		return execution.Observation{Phase: "NOT_STARTED", Result: "FAILURE", Effect: "NOT_OCCURRED"}, nil
	}
	out := execution.Observation{Phase: "FINISHED", Result: "FAILURE", Effect: "CONFIRMED"}
	out.Evidence, _ = json.Marshal(struct {
		OperationID, Fingerprint string
		Committed                bool
	}{c.Request.OperationID, state.InvocationSHA256, true})
	// Effect reconciliation does not depend on retaining the old body. Only
	// disclosure of its controlled reference needs current source permissions.
	if state.State != "retained" {
		return out, nil
	}
	r, err := d.store.Lookup(ctx, d.binding.Namespace, c.Request.OperationID)
	if err != nil {
		return out, nil
	}
	now, err := d.clock.Now()
	if err != nil {
		return out, nil
	}
	if now.Unix() >= r.Restrictions.RetainUntil || !allowed(r.Restrictions.Recipients, d.binding.Recipient) {
		return out, nil
	}
	refs := make([]*wire.ContentSource, 0, len(r.Candidate.Sources))
	for _, s := range r.Candidate.Sources {
		refs = append(refs, s.Ref)
	}
	if d.sources.Validate(ctx, refs, r.Restrictions) != nil || d.auth.Check(ctx, d.binding, "disclose") != nil {
		return out, nil
	}
	latest, err := d.store.Inspect(ctx, d.binding.Namespace, c.Request.OperationID)
	if err != nil || latest.State != "retained" {
		return out, nil
	}
	out.Output, _ = json.Marshal(struct{ Namespace, OperationID string }{d.binding.Namespace, c.Request.OperationID})
	out.Result = "SUCCESS"
	return out, nil
}
