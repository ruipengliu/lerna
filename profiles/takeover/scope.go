package takeover

import (
	"context"
	"lerna/execution"
	"time"
)

func capabilityFor(entry string) execution.Capability {
	c := capability()
	if entry == "sync" {
		c.Name = "counter.sync"
		c.Implementation = "sqlite-sync"
		c.Synchronous = true
		c.Async = nil
	}
	if entry == "scale" {
		c.Name = "counter.scale"
		c.Implementation = "sqlite-scale"
	}
	return c
}
func capabilityAt(entry, key string) execution.Capability {
	c := capabilityFor(entry)
	c.Implementation += "/" + key
	if c.Async != nil {
		c.Async.Target = "counter-jobs/" + key
	}
	return c
}
func scope() execution.ResourceScope { return scopeAt("main") }
func scopeAt(key string) execution.ResourceScope {
	return execution.ResourceScope{Ref: execution.ResourceRef{Namespace: "local", Kind: "counter", Key: key}, Aliases: []execution.ResourceRef{{Namespace: "local", Kind: "counter", Key: key + "-alias"}}, Authority: "local-resource-authority", AuthorizationResource: "root", Purpose: "task", Location: "local", Participants: []string{capabilityAt("add", key).Digest(), capabilityAt("scale", key).Digest(), capabilityAt("sync", key).Digest()}, MaxOperations: 32, MaxChecks: 8, PollInterval: time.Second, Lease: 3 * time.Second, Window: time.Minute}
}
func (h *harness) other(ctx context.Context) (*harness, error) {
	return openEntry(ctx, h.root, h.token, "scale")
}
