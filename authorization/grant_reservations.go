package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"time"
)

// UsePermit is a durable allocation, not proof that a business action executed.
// A consumer must atomically bind it to its own operation/start record before use.
type UsePermit struct {
	Namespace, GrantID, Subject, Audience, Presenter, CertificateSHA256, OperationID, SemanticSHA256, ActionSHA256 string
	Units                                                                                                          uint64
}

func (p UsePermit) matches(other GrantPresentation) bool {
	return p.Namespace == other.Namespace && p.Subject == other.Subject && p.Audience == other.Audience && p.Presenter == other.Presenter && p.CertificateSHA256 == other.CertificateSHA256 && p.OperationID == other.OperationID && p.SemanticSHA256 == other.SemanticSHA256
}
func digestValid(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func (g *GrantAuthority) ReserveUse(ctx context.Context, material string, p GrantPresentation, action *wire.AuthorizationAction, units uint64) (UsePermit, error) {
	ctx, cancelCall := context.WithTimeout(ctx, g.config.IOTimeout)
	defer cancelCall()
	if len(p.OperationID) == 0 || len(p.OperationID) > 512 || !digestValid(p.SemanticSHA256) || units == 0 || units > 1000000 {
		return UsePermit{}, fail(Invalid)
	}
	record, err := g.Verify(ctx, material, p, action)
	if err != nil {
		return UsePermit{}, err
	}
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(action)
	if err != nil {
		return UsePermit{}, fail(Invalid)
	}
	digest := sha256.Sum256(encoded)
	in := UsePermit{p.Namespace, record.Id, p.Subject, p.Audience, p.Presenter, p.CertificateSHA256, p.OperationID, p.SemanticSHA256, hex.EncodeToString(digest[:]), units}
	var out UsePermit
	err = g.service.update(ctx, func(st *State, now time.Time) error {
		if err := g.journal(st); err != nil {
			return err
		}
		if err := g.chain(st, record.Id, now); err != nil {
			return err
		}
		// Shared namespace operation identity: a changed grant/receiver cannot spend
		// the same logical operation again. Management identities remain separate.
		if _, ok := st.Operations[p.OperationID]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := st.MemoryOperations[p.OperationID]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := st.ContentOperations[p.OperationID]; ok {
			return fail(IdentityConflict)
		}
		if subject, ok := st.ExecutionOperations[p.OperationID]; ok {
			if subject != p.Subject {
				return fail(Denied)
			}
			if _, allocated := st.Signed.Uses[p.OperationID]; !allocated && !st.ExecutionReservations[p.OperationID] {
				return fail(IdentityConflict)
			}
		}
		if _, ok := st.RuntimeOperations[p.OperationID]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := st.Signed.Operations[p.OperationID]; ok {
			return fail(IdentityConflict)
		}
		if old, ok := st.Signed.Uses[p.OperationID]; ok {
			if old != in {
				return fail(IdentityConflict)
			}
			out = old
			return nil
		}
		if len(st.Signed.Uses) >= g.config.MaxRecords {
			return fail(Unavailable)
		}
		e := st.Signed.Grants[record.Id]
		if e.Record.Revision != record.Revision || e.Record.Allocated > record.Spec.Units || units > record.Spec.Units-e.Record.Allocated {
			return fail(Denied)
		}
		if record.Spec.Mode == "single" && (units != 1 || p.OperationID != record.Spec.OperationBinding || p.SemanticSHA256 != record.Spec.SemanticSha256) {
			return fail(Denied)
		}
		e.Record.Allocated += units
		st.Signed.Grants[record.Id] = e
		st.Signed.Uses[p.OperationID] = in
		out = in
		return nil
	})
	if err != nil {
		return UsePermit{}, err
	}
	return out, nil
}

// LookupUse recovers an existing allocation even after revocation. It does not
// permit a new action; the consumer must revalidate before its first start.
func (g *GrantAuthority) LookupUse(ctx context.Context, p GrantPresentation) (UsePermit, error) {
	ctx, cancelCall := context.WithTimeout(ctx, g.config.IOTimeout)
	defer cancelCall()
	var out UsePermit
	err := g.service.update(ctx, func(st *State, now time.Time) error {
		if err := g.journal(st); err != nil {
			return err
		}
		old, ok := st.Signed.Uses[p.OperationID]
		if !ok {
			return fail(NotFound)
		}
		if !old.matches(p) {
			return fail(Denied)
		}
		out = old
		return nil
	})
	if err != nil {
		return UsePermit{}, err
	}
	return out, nil
}
