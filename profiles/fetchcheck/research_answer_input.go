package fetchcheck

import (
	"context"
	"lerna/brain"
	"lerna/tasks"
)

type researchTaskInputs interface {
	Validate(context.Context, tasks.Task, string) error
}

// Page evidence does not authorize the original goal or submitted sources.
// Keep both checks at model disclosure and publication boundaries.
type researchAnswerInput struct {
	evidence   brain.Context
	taskInputs researchTaskInputs
}

func (c researchAnswerInput) Assemble(ctx context.Context, t tasks.Task, location string, limit int) (brain.Input, error) {
	if err := c.taskInputs.Validate(ctx, t, location); err != nil {
		return brain.Input{}, err
	}
	return c.evidence.Assemble(ctx, t, location, limit)
}
func (c researchAnswerInput) Validate(ctx context.Context, t tasks.Task, location string) error {
	if err := c.taskInputs.Validate(ctx, t, location); err != nil {
		return err
	}
	return c.evidence.Validate(ctx, t, location)
}
