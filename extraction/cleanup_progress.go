package extraction

import "context"

// CleanupBinding fixes a trusted worker's effective scope/configuration. A
// checkpoint is scheduling progress only, never a consumer erasure receipt.
type CleanupBinding struct{ Namespace, Subject, Consumer, ConfigSHA256 string }
type CleanupProgress struct {
	Version uint64
	After   string
}
type CleanupProgressStore interface {
	LoadCleanupProgress(context.Context, CleanupBinding) (CleanupProgress, error)
	AdvanceCleanupProgress(context.Context, CleanupBinding, uint64, string) error
}
