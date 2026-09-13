package answer

import (
	"context"
	"fmt"
	"google.golang.org/protobuf/proto"
	"lerna/answers"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/tasks"
	"time"
)

type InvalidationReport struct {
	Published, OutputSaved bool
	ModelCalls             int
	ReadAllocated          uint64
	ValidationError        string
}

// RunPersonalizedInvalidation changes actual governed Memory or its signed read
// grant at a controlled model/publication boundary. It uses a deterministic model
// and never calls an external provider. Unrelated changes must still publish.
func RunPersonalizedInvalidation(ctx context.Context, phase, change string) (InvalidationReport, error) {
	report := InvalidationReport{}
	if phase != "model" && phase != "publication" {
		return report, fmt.Errorf("invalid boundary")
	}
	if change != "related" && change != "unrelated" && change != "revoke" && change != "delete" {
		return report, fmt.Errorf("invalid mutation")
	}
	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, e := fresh(ctx, l)
	if e != nil {
		return report, e
	}
	defer h.destroy()
	s, e := h.submit(ctx, l)
	if e != nil {
		return report, e
	}
	s, e = start(ctx, h, s)
	if e != nil {
		return report, e
	}
	stopRenewal := keepAnswerRecoveryLease(ctx, h, tasks.QualificationOf(s))
	defer stopRenewal()
	p, e := h.personalize(ctx, s, "concise", true)
	if e != nil {
		return report, e
	}
	defer p.close()
	var mutationError error
	model := &controlledModel{fn: func(ctx context.Context, r brain.Request) (brain.Result, error) {
		report.ModelCalls++
		if phase == "model" {
			mutationError = p.change(ctx, h, change)
			if mutationError != nil {
				return brain.Result{}, mutationError
			}
		}
		return (&personalizedModel{}).Generate(ctx, r)
	}}
	b, e := brain.NewAnswer(model, p.session, h.access, h.generation, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: brain.MaxAnswerBytes, SettlementTimeout: time.Second})
	if e != nil {
		return report, e
	}
	proposal, validation := b.Decide(ctx, tasks.DecisionInput{Task: s.Task, Work: s.Work[0], Generation: s.Generations[0]})
	if mutationError != nil {
		return report, mutationError
	}
	report.OutputSaved = proposal.Result != ""
	if phase == "publication" {
		if validation != nil {
			return report, validation
		}
		stored, e := h.content.Call(ctx, h.binding(), &wire.ContentRequest{Method: "LOOKUP", OperationId: s.Generations[0].OutputOperation, Purpose: "task"})
		if e != nil {
			return report, e
		}
		report.OutputSaved = stored.GetRecord().GetState() == "available" && answers.Reference(stored.Record.Ref) == proposal.Result
		if !report.OutputSaved {
			return report, fmt.Errorf("publication fault has no durable output")
		}
		if e = p.change(ctx, h, change); e != nil {
			return report, e
		}
	}
	if validation == nil {
		_, validation = h.port.Commit(ctx, tasks.WorkChange{Kind: "complete", ChangeID: "memory-invalidation-publication", Qualification: tasks.QualificationOf(s), Finished: true, Proposal: proposal})
	}
	if validation != nil {
		report.ValidationError = validation.Error()
	}
	current, e := h.core.Load(ctx, s.Task.Ref)
	if e != nil {
		return report, e
	}
	report.Published = current.Task.State == "COMPLETED" && current.Task.Result != ""
	if !report.Published && current.Task.Result != "" {
		return report, fmt.Errorf("unpublished task exposed result")
	}
	grant, e := p.grants.Get(ctx, h.token, p.grantID)
	if e != nil {
		return report, e
	}
	report.ReadAllocated = grant.Allocated
	return report, nil
}
func (p *personalizedMemory) change(ctx context.Context, h *harness, kind string) error {
	if kind == "revoke" {
		grant, e := p.grants.Get(ctx, h.token, p.grantID)
		if e != nil {
			return e
		}
		op, e := h.auth.NewOperation(ctx, h.token)
		if e != nil {
			return e
		}
		state, e := h.db.Load(ctx)
		if e != nil {
			return e
		}
		_, e = p.grants.Mutate(ctx, h.token, &wire.GrantMutation{OperationId: op, ExpectedRevision: state.State.Revision, Kind: "REVOKE", GrantId: p.grantID, ExpectedGrantRevision: grant.Revision})
		return e
	}
	if kind == "delete" {
		op, e := h.auth.NewOperation(ctx, h.token)
		if e != nil {
			return e
		}
		binding := p.binding
		binding.Recipient = binding.Location
		_, e = p.service.Delete(ctx, binding, memory.DeleteRequest{OperationID: op, Ref: p.original.Ref, ExpectedRevision: 1, Purpose: p.original.Spec.Purpose})
		return e
	}
	write := proto.Clone(p.original).(*wire.MemoryWrite)
	if kind == "unrelated" {
		write.Ref.Key = "unrelated-style"
		op, e := h.auth.NewOperation(ctx, h.token)
		if e != nil {
			return e
		}
		write.OperationId = op
		if _, e = p.service.Put(ctx, p.binding, write); e != nil {
			return e
		}
	}
	op, e := h.auth.NewOperation(ctx, h.token)
	if e != nil {
		return e
	}
	write.OperationId = op
	write.ExpectedRevision = 1
	write.Spec.Content.Json = []byte(`{"style":"detailed"}`)
	_, e = p.service.Correct(ctx, p.binding, write)
	return e
}
