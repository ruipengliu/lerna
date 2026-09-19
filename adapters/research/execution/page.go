package execution

import (
	"context"
	"encoding/json"
	"lerna/execution"
	"lerna/fetch"
	"lerna/internal/jsonvalue"
	"strconv"
	"time"
)

type PageDriver struct {
	driver
	source   fetch.Fetcher
	acquirer *fetch.Acquirer
}

var _ execution.Driver = (*PageDriver)(nil)

func NewPage(f fetch.Fetcher, s fetch.OutcomeStore, e fetch.Evidence, o Operations, c Config) (*PageDriver, error) {
	if o == nil || !c.valid() {
		return nil, fetch.Invalid
	}
	a, err := fetch.NewAcquirer(f, s, e)
	if err != nil {
		return nil, err
	}
	return &PageDriver{driver: driver{store: s, operations: o, config: c}, source: f, acquirer: a}, nil
}
func (d *PageDriver) Start(ctx context.Context, c execution.Call) error {
	if err := d.identity(c); err != nil {
		return err
	}
	bound, err := d.scoped(c.Request)
	if err != nil {
		return err
	}
	d = bound
	if in, err := d.original(ctx, c); err == nil {
		_, _, err = d.acquirer.Recover(ctx, in)
		return err
	} else if err != fetch.Missing {
		return err
	}
	request, err := d.input(c.Input)
	if err != nil {
		return err
	}
	op, err := d.operations.NewOperation(ctx, d.config.Token)
	if err != nil {
		return fetch.Unavailable
	}
	in := fetch.AttemptIntent{Task: c.Request.Qualification.Ref, Subject: d.config.Subject, OperationID: c.Request.OperationID, Fingerprint: c.Request.Fingerprint(), MaxRequests: uint32(request.MaxRequests), TaskLimit: d.config.TaskLimit, EvidenceOperation: op}
	original := c.Request
	ctx = fetch.WithGuard(ctx, func(check context.Context) error { return d.config.Guard.Check(check, original) })
	_, _, err = d.acquirer.Acquire(ctx, in, request)
	return err
}
func (d *PageDriver) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
	unknown := execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}
	if err := d.identity(c); err != nil {
		return unknown, err
	}
	bound, err := d.scoped(c.Request)
	if err != nil {
		return unknown, err
	}
	return bound.driver.inspect(ctx, c, bound.acquirer.Recover)
}
func (d *PageDriver) input(raw []byte) (fetch.Request, error) {
	if len(raw) == 0 || len(raw) > 8192 {
		return fetch.Request{}, fetch.Invalid
	}
	value, err := jsonvalue.Decode(raw)
	object, ok := value.(map[string]any)
	if err != nil || !ok || len(object) != 4 {
		return fetch.Request{}, fetch.Invalid
	}
	url, ok := object["url"].(string)
	if !ok || url == "" || len(url) > 4096 {
		return fetch.Request{}, fetch.Invalid
	}
	number := func(key string) (int64, bool) {
		v, ok := object[key].(json.Number)
		if !ok {
			return 0, false
		}
		n, e := strconv.ParseInt(string(v), 10, 64)
		return n, e == nil
	}
	bytes, bOK := number("max_bytes")
	requests, rOK := number("max_requests")
	millis, tOK := number("timeout_ms")
	if !bOK || !rOK || !tOK || bytes < 1 || bytes > d.config.MaxBytes || requests < 1 || requests > int64(d.config.MaxRequests) || millis < 1 || millis > d.config.Timeout.Milliseconds() {
		return fetch.Request{}, fetch.Invalid
	}
	return fetch.Request{URL: url, MaxBytes: bytes, MaxRequests: int(requests), Timeout: time.Duration(millis) * time.Millisecond}, nil
}

func (d *PageDriver) scoped(request execution.Request) (*PageDriver, error) {
	if d.config.Observations == nil {
		return d, nil
	}
	store, evidence, err := d.config.Observations.Bind(request)
	if err != nil {
		return nil, err
	}
	acquirer, err := fetch.NewAcquirer(d.source, store, evidence)
	if err != nil {
		return nil, err
	}
	copy := *d
	copy.store, copy.acquirer = store, acquirer
	return &copy, nil
}

// WithObservations preserves the configured fetcher and network limits while
// binding the driver to a host-owned observation scope.
func (d *PageDriver) WithObservations(scope ObservationScope) (*PageDriver, error) {
	if scope == nil {
		return nil, fetch.Invalid
	}
	copy := *d
	copy.config.Observations = scope
	return &copy, nil
}

// WithRecovery binds the original transport to the current recovery authority
// and observation budget. It does not change the acquisition or network limits.
func (d *PageDriver) WithRecovery(scope ObservationScope, guard TaskGuard) (*PageDriver, error) {
	if guard == nil {
		return nil, fetch.Invalid
	}
	copy, err := d.WithObservations(scope)
	if err != nil {
		return nil, err
	}
	copy.config.Guard = guard
	return copy, nil
}
