package contract

import (
	"context"
	"time"
)

// ReadAuthorizer checks a trusted, host-authenticated principal's permission
// to read the exact original command, without disclosing whether it exists.
type ReadAuthorizer interface {
	AuthorizeCommandRead(context.Context, SubjectBinding, CommandRef) (bool, error)
}

// ResolvedCommandOwner is a host-injected read route for one logical owner.
// Replacing Reader may replace a process, never the owner or command identity.
type ResolvedCommandOwner struct {
	Owner  OwnerRef
	Reader CommandFactReader
}

// OwnerDirectory resolves only the requested logical owner; there is no
// default-owner fallback. Production transport adapters belong outside contract.
type OwnerDirectory interface {
	ResolveCommandOwner(context.Context, OwnerRef) (ResolvedCommandOwner, error)
}

// GetCommand is the authenticated Application / Component command.get entry.
// trusted must come from the host's authenticated context, never the payload.
// ctx must have a finite resource deadline and ports must honor cancellation.
// That deadline is independent of accept_before, which gates only read start.
func GetCommand(ctx context.Context, data []byte, trusted *SubjectBinding, authorizer ReadAuthorizer, directory OwnerDirectory, now func() time.Time) (CommandGetResponse, error) {
	request, err := DecodeCommand(data)
	if err != nil {
		return CommandGetResponse{}, err
	}
	// Freeze the validated request before any injected callback may run.
	data, err = Encode(request)
	if err != nil {
		return CommandGetResponse{}, refusal("schema_invalid", err)
	}
	original := request.Payload.CommandRef
	rejected := func(reason ErrorCode) CommandGetResponse {
		return NewCommandGetResponseRejected(CommandGetResponseRejected{Reason: reason})
	}
	unavailable := func() CommandGetResponse {
		return NewCommandGetResponseUnavailable(CommandGetResponseUnavailable{CommandRef: original, Reason: "dependency_unavailable"})
	}
	if trusted == nil || authorizer == nil {
		return rejected("forbidden"), nil
	}
	// Snapshot and validate before exposing the binding to injected code. In
	// particular, the delegation slice must not alias host authentication state.
	encoded, err := Encode(*trusted)
	if err != nil {
		return rejected("forbidden"), nil
	}
	principal, err := Decode[SubjectBinding](encoded)
	if err != nil || principal.TenantID != original.Owner.TenantID {
		return rejected("forbidden"), nil
	}
	if now == nil {
		return unavailable(), nil
	}
	started := now()
	cutoff, err := time.Parse("2006-01-02T15:04:05.000000Z", string(request.AcceptBefore))
	if err != nil {
		return CommandGetResponse{}, refusal("schema_invalid", err)
	}
	if !started.Before(cutoff) {
		return rejected("expired"), nil
	}
	if ctx == nil {
		return unavailable(), nil
	}
	if _, finite := ctx.Deadline(); !finite || ctx.Err() != nil {
		return unavailable(), nil
	}
	allowed, err := authorizer.AuthorizeCommandRead(ctx, principal, original)
	if ctx.Err() != nil {
		return unavailable(), nil
	}
	if err != nil || !allowed {
		return rejected("forbidden"), nil
	}
	if ctx.Err() != nil || directory == nil {
		return unavailable(), nil
	}
	route, err := directory.ResolveCommandOwner(ctx, original.Owner)
	if err != nil || ctx.Err() != nil || route.Owner != original.Owner || route.Reader == nil {
		return unavailable(), nil
	}
	return ReadCommandFacts(ctx, data, route.Reader, func() time.Time { return started })
}
