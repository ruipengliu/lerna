package memory

import (
	"context"
	"encoding/hex"
	"time"
)

// SourceSink applies one authoritative event idempotently. Complete means its
// own controlled cleanup has finished, never that all replicas are clean.
type SourceSink interface {
	Apply(context.Context, SourceEvent) (complete bool, err error)
}

type ConsumerConfig struct {
	Binding ConsumerBinding
	Batch   int
	Timeout time.Duration
}

// ConsumerResult reports the last confirmed position. On acknowledgment error,
// the actual durable position may be newer; the next Run reconciles it first.
type ConsumerResult struct {
	Position uint64
	Applied  int
	Pending  bool
}

type SourceConsumer struct {
	events   SourceEvents
	progress ConsumerProgress
	sink     SourceSink
	config   ConsumerConfig
}

func NewSourceConsumer(events SourceEvents, progress ConsumerProgress, sink SourceSink, config ConsumerConfig) (*SourceConsumer, error) {
	b := config.Binding
	hash, err := hex.DecodeString(b.ConfigSHA256)
	if events == nil || progress == nil || sink == nil || !text(b.Namespace, 256) || !text(b.Collection, 256) || !text(b.Consumer, 256) || err != nil || len(hash) != 32 || hex.EncodeToString(hash) != b.ConfigSHA256 || config.Batch < 1 || config.Batch > 32 || config.Timeout <= 0 || config.Timeout > 10*time.Second {
		return nil, Invalid
	}
	return &SourceConsumer{events, progress, sink, config}, nil
}

// Run is one bounded batch. Configuration is durably bound before any effect;
// failed or incomplete cleanup never advances the applied position. Hosts must
// pin all effective sink configuration in Binding.ConfigSHA256 and use a new
// consumer identity when changing that configuration.
func (c *SourceConsumer) Run(ctx context.Context) (ConsumerResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	b := c.config.Binding
	position, err := c.progress.BindConsumer(ctx, b)
	result := ConsumerResult{Position: position}
	if err != nil {
		return result, err
	}
	events, err := c.events.ReadEvents(ctx, b.Namespace, b.Collection, position, c.config.Batch)
	if err != nil {
		return result, err
	}
	if len(events) > c.config.Batch {
		return result, Invalid
	}
	// Validate the entire returned batch before performing any effects.
	for _, event := range events {
		if !ValidSourceEvent(event) || event.Ref.Namespace != b.Namespace || event.Ref.Collection != b.Collection || position >= 1<<63-1 || event.Position != position+1 {
			return result, Invalid
		}
		position = event.Position
	}
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		complete, err := c.sink.Apply(ctx, event)
		if err != nil {
			return result, err
		}
		if !complete {
			result.Pending = true
			return result, nil
		}
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if err = c.progress.AckEvent(ctx, b, result.Position, event.Position); err != nil {
			return result, err
		}
		result.Position = event.Position
		result.Applied++
	}
	return result, nil
}

// ValidSourceEvent validates trusted adapter output before cleanup effects.
func ValidSourceEvent(event SourceEvent) bool {
	if !text(event.Ref.Namespace, 256) || !text(event.Ref.Collection, 256) || !text(event.Ref.Key, 256) || event.Position == 0 || event.Position > 1<<63-1 || event.Revision == 0 || event.Revision > 1<<32 {
		return false
	}
	switch event.Kind {
	case SourceErased:
		return true
	case SourceCreated:
		return event.Revision == 1
	case SourceCorrected, SourceDeleted:
		return event.Revision > 1
	default:
		return false
	}
}
