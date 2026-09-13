// Package fetchcontext projects governed acquisition evidence into Brain input.
// It is a fact-content adapter for the existing taskcontext.Facts/Session host;
// that host still validates the current Core decision and context retention.
package fetchcontext

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"lerna/answers"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/fetch"
	"lerna/tasks"
	"time"
	"unicode/utf8"
)

type Evidence interface {
	Read(context.Context, string) (fetch.Result, error)
}
type FailureEvidence interface {
	ReadFailure(context.Context, string) (fetch.Outcome, error)
}
type Context struct {
	evidence          Evidence
	failures          FailureEvidence
	facts             [32]byte
	location, subject string
	refs              []string
	failureRefs       []string
}

var _ brain.Context = (*Context)(nil)

func fingerprint(t tasks.Task) [32]byte {
	raw, _ := json.Marshal(struct {
		Ref                                           tasks.Ref
		Subject, Resource, Goal, GoalRef, GuidanceRef string
		Constraints                                   tasks.Constraints
		Inputs                                        []string
		Facts                                         []tasks.InputFact
	}{t.Ref, t.Subject, t.Resource, t.Goal, t.GoalRef, t.GuidanceRef, t.Constraints, t.InputRefs, t.InputFacts})
	return sha256.Sum256(raw)
}

// New receives host-selected references, never links discovered in a webpage.
// References are fixed for this task's facts and do not grant new access.
func New(e Evidence, task tasks.Task, location string, refs []string) (*Context, error) {
	return NewWithFailures(e, nil, task, location, refs, nil)
}

// NewWithFailures adds host-selected, controlled failure artifacts. An absent
// or revoked artifact still fails closed; no read error is invented as a gap.
func NewWithFailures(e Evidence, failures FailureEvidence, task tasks.Task, location string, refs, failureRefs []string) (*Context, error) {
	if (len(refs) > 0 && e == nil) || (len(failureRefs) > 0 && failures == nil) || task.Ref.Namespace == "" || task.Ref.TaskID == "" || task.Subject == "" || len(location) == 0 || len(location) > 256 || len(refs)+len(failureRefs) == 0 || len(refs)+len(failureRefs) > 8 {
		return nil, contextassembly.Invalid
	}
	seen := map[string]bool{}
	all := append(append([]string(nil), refs...), failureRefs...)
	for _, ref := range all {
		id, err := answers.ParseReference(ref)
		if err != nil || id.Namespace != task.Ref.Namespace || seen[ref] {
			return nil, contextassembly.Invalid
		}
		seen[ref] = true
	}
	return &Context{evidence: e, failures: failures, facts: fingerprint(task), location: location, subject: task.Subject, refs: append([]string(nil), refs...), failureRefs: append([]string(nil), failureRefs...)}, nil
}
func (c *Context) check(t tasks.Task, location string) error {
	if location != c.location || fingerprint(t) != c.facts {
		return contextassembly.Invalidated
	}
	return nil
}
func (c *Context) Validate(ctx context.Context, t tasks.Task, location string) error {
	if err := c.check(t, location); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, ref := range c.refs {
		if _, err := c.evidence.Read(ctx, ref); err != nil {
			return err
		}
	}
	for _, ref := range c.failureRefs {
		if _, err := c.failure(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}
func (c *Context) failure(ctx context.Context, ref string) (fetch.Outcome, error) {
	out, err := c.failures.ReadFailure(ctx, ref)
	if err != nil {
		return fetch.Outcome{}, err
	}
	if !fetch.IsFailureStatus(out.Status) || out.Reference != "" || out.Requests > 5 || (out.Mode != "" && out.Mode != "http" && out.Mode != "fixed-replay") || (out.Mode == "fixed-replay" && out.Requests != 0) {
		return fetch.Outcome{}, contextassembly.Invalidated
	}
	return out, nil
}
func (c *Context) Assemble(ctx context.Context, t tasks.Task, location string, max int) (brain.Input, error) {
	if err := c.check(t, location); err != nil {
		return brain.Input{}, err
	}
	if max < 1 || max > brain.MaxInputBytes {
		return brain.Input{}, contextassembly.BudgetExceeded
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	constraints, _ := json.Marshal(t.Constraints)
	out := brain.Input{Goal: t.Goal, Constraints: string(constraints)}
	for _, ref := range c.refs {
		result, err := c.evidence.Read(ctx, ref)
		if err != nil {
			return brain.Input{}, err
		}
		if !utf8.Valid(result.Body) {
			return brain.Input{}, contextassembly.Invalidated
		}
		// HTTP adapters validate complete textual source bytes. This projection
		// retains their provenance without promoting embedded instructions to goals.
		raw, err := json.Marshal(struct {
			Status, Body, RequestedURL, FinalURL, MediaType, SHA256, Mode string
			FetchedAt                                                     time.Time
			Sources                                                       []string
		}{"acquired", string(result.Body), result.RequestedURL, result.FinalURL, result.MediaType, result.SHA256, result.Mode, result.FetchedAt, result.Sources})
		if err != nil {
			return brain.Input{}, contextassembly.Invalid
		}
		out.Blocks = append(out.Blocks, brain.Block{Ref: ref, Text: string(raw), Subject: c.subject, Role: "external-evidence"})
		encoded, err := json.Marshal(out)
		if err != nil || len(encoded) > max {
			return brain.Input{}, contextassembly.BudgetExceeded
		}
	}
	for _, ref := range c.failureRefs {
		failure, err := c.failure(ctx, ref)
		if err != nil {
			return brain.Input{}, err
		}
		raw, _ := json.Marshal(struct {
			Status, Mode string
			Requests     uint32
		}{failure.Status, failure.Mode, failure.Requests})
		out.Blocks = append(out.Blocks, brain.Block{Ref: ref, Text: string(raw), Subject: c.subject, Role: "external-evidence-gap"})
	}
	encoded, err := json.Marshal(out)
	if err != nil || len(encoded) > max {
		return brain.Input{}, contextassembly.BudgetExceeded
	}
	// A sole source was just read under current authority; only local encoding
	// follows. Multiple sources still need the final combined validation.
	if len(c.refs)+len(c.failureRefs) > 1 {
		if err := c.Validate(ctx, t, location); err != nil {
			return brain.Input{}, err
		}
	}
	return out, nil
}
