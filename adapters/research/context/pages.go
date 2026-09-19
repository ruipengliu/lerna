package context

import (
	"context"
	"encoding/json"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/fetch"
	"lerna/tasks"
	"time"
	"unicode/utf8"
)

type PageEvidence interface {
	Read(context.Context, string) (fetch.Result, error)
}
type FailureEvidence interface {
	ReadFailure(context.Context, string) (fetch.Outcome, error)
}
type PageContext struct {
	boundContext
	evidence    PageEvidence
	failures    FailureEvidence
	refs        []string
	failureRefs []string
}

var _ brain.Context = (*PageContext)(nil)

// NewPages receives host-selected references, never links discovered in a webpage.
// References are fixed for this task's facts and do not grant new access.
func NewPages(e PageEvidence, task tasks.Task, location string, refs []string) (*PageContext, error) {
	return NewPagesWithFailures(e, nil, task, location, refs, nil)
}

// NewPagesWithFailures adds host-selected, controlled failure artifacts. An absent
// or revoked artifact still fails closed; no read error is invented as a gap.
func NewPagesWithFailures(e PageEvidence, failures FailureEvidence, task tasks.Task, location string, refs, failureRefs []string) (*PageContext, error) {
	if (len(refs) > 0 && e == nil) || (len(failureRefs) > 0 && failures == nil) {
		return nil, contextassembly.Invalid
	}
	bound, err := bindContext(task, location, append(append([]string(nil), refs...), failureRefs...))
	if err != nil {
		return nil, err
	}
	return &PageContext{boundContext: bound, evidence: e, failures: failures, refs: append([]string(nil), refs...), failureRefs: append([]string(nil), failureRefs...)}, nil
}
func (c *PageContext) Validate(ctx context.Context, t tasks.Task, location string) error {
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
func (c *PageContext) failure(ctx context.Context, ref string) (fetch.Outcome, error) {
	out, err := c.failures.ReadFailure(ctx, ref)
	if err != nil {
		return fetch.Outcome{}, err
	}
	if !fetch.IsFailureStatus(out.Status) || out.Reference != "" || out.Requests > 5 || (out.Mode != "" && out.Mode != "http" && out.Mode != "fixed-replay") || (out.Mode == "fixed-replay" && out.Requests != 0) {
		return fetch.Outcome{}, contextassembly.Invalidated
	}
	return out, nil
}
func (c *PageContext) Assemble(ctx context.Context, t tasks.Task, location string, max int) (brain.Input, error) {
	if err := c.check(t, location); err != nil {
		return brain.Input{}, err
	}
	if max < 1 || max > brain.MaxInputBytes {
		return brain.Input{}, contextassembly.BudgetExceeded
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := taskInput(t)
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
		if err := checkSize(out, max); err != nil {
			return brain.Input{}, err
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
	return finishAssembly(ctx, c, t, location, out, max, len(c.refs)+len(c.failureRefs))
}
