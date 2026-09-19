package cleanup

import (
	"context"
	"lerna/authorization"
	"lerna/memory"
)

type AdmissionStore interface {
	EraseMemoryComparisons(context.Context, string, []authorization.MemoryComparison) error
}

type Admissions struct {
	memory     memory.ErasedOperationStore
	admissions AdmissionStore
}

func NewAdmissions(m memory.ErasedOperationStore, a AdmissionStore) (*Admissions, error) {
	if m == nil || a == nil {
		return nil, memory.Invalid
	}
	return &Admissions{m, a}, nil
}

func (a *Admissions) Apply(ctx context.Context, event memory.SourceEvent) (bool, error) {
	if !memory.ValidSourceEvent(event) {
		return false, memory.Invalid
	}
	if event.Kind == memory.SourceErased {
		proof, ok := a.memory.(memory.ErasedRevisionStore)
		if !ok {
			return false, memory.Unavailable
		}
		receipt, err := proof.ErasedRevisionOperation(ctx, event)
		if err != nil {
			return false, err
		}
		if receipt.Ref != event.Ref || receipt.Revision != event.Revision || receipt.OperationID == "" || receipt.Subject == "" || receipt.SemanticSHA256 != "" || receipt.Position == 0 || receipt.Position >= event.Position {
			return false, memory.Unavailable
		}
		err = a.admissions.EraseMemoryComparisons(ctx, event.Ref.Namespace, []authorization.MemoryComparison{{OperationID: receipt.OperationID, Subject: receipt.Subject}})
		return err == nil, err
	}
	if event.Kind != memory.SourceDeleted {
		return true, nil
	}
	receipts, err := a.memory.ErasedOperations(ctx, event.Ref, event.Revision)
	if err != nil {
		return false, err
	}
	if len(receipts) == 0 || len(receipts) > 512 || uint64(len(receipts)) != event.Revision-1 {
		return false, memory.Unavailable
	}
	entries := make([]authorization.MemoryComparison, 0, len(receipts))
	for i, r := range receipts {
		if r.Ref != event.Ref || r.Revision != uint64(i+1) || r.SemanticSHA256 != "" || r.Position == 0 || r.Position >= event.Position {
			return false, memory.Unavailable
		}
		entries = append(entries, authorization.MemoryComparison{OperationID: r.OperationID, Subject: r.Subject})
	}
	if err = a.admissions.EraseMemoryComparisons(ctx, event.Ref.Namespace, entries); err != nil {
		return false, err
	}
	return true, nil
}

var _ memory.SourceSink = (*Admissions)(nil)
