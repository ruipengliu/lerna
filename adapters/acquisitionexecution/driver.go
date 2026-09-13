// Package acquisitionexecution binds admitted page and search calls to the
// shared acquisition ledger. Only Execution may invoke this trusted seam.
package acquisitionexecution

import (
	"context"
	"encoding/json"
	"lerna/execution"
	"lerna/fetch"
	"time"
)

type Operations interface {
	NewOperation(context.Context, string) (string, error)
}
type TaskGuard interface {
	Check(context.Context, execution.Request) error
}

// ObservationScope binds business reads to the original admitted request.
// Binding supplies no authority; each observation must check the current task
// and original action before I/O, including recovery and failed observations.
type ObservationScope interface {
	Bind(execution.Request) (fetch.OutcomeStore, fetch.Evidence, error)
}

type Config struct {
	Observations ObservationScope
	// Guard is required: every execution fetch checks current task qualification.
	Guard                     TaskGuard
	Token, Namespace, Subject string
	Capability                execution.Capability
	MaxBytes                  int64
	MaxRequests, TaskLimit    uint32
	Timeout                   time.Duration
}

// driver owns the shared identity and observation semantics. Each transport
// retains its typed input parser and acquisition coordinator.
type driver struct {
	store      fetch.OutcomeStore
	operations Operations
	config     Config
}

func (c Config) valid() bool {
	return !(c.Guard == nil || c.Token == "" || c.Namespace == "" || c.Subject == "" || c.Capability.Name == "" || c.Capability.Version == "" || c.Capability.Implementation == "" || c.Capability.ImplementationVersion == "" || c.MaxBytes < 1 || c.MaxBytes > 1<<20 || c.MaxRequests < 1 || c.MaxRequests > 5 || c.TaskLimit < c.MaxRequests || c.TaskLimit > 128 || c.Timeout < time.Millisecond || c.Timeout > 5*time.Second)
}

func (d *driver) identity(c execution.Call) error {
	r := c.Request
	cap := d.config.Capability
	if r.OperationID == "" || r.InputRef == "" || r.Qualification.Ref.Namespace != d.config.Namespace || r.Qualification.Ref.TaskID == "" || r.Qualification.Version == 0 || r.Capability != cap.Name || r.Version != cap.Version || r.Implementation != cap.Implementation || r.ImplementationVersion != cap.ImplementationVersion || r.DescriptorSHA256 != cap.Digest() {
		return fetch.Denied
	}
	return nil
}
func (d *driver) original(ctx context.Context, c execution.Call) (fetch.AttemptIntent, error) {
	in, err := d.store.Inspect(ctx, d.config.Namespace, c.Request.OperationID)
	if err != nil {
		return in, err
	}
	if in.Subject != d.config.Subject || in.Task != c.Request.Qualification.Ref || in.Fingerprint != c.Request.Fingerprint() {
		return fetch.AttemptIntent{}, fetch.IdentityConflict
	}
	return in, nil
}
func (d *driver) inspect(ctx context.Context, c execution.Call, recover func(context.Context, fetch.AttemptIntent) (fetch.Outcome, bool, error)) (execution.Observation, error) {
	unknown := execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}
	in, err := d.original(ctx, c)
	if err == fetch.Missing {
		return unknown, nil
	}
	if err != nil {
		return unknown, err
	}
	result, known, err := recover(ctx, in)
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
