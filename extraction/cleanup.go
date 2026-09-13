package extraction

import (
	"context"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"time"
)

type CleanupJournal interface {
	SaveJournal
	ReserveCleanup(context.Context, string, string, string, memory.DeleteRequest) error
	LookupCleanup(context.Context, string, string, string) (memory.DeleteRequest, error)
}

// CleanupDiscovery lists identities only, from a trusted host scope. Callers
// run bounded passes and restart from the beginning after reaching the end:
// concurrent new retirements may sort before an earlier cursor. Presence does
// not mean deletion is pending or complete; Clean reconciles the original fact.
type CleanupDiscovery interface {
	ListRetiredSaves(context.Context, string, string, string, int) ([]string, error)
}
type MemoryCleaner interface {
	InspectOperation(context.Context, memory.Binding, string) (memory.OperationState, error)
	Delete(context.Context, memory.Binding, memory.DeleteRequest) (memory.Receipt, error)
	DeletionStatus(context.Context, memory.Binding, memory.DeletionStatusRequest) (memory.DeletionInspection, error)
}
type Cleaner struct {
	journal  CleanupJournal
	memory   MemoryCleaner
	binding  memory.Binding
	purpose  string
	allocate func(context.Context) (string, error)
}

func NewCleaner(journal CleanupJournal, service MemoryCleaner, b memory.Binding, purpose string, allocate func(context.Context) (string, error)) (*Cleaner, error) {
	if journal == nil || service == nil || allocate == nil || b.Token == "" || b.Namespace == "" || b.Subject == "" || b.Location == "" || b.Recipient != b.Location || purpose == "" {
		return nil, memory.Invalid
	}
	return &Cleaner{journal, service, b, purpose, allocate}, nil
}

// Clean performs a bounded cleanup attempt for one retired candidate. Memory
// checks current deletion authority independently of revoked source access.
// The returned report preserves unconnected/pending consumer dimensions; a
// committed deletion is never presented as global cleanup completion.
func (c *Cleaner) Clean(ctx context.Context, candidate string) (memory.DeletionInspection, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	save, err := c.journal.LookupSave(ctx, c.binding.Namespace, c.binding.Subject, candidate)
	if err != nil {
		return memory.DeletionInspection{}, err
	}
	if save.State != "retired" {
		return memory.DeletionInspection{}, memory.Denied
	}
	request, err := c.journal.LookupCleanup(ctx, c.binding.Namespace, c.binding.Subject, candidate)
	if err == memory.Missing {
		original, e := c.memory.InspectOperation(ctx, c.binding, save.Request.OperationId)
		if e != nil {
			return memory.DeletionInspection{}, e
		}
		if original.State != "committed" || original.Receipt == nil {
			return memory.DeletionInspection{State: original.State}, memory.Unavailable
		}
		ref := original.Receipt.Ref
		if ref.Namespace != save.Request.Ref.Namespace || ref.Collection != save.Request.Ref.Collection || ref.Key != save.Request.Ref.Key {
			return memory.DeletionInspection{}, memory.IdentityConflict
		}
		id, e := c.allocate(ctx)
		if e != nil {
			return memory.DeletionInspection{}, e
		}
		request = memory.DeleteRequest{OperationID: id, Ref: proto.Clone(save.Request.Ref).(*wire.MemoryRef), ExpectedRevision: original.Receipt.Revision, Purpose: c.purpose}
		e = c.journal.ReserveCleanup(ctx, c.binding.Namespace, c.binding.Subject, candidate, request)
		if e != nil && e != memory.IdentityConflict && e != memory.Unavailable {
			return memory.DeletionInspection{}, e
		}
		request, err = c.journal.LookupCleanup(ctx, c.binding.Namespace, c.binding.Subject, candidate)
	}
	if err != nil {
		return memory.DeletionInspection{}, err
	}
	inspect := memory.DeletionStatusRequest{OperationID: request.OperationID, Ref: request.Ref, Purpose: request.Purpose}
	state, err := c.memory.DeletionStatus(ctx, c.binding, inspect)
	if err != nil {
		return state, err
	}
	if state.State != "not_admitted" {
		return state, nil
	}
	_, deleteErr := c.memory.Delete(ctx, c.binding, request)
	state, err = c.memory.DeletionStatus(ctx, c.binding, inspect)
	if err != nil {
		return state, err
	}
	if state.State == "committed" {
		return state, nil
	}
	if deleteErr != nil {
		return state, deleteErr
	}
	return state, memory.Unavailable
}
