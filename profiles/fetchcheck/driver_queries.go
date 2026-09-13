package fetchcheck

import (
	"lerna/adapters/fetchqueries"
	"lerna/adapters/fetchtask"
	"lerna/artifacts"
	"lerna/execution"
	"lerna/tasks"
)

func acquisitionQueries(h *harness, port *tasks.ActionPort, capability execution.Capability) (*fetchqueries.Scope, error) {
	guard, err := fetchtask.New(h.auth, port, h.core, h.token)
	if err != nil {
		return nil, err
	}
	return fetchqueries.New(h.attempts, h.content, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, h.evidenceConfig, capability, port, guard)
}
