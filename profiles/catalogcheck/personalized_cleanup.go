package catalogcheck

import (
	"context"
	"crypto/sha256"
	"fmt"
	"lerna/memory"
	"time"
)

func actionArtifactCleanupBinding(namespace string) memory.ConsumerBinding {
	hash := sha256.Sum256([]byte("action-artifact-source-cleanup:content:v1"))
	return memory.ConsumerBinding{Namespace: namespace, Collection: "personal", Consumer: "action-artifacts", ConfigSHA256: fmt.Sprintf("%x", hash)}
}

// This local conformance report observes committed progress. It neither drives
// cleanup nor grants an ordinary caller access to internal source metadata.
func (p *personalizedMemory) observeDeletionCleanup(ctx context.Context) (memory.DeletionStatus, error) {
	binding := actionArtifactCleanupBinding(p.binding.Namespace)
	admission := actionAdmissionCleanupBinding(p.binding.Namespace)
	hash := sha256.Sum256([]byte("action-context-source-cleanup:context.db:v1"))
	contexts := memory.ConsumerBinding{Namespace: p.binding.Namespace, Collection: "personal", Consumer: "action-contexts", ConfigSHA256: fmt.Sprintf("%x", hash)}
	reporter, err := memory.NewDeletionReporter(p.store, p, []memory.CleanupTarget{{Name: "artifacts", Dimension: memory.DerivedCleanup, Binding: &binding}, {Name: "contexts", Dimension: memory.DerivedCleanup, Binding: &contexts}, {Name: "archives", Dimension: memory.DerivedCleanup}, {Name: "legacy-checkpoints", Dimension: memory.DerivedCleanup, Binding: &p.checkpointBinding}, {Name: "controlled-backups", Dimension: memory.DerivedCleanup}, {Name: "admission-comparisons", Dimension: memory.LocalCleanup, Binding: &admission}})
	if err != nil {
		return memory.DeletionStatus{}, err
	}
	wait, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	for {
		status, err := reporter.Status(wait, *p.deletion)
		if err != nil {
			return status, err
		}
		pending := false
		for _, group := range [][]memory.CleanupProgress{status.Local, status.Derived} {
			for _, target := range group {
				pending = pending || target.State == "pending"
			}
		}
		if !pending {
			return status, nil
		}
		select {
		case <-wait.Done():
			return status, nil
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func actionAdmissionCleanupBinding(namespace string) memory.ConsumerBinding {
	hash := sha256.Sum256([]byte("action-admission-comparison-cleanup:authority.db:v1"))
	return memory.ConsumerBinding{Namespace: namespace, Collection: "personal", Consumer: "action-admissions", ConfigSHA256: fmt.Sprintf("%x", hash)}
}

func (p *personalizedMemory) InspectConsumer(ctx context.Context, b memory.ConsumerBinding) (uint64, error) {
	if b.Consumer == p.checkpointBinding.Consumer {
		return p.checkpoints.InspectConsumer(ctx, b)
	}
	return p.store.InspectConsumer(ctx, b)
}
