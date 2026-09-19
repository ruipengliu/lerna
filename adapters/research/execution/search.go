package execution

import (
	"context"
	"encoding/json"
	"lerna/execution"
	"lerna/fetch"
	"lerna/internal/jsonvalue"
	"lerna/websearch"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type QueryGuard interface {
	Check(context.Context, execution.Call) error
}

type SearchConfig struct {
	Config
	QueryGuard QueryGuard
	MaxResults int
}
type SearchDriver struct {
	driver
	source     websearch.Searcher
	acquirer   *websearch.Acquirer
	queryGuard QueryGuard
	maxResults int
}

var _ execution.Driver = (*SearchDriver)(nil)

func NewSearch(f websearch.Searcher, s fetch.OutcomeStore, e fetch.Evidence, o Operations, c SearchConfig) (*SearchDriver, error) {
	if o == nil || !c.Config.valid() || c.QueryGuard == nil || c.MaxResults < 1 || c.MaxResults > 16 {
		return nil, fetch.Invalid
	}
	a, err := websearch.NewAcquirer(f, s, e)
	if err != nil {
		return nil, err
	}
	return &SearchDriver{driver: driver{store: s, operations: o, config: c.Config}, source: f, acquirer: a, queryGuard: c.QueryGuard, maxResults: c.MaxResults}, nil
}
func (d *SearchDriver) Start(ctx context.Context, c execution.Call) error {
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
	ctx = fetch.WithGuard(ctx, func(check context.Context) error {
		if err := d.config.Guard.Check(check, original); err != nil {
			return err
		}
		return d.queryGuard.Check(check, c)
	})
	_, _, err = d.acquirer.Acquire(ctx, in, request)
	return err
}
func (d *SearchDriver) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
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
func (d *SearchDriver) input(raw []byte) (websearch.Request, error) {
	if len(raw) == 0 || len(raw) > 8192 {
		return websearch.Request{}, fetch.Invalid
	}
	value, err := jsonvalue.Decode(raw)
	object, ok := value.(map[string]any)
	if err != nil || !ok || len(object) != 5 {
		return websearch.Request{}, fetch.Invalid
	}
	query, ok := object["query"].(string)
	if !ok || strings.TrimSpace(query) == "" || len(query) > 2048 || !utf8.ValidString(query) {
		return websearch.Request{}, fetch.Invalid
	}
	number := func(key string) (int64, bool) {
		v, ok := object[key].(json.Number)
		if !ok {
			return 0, false
		}
		n, e := strconv.ParseInt(string(v), 10, 64)
		return n, e == nil
	}
	results, resultOK := number("max_results")
	bytes, bOK := number("max_bytes")
	requests, rOK := number("max_requests")
	millis, tOK := number("timeout_ms")
	if !resultOK || results < 1 || results > int64(d.maxResults) || !bOK || !rOK || !tOK || bytes < 1 || bytes > d.config.MaxBytes || requests < 1 || requests > int64(d.config.MaxRequests) || millis < 1 || millis > d.config.Timeout.Milliseconds() {
		return websearch.Request{}, fetch.Invalid
	}
	return websearch.Request{Query: query, MaxResults: int(results), MaxBytes: bytes, MaxRequests: int(requests), Timeout: time.Duration(millis) * time.Millisecond}, nil
}

func (d *SearchDriver) scoped(request execution.Request) (*SearchDriver, error) {
	if d.config.Observations == nil {
		return d, nil
	}
	store, evidence, err := d.config.Observations.Bind(request)
	if err != nil {
		return nil, err
	}
	acquirer, err := websearch.NewAcquirer(d.source, store, evidence)
	if err != nil {
		return nil, err
	}
	copy := *d
	copy.store, copy.acquirer = store, acquirer
	return &copy, nil
}
