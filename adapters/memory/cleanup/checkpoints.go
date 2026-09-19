package cleanup

import (
	"context"

	"lerna/memory"
)

// CheckpointFiles belongs to the host that verifies the original task and file.
// Inspect is read-only and confirms that no legacy comparisons remain. Maintain
// must verify original integrity before atomically replacing a legacy file.
type CheckpointFiles struct {
	Maintain func(context.Context) error
	Inspect  func(context.Context) (bool, error)
}

type checkpointProgress interface {
	memory.ConsumerProgress
	memory.ConsumerInspection
}

type Checkpoints struct {
	files    CheckpointFiles
	progress checkpointProgress
	config   memory.ConsumerConfig
	consumer *memory.SourceConsumer
}

func NewCheckpoints(events memory.SourceEvents, progress checkpointProgress, files CheckpointFiles, config memory.ConsumerConfig) (*Checkpoints, error) {
	if files.Maintain == nil || files.Inspect == nil {
		return nil, memory.Invalid
	}
	out := &Checkpoints{files: files, progress: progress, config: config}
	consumer, err := memory.NewSourceConsumer(events, progress, out, config)
	if err != nil {
		return nil, err
	}
	out.consumer = consumer
	return out, nil
}
func (c *Checkpoints) maintain(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	if err := c.files.Maintain(ctx); err != nil {
		return err
	}
	clean, err := c.files.Inspect(ctx)
	if err != nil {
		return err
	}
	if !clean {
		return memory.Unavailable
	}
	return nil
}
func (c *Checkpoints) Apply(ctx context.Context, event memory.SourceEvent) (bool, error) {
	b := c.config.Binding
	if !memory.ValidSourceEvent(event) || event.Ref.Namespace != b.Namespace || event.Ref.Collection != b.Collection {
		return false, memory.Invalid
	}
	err := c.maintain(ctx)
	return err == nil, err
}

// Run confirms actual file maintenance before SourceConsumer commits each
// position. It also checks for a late legacy file when no new event exists.
func (c *Checkpoints) Run(ctx context.Context) (memory.ConsumerResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	result, err := c.consumer.Run(ctx)
	if err != nil {
		return result, err
	}
	return result, c.maintain(ctx)
}

// InspectConsumer cannot elevate a cursor while a late/invalid legacy file is
// present. It performs no migration or acknowledgment and rechecks after reading
// durable progress so an intervening file arrival cannot reuse old completion.
func (c *Checkpoints) InspectConsumer(ctx context.Context, b memory.ConsumerBinding) (uint64, error) {
	if b != c.config.Binding {
		return 0, memory.IdentityConflict
	}
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	check := func() error {
		clean, err := c.files.Inspect(ctx)
		if err != nil {
			return err
		}
		if !clean {
			return memory.Missing
		}
		return nil
	}
	if err := check(); err != nil {
		return 0, err
	}
	position, err := c.progress.InspectConsumer(ctx, b)
	if err != nil {
		return 0, err
	}
	if err = check(); err != nil {
		return 0, err
	}
	return position, nil
}
