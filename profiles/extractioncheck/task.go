package extractioncheck

import (
	"context"
	"fmt"
	"lerna/adapters/executionlocal"
	"lerna/execution"
	"lerna/extraction"
	"lerna/sdk"
)

// CheckTask exercises actual Core qualification, signed grants, Capability SDK,
// controlled input/output and the real file-to-candidate execution driver.
func CheckTask(ctx context.Context) error {
	h, err := fresh(ctx)
	if err != nil {
		return err
	}
	defer h.destroy()
	request, material, err := h.request(ctx)
	if err != nil {
		return err
	}
	receipt, err := h.client.Invoke(ctx, request, material)
	if err != nil {
		return err
	}
	outcome, err := h.exec.Run(ctx, request.OperationID)
	if err != nil {
		return err
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		return err
	}
	task, err := h.core.Get(ctx, h.token, request.Qualification.Ref)
	if err != nil {
		return err
	}
	if outcome.Effect != "CONFIRMED" || outcome.Result != "SUCCESS" || outcome.Reference == "" || task.State != "COMPLETED" {
		return fmt.Errorf("extraction task state=%s effect=%s result=%s", task.State, outcome.Effect, outcome.Result)
	}
	candidate, err := h.candidates.Lookup(ctx, "local", request.OperationID)
	if err != nil {
		return err
	}
	if candidate.Candidate.Kind != "preference" || candidate.Candidate.Value != "concise" || candidate.InvocationSHA256 != request.Fingerprint() {
		return fmt.Errorf("wrong extracted candidate")
	}
	replay, err := h.client.Invoke(ctx, request, "")
	if err != nil {
		return err
	}
	if replay != receipt {
		return fmt.Errorf("changed original invocation receipt")
	}
	if _, err = h.exec.Run(ctx, request.OperationID); err != nil {
		return err
	}
	current, err := h.core.Get(ctx, h.token, request.Qualification.Ref)
	if err != nil {
		return err
	}
	if current.Version != task.Version {
		return fmt.Errorf("replay changed completed task")
	}
	return nil
}

// CheckRetiredTask reconciles an actual candidate effect after its body was
// retired before the execution coordinator could publish a result.
func CheckRetiredTask(ctx context.Context) error {
	h, err := fresh(ctx)
	if err != nil {
		return err
	}
	defer h.destroy()
	driver := retiringDriver{Driver: h.target, store: h.candidates}
	h.exec, err = execution.New(h.grants, h.work, h.access, driver, h.binding, h.cap, config(), h.operation)
	if err != nil {
		return err
	}
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	request, material, err := h.request(ctx)
	if err != nil {
		return err
	}
	if _, err = h.client.Invoke(ctx, request, material); err != nil {
		return err
	}
	outcome, err := h.exec.Run(ctx, request.OperationID)
	if err != nil {
		return err
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		return err
	}
	task, err := h.core.Get(ctx, h.token, request.Qualification.Ref)
	if err != nil {
		return err
	}
	if outcome.Effect != "CONFIRMED" || outcome.Result != "FAILURE" || outcome.Reference != "" || task.State == "COMPLETED" {
		return fmt.Errorf("retired task state=%s effect=%s result=%s ref=%s", task.State, outcome.Effect, outcome.Result, outcome.Reference)
	}
	return nil
}

type retiringDriver struct {
	execution.Driver
	store extraction.CandidateLifecycle
}

func (d retiringDriver) Start(ctx context.Context, c execution.Call) error {
	if err := d.Driver.Start(ctx, c); err != nil {
		return err
	}
	return d.store.Retire(ctx, c.Request.Qualification.Ref.Namespace, "operator", c.Request.OperationID)
}
