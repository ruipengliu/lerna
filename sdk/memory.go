package sdk

import (
	"context"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/internal/memorywire"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"lerna/memory"
)

type MemoryClient struct {
	transport Transport
	namespace string
}

func NewMemoryClient(transport Transport, namespace string) *MemoryClient {
	return &MemoryClient{transport, namespace}
}

// Exchange keeps operation/read identity while creating a new message attempt.
// Grant presentation and host identity are separate from the mutable payload.
func (c *MemoryClient) Exchange(ctx context.Context, request *wire.MemoryRequest) (*wire.MemoryResponse, error) {
	if request == nil || c.transport == nil {
		return nil, memory.Invalid
	}
	in := proto.Clone(request).(*wire.MemoryRequest)
	id, e := randomid.New()
	if e != nil {
		return nil, memory.Unavailable
	}
	in.MessageId = id
	in.Namespace = c.namespace
	if !memorywire.Request(in) {
		return nil, memory.Invalid
	}
	data, e := proto.Marshal(in)
	if e != nil {
		return nil, memory.Invalid
	}
	data, e = c.transport.Exchange(ctx, data)
	if e != nil {
		return nil, e
	}
	out := new(wire.MemoryResponse)
	if len(data) > memorywire.MaxResponse || proto.Unmarshal(data, out) != nil || !taskwire.Known(out.ProtoReflect()) || out.Namespace != c.namespace || out.ReplyTo != id || out.MessageId == "" || out.MessageId == id || len(out.MessageId) > 128 {
		return nil, memory.Invalid
	}
	if out.Code != "OK" {
		if out.Receipt != nil || out.Operation != nil || out.Result != nil || out.Deletion != nil {
			return nil, memory.Invalid
		}
		return nil, memorywire.Failure(out.Code)
	}
	if in.Method != "DELETION_STATUS" && out.Deletion != nil {
		return nil, memory.Invalid
	}
	validReceipt := func(r *wire.MemoryReceipt) bool {
		return r != nil && r.Ref != nil && r.Ref.Namespace == c.namespace && r.Ref.Collection != "" && r.Ref.Key != "" && r.Revision > 0 && r.ChangePosition > 0 && r.Subject != ""
	}
	switch in.Method {
	case "PUT", "CORRECT":
		if out.Result != nil || out.Operation != nil || !validReceipt(out.Receipt) || len(out.Receipt.SemanticSha256) != 64 || out.Receipt.OperationId != in.Write.OperationId || !proto.Equal(out.Receipt.Ref, in.Write.Ref) || out.Receipt.Revision != in.Write.ExpectedRevision+1 {
			return nil, memory.Invalid
		}
	case "DELETION_STATUS":
		if out.Result != nil || out.Operation != nil || out.Receipt != nil || !memorywire.ValidDeletionState(in.DeletionQuery, out.Deletion) {
			return nil, memory.Invalid
		}
	case "DELETE":
		if out.Result != nil || out.Operation != nil || !validReceipt(out.Receipt) || len(out.Receipt.SemanticSha256) != 64 || out.Receipt.OperationId != in.Delete.OperationId || !proto.Equal(out.Receipt.Ref, in.Delete.Ref) || out.Receipt.Revision != in.Delete.ExpectedRevision+1 {
			return nil, memory.Invalid
		}
	case "LOOKUP":
		if out.Result != nil || out.Receipt != nil || out.Operation == nil {
			return nil, memory.Invalid
		}
		switch out.Operation.State {
		case "committed":
			if (out.Operation.ContentAvailability != "available" && out.Operation.ContentAvailability != "unavailable") || !validReceipt(out.Operation.Receipt) || (len(out.Operation.Receipt.SemanticSha256) != 64 && !(out.Operation.ContentAvailability == "unavailable" && out.Operation.Receipt.SemanticSha256 == "")) || out.Operation.Receipt.OperationId != in.OperationId {
				return nil, memory.Invalid
			}
		case "not_admitted", "admission_expired", "unknown":
			if out.Operation.Receipt != nil || out.Operation.ContentAvailability != "" {
				return nil, memory.Invalid
			}
		default:
			return nil, memory.Invalid
		}
	case "GET", "QUERY":
		if out.Receipt != nil || out.Operation != nil || out.Result == nil || len(out.Result.Records) > 32 {
			return nil, memory.Invalid
		}
		switch out.Result.Coverage {
		case "complete", "partial_unavailable", "budget_exhausted", "partial_and_budget_exhausted":
		default:
			return nil, memory.Invalid
		}
		if in.Method == "GET" && (len(out.Result.Records) != 1 || out.Result.Coverage != "complete") {
			return nil, memory.Invalid
		}
		if in.Method == "QUERY" && (len(out.Result.Records) > int(in.Query.MaxResults) || proto.Size(&wire.MemoryReadResult{Records: out.Result.Records}) > int(in.Query.MaxBytes)) {
			return nil, memory.Invalid
		}
		seen := map[string]bool{}
		for _, r := range out.Result.Records {
			if r.Ref == nil || r.Spec == nil || r.Ref.Namespace != c.namespace || r.Ref.Key == "" || r.Revision == 0 || r.PreviousRevision != r.Revision-1 || r.OperationId == "" {
				return nil, memory.Invalid
			}
			key := r.Ref.Collection + "\x00" + r.Ref.Key
			if seen[key] {
				return nil, memory.Invalid
			}
			seen[key] = true
			if in.Method == "GET" {
				if !proto.Equal(r.Ref, in.Get.Ref) || r.Revision != in.Get.Revision || r.Spec.Purpose != in.Get.Purpose {
					return nil, memory.Invalid
				}
			} else if r.Ref.Collection != in.Query.Collection || r.Spec.Purpose != in.Query.Purpose {
				return nil, memory.Invalid
			}
		}
	}
	return out, nil
}
