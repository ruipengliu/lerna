package answers

import (
	"context"
	"lerna/authorization"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
)

// Port guards result content while the embedded GenerationPort retains Core's
// state and control checks. Sources remain subject to finite local freshness.
type Port struct {
	*tasks.GenerationPort
	content  *ContentAccess
	location string
	input    brain.Context
	evidence bool
}

func BindPort(p *tasks.GenerationPort, c *ContentAccess, location string) *Port {
	return &Port{GenerationPort: p, content: c, location: location, input: c}
}

// BindContextPort uses the same fixed context at publication and recovery that
// the personalized Brain used. Existing content-only bindings remain supported.
func BindContextPort(p *tasks.GenerationPort, c *ContentAccess, location string, input brain.Context) (*Port, error) {
	if p == nil || c == nil || location == "" || input == nil {
		return nil, brain.Error("INVALID_ARGUMENT")
	}
	out := BindPort(p, c, location)
	out.input = input
	return out, nil
}
func (p *Port) Commit(ctx context.Context, c tasks.WorkChange) (tasks.RunSnapshot, error) {
	if c.Kind == "complete" {
		if _, e := p.GenerationPort.Lookup(ctx, c.Qualification.Ref, c.ChangeID); e == nil {
			return p.GenerationPort.Commit(ctx, c)
		} else if !authorization.Is(e, authorization.NotFound) {
			return tasks.RunSnapshot{}, e
		}
		if c.Proposal.Kind != "answer" {
			return tasks.RunSnapshot{}, brain.Error("OUTPUT_INVALID")
		}
		snap, err := p.GenerationPort.Current(ctx, c.Qualification.Ref)
		if err != nil {
			return tasks.RunSnapshot{}, err
		}
		operation := ""
		for _, g := range snap.Generations {
			if g.Qualification == c.Qualification {
				operation = g.OutputOperation
			}
		}
		if operation == "" {
			return tasks.RunSnapshot{}, brain.Error("OUTPUT_INVALID")
		}
		if err = p.content.Validate(ctx, snap.Task, p.location); err != nil {
			return tasks.RunSnapshot{}, err
		}
		if err = p.input.Validate(ctx, snap.Task, p.location); err != nil {
			return tasks.RunSnapshot{}, err
		}
		// Check the original output identity and its current availability together,
		// after input/source validation. A preceding LOOKUP plus a later GET of
		// that same record would duplicate the final Content observation.
		out, err := p.content.content.Call(ctx, p.content.binding, &wire.ContentRequest{Method: "LOOKUP", OperationId: operation, Purpose: p.content.purpose})
		if err != nil {
			return tasks.RunSnapshot{}, err
		}
		if out.GetRecord().GetState() != "available" || Reference(out.Record.Ref) != c.Proposal.Result {
			return tasks.RunSnapshot{}, brain.Error("OUTPUT_INVALID")
		}
		if err = p.PreparePublication(ctx, c); err != nil {
			return tasks.RunSnapshot{}, err
		}
	}
	return p.GenerationPort.Commit(ctx, c)
}

// BindEvidencePort fixes the recovery schema to the writer's evidence contract.
// The stored model output cannot select or downgrade its own validator.
func BindEvidencePort(p *tasks.GenerationPort, c *ContentAccess, location string, input brain.Context) (*Port, error) {
	out, err := BindContextPort(p, c, location, input)
	if err != nil {
		return nil, err
	}
	out.evidence = true
	return out, nil
}
