package answers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
)

// Recover never generates. It resolves the retained content operation and exact
// Core publication, subject to current task qualification and source policy.
// If no recoverable output exists it fails closed; ordinary worker recovery may
// later schedule a new decision using only the remaining generation budget.
func (p *Port) Recover(ctx context.Context, ref tasks.Ref) (tasks.RunSnapshot, error) {
	s, e := p.GenerationPort.PollControl(ctx, ref)
	if e != nil {
		return tasks.RunSnapshot{}, e
	}
	if s.Task.State == "COMPLETED" {
		return s, nil
	}
	if len(s.Generations) == 0 {
		return tasks.RunSnapshot{}, brain.Error("RESULT_UNRESOLVED")
	}
	g := s.Generations[len(s.Generations)-1]
	if g.Publication != nil {
		// Lookup precedes any replay; even if the content subsequently expires, the
		// historical commit remains a fact. Query checks current availability.
		if out, e := p.GenerationPort.Lookup(ctx, ref, g.Publication.ChangeID); e == nil {
			return out, nil
		}
		return p.Commit(ctx, *g.Publication)
	}
	if g.OutputOperation == "" {
		return tasks.RunSnapshot{}, brain.Error("RESULT_UNRESOLVED")
	}
	out, e := p.content.content.Call(ctx, p.content.binding, &wire.ContentRequest{Method: "LOOKUP", OperationId: g.OutputOperation, Purpose: p.content.purpose})
	if e != nil {
		return tasks.RunSnapshot{}, e
	}
	if out.GetRecord().GetState() != "available" {
		return tasks.RunSnapshot{}, brain.Error("INPUT_INVALIDATED")
	}
	// Only the trusted complete-answer writer owns this operation identity.
	// Validate the stored schema again before adopting an orphaned output.
	n := out.Record.Spec.Size
	if n == 0 || n > brain.MaxAnswerBytes {
		return tasks.RunSnapshot{}, brain.Error("OUTPUT_INVALID")
	}
	body, e := p.content.content.Call(ctx, p.content.binding, &wire.ContentRequest{Method: "READ", Ref: out.Record.Ref, Purpose: p.content.purpose, Limit: uint32(n)})
	if e != nil {
		return tasks.RunSnapshot{}, e
	}
	input, e := p.input.Assemble(ctx, s.Task, p.location, brain.MaxInputBytes)
	if e != nil {
		return tasks.RunSnapshot{}, e
	}
	validate := brain.ValidateAnswer
	if p.evidence {
		validate = brain.ValidateEvidenceAnswer
	}
	if e = validate(body.Data, input, brain.MaxAnswerBytes); e != nil {
		return tasks.RunSnapshot{}, e
	}
	sum := sha256.Sum256([]byte(g.OutputOperation))
	c := tasks.WorkChange{Kind: "complete", ChangeID: hex.EncodeToString(sum[:]), Qualification: g.Qualification, Finished: true, Proposal: tasks.Proposal{Kind: "answer", BaseVersion: g.Qualification.Version, Complete: true, Result: Reference(out.Record.Ref)}}
	return p.Commit(ctx, c)
}
