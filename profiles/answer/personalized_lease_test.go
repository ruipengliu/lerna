package answer

import (
	"context"
	"lerna/brain"
	"testing"
	"time"
)

func TestPersonalizedAnswerPublishesAfterOriginalLeaseExpires(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	model := &longAnswerModel{controlledModel: controlledModel{fn: func(ctx context.Context, request brain.Request) (brain.Result, error) {
		timer := time.NewTimer(limits().Lease + 100*time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return brain.Result{}, ctx.Err()
		case <-timer.C:
		}
		return (&personalizedModel{}).Generate(ctx, request)
	}}}
	report, err := RunPersonalizedAnswer(ctx, "concise", true, model)
	if err != nil {
		t.Fatal(err)
	}
	if report.State != "COMPLETED" || report.Answer != "Memory, Brain, Execution" || report.ReadAllocated != 1 {
		t.Fatalf("long-running answer lost original task or memory: %+v", report)
	}
}

// The public model entry reserves 512 output tokens; declare enough context for
// that reservation as well as the existing synthetic input bound.
type longAnswerModel struct{ controlledModel }

func (m *longAnswerModel) Capabilities() brain.Capabilities {
	c := m.controlledModel.Capabilities()
	c.ContextTokens = 2048
	return c
}
