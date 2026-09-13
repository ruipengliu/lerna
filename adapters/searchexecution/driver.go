// Package searchexecution connects an admitted Execution call to acquisition.
// Only Execution may invoke this trusted seam after Core qualification checks.
package searchexecution

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

type Operations interface {
	NewOperation(context.Context, string) (string, error)
}
type TaskGuard interface {
	Check(context.Context, execution.Request) error
}
type QueryGuard interface {
	Check(context.Context, execution.Call) error
}

// ObservationScope binds business reads to the original admitted request.
// Binding supplies no authority; each observation must check the current task
// and original action before I/O, including recovery and failed observations.
type ObservationScope interface {
	Bind(execution.Request) (fetch.OutcomeStore, fetch.Evidence, error)
}

type Config struct {
	Observations ObservationScope
	QueryGuard   QueryGuard
	MaxResults   int
	// Guard is required: every execution fetch checks current task qualification.
	Guard                     TaskGuard
	Token, Namespace, Subject string
	Capability                execution.Capability
	MaxBytes                  int64
	MaxRequests, TaskLimit    uint32
	Timeout                   time.Duration
}
type Driver struct {
	source     websearch.Searcher
	acquirer   *websearch.Acquirer
	store      fetch.OutcomeStore
	operations Operations
	config     Config
}

var _ execution.Driver = (*Driver)(nil)

func New(f websearch.Searcher, s fetch.OutcomeStore, e fetch.Evidence, o Operations, c Config) (*Driver, error) {
	if c.QueryGuard == nil || c.MaxResults < 1 || c.MaxResults > 16 || o == nil || c.Guard == nil || c.Token == "" || c.Namespace == "" || c.Subject == "" || c.Capability.Name == "" || c.Capability.Version == "" || c.Capability.Implementation == "" || c.Capability.ImplementationVersion == "" || c.MaxBytes < 1 || c.MaxBytes > 1<<20 || c.MaxRequests < 1 || c.MaxRequests > 5 || c.TaskLimit < c.MaxRequests || c.TaskLimit > 128 || c.Timeout < time.Millisecond || c.Timeout > 5*time.Second {
		return nil, fetch.Invalid
	}
	a, err := websearch.NewAcquirer(f, s, e)
	if err != nil {
		return nil, err
	}
	return &Driver{source: f, acquirer: a, store: s, operations: o, config: c}, nil
}
func (d *Driver) identity(c execution.Call) error {
	r := c.Request
	cap := d.config.Capability
	if r.OperationID == "" || r.InputRef == "" || r.Qualification.Ref.Namespace != d.config.Namespace || r.Qualification.Ref.TaskID == "" || r.Qualification.Version == 0 || r.Capability != cap.Name || r.Version != cap.Version || r.Implementation != cap.Implementation || r.ImplementationVersion != cap.ImplementationVersion || r.DescriptorSHA256 != cap.Digest() {
		return fetch.Denied
	}
	return nil
}
func (d *Driver) original(ctx context.Context, c execution.Call) (fetch.AttemptIntent, error) {
	in, err := d.store.Inspect(ctx, d.config.Namespace, c.Request.OperationID)
	if err != nil {
		return in, err
	}
	if in.Subject != d.config.Subject || in.Task != c.Request.Qualification.Ref || in.Fingerprint != c.Request.Fingerprint() {
		return fetch.AttemptIntent{}, fetch.IdentityConflict
	}
	return in, nil
}
func (d *Driver) Start(ctx context.Context, c execution.Call) error {
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
		return d.config.QueryGuard.Check(check, c)
	})
	_, _, err = d.acquirer.Acquire(ctx, in, request)
	return err
}
func (d *Driver) Inspect(ctx context.Context, c execution.Call) (execution.Observation, error) {
	unknown := execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}
	if err := d.identity(c); err != nil {
		return unknown, err
	}
	bound, err := d.scoped(c.Request)
	if err != nil {
		return unknown, err
	}
	d = bound
	in, err := d.original(ctx, c)
	if err == fetch.Missing {
		return unknown, nil
	}
	if err != nil {
		return unknown, err
	}
	result, known, err := d.acquirer.Recover(ctx, in)
	if fact, ok := err.(*fetch.ControlledAcquisition); ok {
		evidence, _ := json.Marshal(struct {
			Status   string `json:"status"`
			Mode     string `json:"mode,omitempty"`
			Requests uint32 `json:"requests"`
			Delivery string `json:"delivery"`
		}{"acquired", fact.Mode, fact.Requests, "withheld"})
		return execution.Observation{Phase: "FINISHED", Result: "FAILURE", Effect: "CONFIRMED", Evidence: evidence}, nil
	}

	// Expiry follows a current authorized Content lookup. Preserve a previously
	// committed acquisition fact without reviving its retired body or reference.
	if err == fetch.Expired {
		previous, committed, e := d.store.Outcome(ctx, in.Task.Namespace, in.OperationID)
		if e != nil {
			return unknown, e
		}
		if committed && previous.Status == "acquired" {
			evidence, _ := json.Marshal(struct {
				Status   string `json:"status"`
				Mode     string `json:"mode,omitempty"`
				Requests uint32 `json:"requests"`
			}{"expired", previous.Mode, previous.Requests})
			return execution.Observation{Phase: "FINISHED", Result: "FAILURE", Effect: "CONFIRMED", Output: outcomeOutput("expired", ""), Evidence: evidence}, nil
		}
	}
	if err != nil {
		return unknown, err
	}
	if !known {
		return unknown, nil
	}
	evidence, _ := json.Marshal(struct {
		Status   string `json:"status"`
		Mode     string `json:"mode,omitempty"`
		Requests uint32 `json:"requests"`
	}{result.Status, result.Mode, result.Requests})
	if result.Status != "acquired" {
		if result.Requests == 0 {
			return execution.Observation{Phase: "NOT_STARTED", Result: "FAILURE", Effect: "NOT_OCCURRED", Output: outcomeOutput(result.Status, ""), Evidence: evidence}, nil
		}
		return execution.Observation{Phase: "FINISHED", Result: "FAILURE", Effect: "CONFIRMED", Output: outcomeOutput(result.Status, ""), Evidence: evidence}, nil
	}
	return execution.Observation{Phase: "FINISHED", Result: "SUCCESS", Effect: "CONFIRMED", Output: outcomeOutput(result.Status, result.Reference), Evidence: evidence}, nil
}
func outcomeOutput(status, reference string) []byte {
	output, _ := json.Marshal(struct {
		Status    string `json:"status"`
		Reference string `json:"reference"`
	}{status, reference})
	return output
}
func (d *Driver) input(raw []byte) (websearch.Request, error) {
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
	if !resultOK || results < 1 || results > int64(d.config.MaxResults) || !bOK || !rOK || !tOK || bytes < 1 || bytes > d.config.MaxBytes || requests < 1 || requests > int64(d.config.MaxRequests) || millis < 1 || millis > d.config.Timeout.Milliseconds() {
		return websearch.Request{}, fetch.Invalid
	}
	return websearch.Request{Query: query, MaxResults: int(results), MaxBytes: bytes, MaxRequests: int(requests), Timeout: time.Duration(millis) * time.Millisecond}, nil
}

func (d *Driver) scoped(request execution.Request) (*Driver, error) {
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
