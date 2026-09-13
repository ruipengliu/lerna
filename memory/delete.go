package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"time"
)

// DeleteRequest names one record and the revision the caller intends to delete.
// It carries no replacement body or caller-selected authorization policy.
type DeleteRequest struct {
	OperationID      string
	Ref              *wire.MemoryRef
	ExpectedRevision uint64
	Purpose          string
}

// DeletionAuthority authorizes destructive metadata operations independently of
// permission to process or retain the old body, which may already be expired.
type DeletionAuthority interface {
	CheckDeletion(context.Context, Binding, *wire.MemoryRef, string) error
	AdmitDeletion(context.Context, Binding, string, string, *wire.MemoryRef, string) (int64, error)
}

func (s *Service) Delete(ctx context.Context, b Binding, request DeleteRequest) (Receipt, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if !s.binding(b) {
		return Receipt{}, Denied
	}
	if !validReference(request.Ref) || !text(request.OperationID, 256) || !text(request.Purpose, 256) || request.ExpectedRevision == 0 || request.ExpectedRevision >= 1<<32 {
		return Receipt{}, Invalid
	}
	in := request
	in.Ref = proto.Clone(request.Ref).(*wire.MemoryRef)
	authority, ok := s.authority.(DeletionAuthority)
	if !ok {
		return Receipt{}, Unavailable
	}
	if err := authority.CheckDeletion(ctx, b, in.Ref, in.Purpose); err != nil {
		return Receipt{}, err
	}
	store, ok := s.store.(DeletionStore)
	if !ok {
		return Receipt{}, Unavailable
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return Receipt{}, Invalid
	}
	hash := sha256.Sum256(append([]byte("delete:"), raw...))
	digest := hex.EncodeToString(hash[:])
	_, err = store.LookupOperation(ctx, b.Namespace, in.OperationID)
	if err != nil && err != Missing {
		return Receipt{}, err
	}
	if err == Missing {
		expires, err := authority.AdmitDeletion(ctx, b, in.OperationID, digest, in.Ref, in.Purpose)
		if err != nil {
			return Receipt{}, err
		}
		now, err := s.clock.Now()
		if err != nil {
			return Receipt{}, Unavailable
		}
		remaining := time.Unix(0, expires).Sub(now)
		if remaining <= 0 {
			return Receipt{}, AdmissionExpired
		}
		bounded, stop := context.WithTimeout(ctx, remaining)
		defer stop()
		ctx = bounded
	}
	if err = authority.CheckDeletion(ctx, b, in.Ref, in.Purpose); err != nil {
		return Receipt{}, err
	}
	receipt, err := store.Delete(ctx, Deletion{OperationID: in.OperationID, Subject: b.Subject, SemanticSHA256: digest, Ref: refOf(in.Ref), Expected: in.ExpectedRevision})
	if err != nil {
		return Receipt{}, err
	}
	// Current permission also governs the original result on replay.
	if err = authority.CheckDeletion(ctx, b, in.Ref, in.Purpose); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}
