// Package memorylocal binds authenticated host identity outside Memory payloads.
package memorylocal

import (
	"context"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/internal/memorywire"
	"lerna/internal/randomid"
	"lerna/memory"
)

type Writes interface {
	Put(context.Context, memory.Binding, *wire.MemoryWrite) (memory.Receipt, error)
	Correct(context.Context, memory.Binding, *wire.MemoryWrite) (memory.Receipt, error)
	InspectOperation(context.Context, memory.Binding, string) (memory.OperationState, error)
}
type Deletes interface {
	Delete(context.Context, memory.Binding, memory.DeleteRequest) (memory.Receipt, error)
}

type DeletionStatus interface {
	DeletionStatus(context.Context, memory.Binding, memory.DeletionStatusRequest) (memory.DeletionInspection, error)
}

type Reads interface {
	Query(context.Context, memory.Binding, *wire.MemoryQuery, string) (*wire.MemoryReadResult, error)
	Get(context.Context, memory.Binding, *wire.MemoryGet, string) (*wire.MemoryReadResult, error)
}
type Binding struct {
	writes   Writes
	reads    Reads
	identity memory.Binding
}

func Bind(writes Writes, reads Reads, identity memory.Binding) *Binding {
	return &Binding{writes, reads, identity}
}
func (b *Binding) Exchange(ctx context.Context, data []byte) ([]byte, error) {
	if len(data) > memorywire.MaxRequest || b.writes == nil || b.reads == nil {
		return nil, memory.Invalid
	}
	in := new(wire.MemoryRequest)
	if proto.Unmarshal(data, in) != nil || !memorywire.Request(in) {
		return nil, memory.Invalid
	}
	if in.Namespace != b.identity.Namespace {
		return nil, memory.Denied
	}
	id, e := randomid.New()
	if e != nil {
		return nil, memory.Unavailable
	}
	out := &wire.MemoryResponse{MessageId: id, ReplyTo: in.MessageId, Namespace: in.Namespace, Code: "OK"}
	switch in.Method {
	case "PUT", "CORRECT":
		var receipt memory.Receipt
		if in.Method == "PUT" {
			receipt, e = b.writes.Put(ctx, b.identity, in.Write)
		} else {
			receipt, e = b.writes.Correct(ctx, b.identity, in.Write)
		}
		if e == nil {
			out.Receipt = memorywire.Receipt(receipt)
		}
	case "DELETION_STATUS":
		status, ok := b.writes.(DeletionStatus)
		if !ok {
			e = memory.Unavailable
			break
		}
		q := in.DeletionQuery
		var result memory.DeletionInspection
		result, e = status.DeletionStatus(ctx, b.identity, memory.DeletionStatusRequest{OperationID: q.OperationId, Ref: q.Ref, Purpose: q.Purpose})
		if e == nil {
			out.Deletion = &wire.MemoryDeletionState{OperationId: q.OperationId, Ref: proto.Clone(q.Ref).(*wire.MemoryRef), Purpose: q.Purpose, State: result.State}
			if result.Report != nil {
				out.Deletion.Report = memorywire.DeletionReport(*result.Report)
			}
		}
	case "DELETE":
		deletion, ok := b.writes.(Deletes)
		if !ok {
			e = memory.Unavailable
			break
		}
		var receipt memory.Receipt
		receipt, e = deletion.Delete(ctx, b.identity, memory.DeleteRequest{OperationID: in.Delete.OperationId, Ref: in.Delete.Ref, ExpectedRevision: in.Delete.ExpectedRevision, Purpose: in.Delete.Purpose})
		if e == nil {
			out.Receipt = memorywire.Receipt(receipt)
		}
	case "QUERY":
		out.Result, e = b.reads.Query(ctx, b.identity, in.Query, in.GrantMaterial)
	case "GET":
		out.Result, e = b.reads.Get(ctx, b.identity, in.Get, in.GrantMaterial)
	case "LOOKUP":
		var status memory.OperationState
		status, e = b.writes.InspectOperation(ctx, b.identity, in.OperationId)
		if e == nil {
			out.Operation = &wire.MemoryOperationState{State: status.State, ContentAvailability: status.ContentAvailability}
			if status.Receipt != nil {
				out.Operation.Receipt = memorywire.Receipt(*status.Receipt)
			}
		}
	}
	if e != nil {
		out.Receipt = nil
		out.Result = nil
		out.Operation = nil
		out.Deletion = nil
		out.Code = memorywire.ErrorCode(e)
	}
	if proto.Size(out) > memorywire.MaxResponse {
		return nil, memory.Unavailable
	}
	return proto.Marshal(out)
}
