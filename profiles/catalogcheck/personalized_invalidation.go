package catalogcheck

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"strings"
)

func personalizedFault(mode string) (string, string) {
	parts := strings.Split(mode, "-")
	if len(parts) != 3 || parts[0] != "personalized" {
		return "", ""
	}
	if parts[1] != "related" && parts[1] != "unrelated" && parts[1] != "revoke" && parts[1] != "delete" {
		return "", ""
	}
	if parts[2] != "admitted" && parts[2] != "invoked" && parts[2] != "model" && parts[2] != "save" {
		return "", ""
	}
	return parts[1], parts[2]
}

// change is a deterministic fault injection through real Memory/grant ports.
// It runs once, preserving the original action identity on any later recovery.
func (p *personalizedMemory) change(ctx context.Context, h *harness, kind string) error {
	if p.changed {
		return nil
	}
	if kind == "revoke" {
		grant, e := p.grants.Get(ctx, h.token, p.grantID)
		if e != nil {
			return e
		}
		op, e := h.operation(ctx)
		if e != nil {
			return e
		}
		state, e := h.db.Load(ctx)
		if e != nil {
			return e
		}
		_, e = p.grants.Mutate(ctx, h.token, &wire.GrantMutation{OperationId: op, ExpectedRevision: state.State.Revision, Kind: "REVOKE", GrantId: p.grantID, ExpectedGrantRevision: grant.Revision})
		if e == nil {
			p.changed = true
		}
		return e
	}
	if kind == "delete" {
		op, e := h.operation(ctx)
		if e != nil {
			return e
		}
		binding := p.binding
		binding.Recipient = binding.Location
		receipt, e := p.service.Delete(ctx, binding, memory.DeleteRequest{OperationID: op, Ref: &wire.MemoryRef{Namespace: binding.Namespace, Collection: "personal", Key: "record-choice"}, ExpectedRevision: p.latestRevision, Purpose: "task"})
		if e == nil {
			p.latestRevision = receipt.Revision
			p.deletion = &memory.SourceEvent{Ref: receipt.Ref, Kind: memory.SourceDeleted, Revision: receipt.Revision, Position: receipt.Position}
			p.changed = true
		}
		return e
	}
	write := proto.Clone(p.original).(*wire.MemoryWrite)
	if kind == "unrelated" {
		write.Ref.Key = "unrelated-choice"
		op, e := h.operation(ctx)
		if e != nil {
			return e
		}
		write.OperationId = op
		if _, e = p.service.Put(ctx, p.binding, write); e != nil {
			return e
		}
	}
	op, e := h.operation(ctx)
	if e != nil {
		return e
	}
	write.OperationId = op
	write.ExpectedRevision = p.latestRevision
	if kind == "unrelated" {
		write.ExpectedRevision = 1
	}
	write.Spec.Content.Json = []byte(`{"style":"alternative"}`)
	receipt, e := p.service.Correct(ctx, p.binding, write)
	if e == nil {
		if kind != "unrelated" {
			p.latestRevision = receipt.Revision
		}
		p.changed = true
	}
	return e
}

// Change current authority only after the model has consumed its original input.
type contextAfterModel struct {
	brain.Model
	change func() error
}

func (m contextAfterModel) Generate(ctx context.Context, in brain.Request) (brain.Result, error) {
	out, err := m.Model.Generate(ctx, in)
	if e := m.change(); e != nil {
		return out, e
	}
	return out, err
}

type unknownUsageModel struct{ actionScript }

func (m *unknownUsageModel) Generate(ctx context.Context, in brain.Request) (brain.Result, error) {
	out, err := m.actionScript.Generate(ctx, in)
	out.Usage = brain.Usage{}
	return out, err
}

// RunUnknownUsageActionCase exercises a dispatched model request whose usage is
// unknown. It is a deterministic contract fixture and never calls a provider.
func RunUnknownUsageActionCase(ctx context.Context, mode string) (ActionReport, error) {
	return RunActionCase(ctx, mode, &unknownUsageModel{})
}
