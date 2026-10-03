package development

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/runtime"
)

// Non-worker processes register the accurate capabilities and durable admission
// methods; they never acquire a physical target or run its outbound job.
type contractOnlyDriver struct{ capability domain.Capability }

func (d contractOnlyDriver) Capability() domain.Capability { return d.capability }
func (contractOnlyDriver) Prepare(context.Context, runtime.Scope, runtime.Auth, domain.InvokeInput, domain.ExecutionIntent, []byte) (domain.PreparedRequest, error) {
	return domain.PreparedRequest{}, api.E("unsupported", "worker_target_ownership_required")
}
func (contractOnlyDriver) Start(context.Context, domain.AttemptRequest, func(context.Context) error) (domain.Fact, error) {
	return domain.Fact{}, api.E("unsupported", "worker_target_ownership_required")
}
func (contractOnlyDriver) Reconcile(context.Context, domain.AttemptRequest) (domain.Fact, error) {
	return domain.Fact{}, api.E("unsupported", "worker_target_ownership_required")
}
func (contractOnlyDriver) Stop(context.Context, domain.AttemptRequest) (domain.StopFact, error) {
	return domain.StopFact{}, api.E("unsupported", "worker_target_ownership_required")
}

type contractOnlyResources struct{}

func (contractOnlyResources) Fence(context.Context, runtime.Scope, domain.ResourceLease) (domain.StopFact, error) {
	return domain.StopFact{}, api.E("unsupported", "worker_target_ownership_required")
}
func (contractOnlyResources) Observe(context.Context, runtime.Scope, domain.ResourceLease, string) (domain.Observation, error) {
	return domain.Observation{}, api.E("unsupported", "worker_target_ownership_required")
}
func (contractOnlyResources) ObservationSchema() api.Schema {
	return api.SchemaFor[execution.PhoneState]()
}
