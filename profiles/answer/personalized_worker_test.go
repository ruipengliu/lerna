package answer

import (
	"context"
	"testing"
	"time"

	"lerna/brain"
	"lerna/tasks"
)

// The host binds the personalized publication guard after Core reserves the
// decision. Every commit still passes through the actual answer port.
type personalizedWorkerPort struct {
	*tasks.GenerationPort
	h *harness
}

func (p personalizedWorkerPort) Commit(ctx context.Context, c tasks.WorkChange) (tasks.RunSnapshot, error) {
	return p.h.port.Commit(ctx, c)
}

func TestPersonalizedAnswerWorkerWaitsForInvalidatedContext(t *testing.T) {
	for _, mutation := range []string{"related", "revoke", "unrelated"} {
		t.Run(mutation, func(t *testing.T) {
			ctx := context.Background()
			l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
			h, err := fresh(ctx, l)
			if err != nil {
				t.Fatal(err)
			}
			defer h.destroy()
			candidate, err := h.submit(ctx, l)
			if err != nil {
				t.Fatal(err)
			}
			var memory *personalizedMemory
			defer func() {
				if memory != nil {
					memory.close()
				}
			}()
			decide := tasks.BrainFunc(func(ctx context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
				current, err := h.core.Load(ctx, in.Task.Ref)
				if err != nil {
					return tasks.Proposal{}, err
				}
				memory, err = h.personalize(ctx, current, "concise", true)
				if err != nil {
					return tasks.Proposal{}, err
				}
				model := &controlledModel{fn: func(ctx context.Context, r brain.Request) (brain.Result, error) {
					if err := memory.change(ctx, h, mutation); err != nil {
						return brain.Result{}, err
					}
					return (&personalizedModel{}).Generate(ctx, r)
				}}
				b, err := brain.NewAnswer(model, memory.session, h.access, h.generation, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: brain.MaxAnswerBytes, SettlementTimeout: time.Second})
				if err != nil {
					return tasks.Proposal{}, err
				}
				return b.Decide(ctx, in)
			})
			runner, err := tasks.NewRunner(personalizedWorkerPort{h.generation, h}, decide, limits(), wallClock{})
			if err != nil {
				t.Fatal(err)
			}
			got, err := runner.Run(ctx, candidate)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Generations) != 1 || !got.Generations[0].Settled || got.Generations[0].Usage.Requests != 1 || got.Generations[0].Usage.UnknownRequests != 0 {
				t.Fatalf("lost usage: state=%s reason=%s generations=%+v", got.Task.State, got.Task.StopReason, got.Generations)
			}
			if mutation == "unrelated" {
				if got.Task.State != "COMPLETED" || got.Task.Result == "" {
					t.Fatalf("unrelated correction blocked answer: %+v", got.Task)
				}
			} else {
				reason := "INPUT_INVALIDATED"
				if mutation == "revoke" {
					reason = "PROCESSING_DENIED"
				}
				if got.Task.State != "WAITING" || got.Task.StopReason != reason || got.Task.Result != "" {
					t.Fatalf("context failure must wait without publication: %+v", got.Task)
				}
			}
		})
	}
}
