package extractionexecution

import (
	"context"
	"lerna/execution"
	"lerna/extraction"
	"lerna/memory"
)

type SavingDriver struct {
	candidate execution.Driver
	saver     *extraction.Saver
}

// WithMemory keeps extraction and its authorized automatic save within the
// same qualified Execution call. Unknown save state cannot complete that call.
func WithMemory(candidate execution.Driver, saver *extraction.Saver) (*SavingDriver, error) {
	if candidate == nil || saver == nil {
		return nil, memory.Invalid
	}
	return &SavingDriver{candidate, saver}, nil
}
func (d *SavingDriver) Start(ctx context.Context, c execution.Call) error {
	if err := d.candidate.Start(ctx, c); err != nil {
		return err
	}
	state, err := d.saver.Save(ctx, c.Request.OperationID)
	if err != nil {
		return err
	}
	if state.State != "committed" {
		return memory.Unavailable
	}
	return nil
}
func (d *SavingDriver) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
	// Validate original candidate invocation binding before looking up a save.
	observation, err := d.candidate.Inspect(ctx, c)
	if err != nil {
		return execution.Observation{}, err
	}
	state, err := d.saver.Inspect(ctx, c.Request.OperationID)
	if err == memory.Missing && observation.Phase == "NOT_STARTED" && observation.Result == "FAILURE" && observation.Effect == "NOT_OCCURRED" {
		return observation, nil
	}
	if err != nil && err != memory.Missing {
		return execution.Observation{}, err
	}
	if err != nil || state.State != "committed" {
		return execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}, nil
	}
	return observation, nil
}
