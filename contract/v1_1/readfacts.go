package v1_1

import (
	"context"
	"time"
)

// CommandFactReader supplies existing observations only. It has no write,
// admission, Job, model or action API. Production callers must authorize the
// exact reference and resolve its fixed owner before invoking this primitive.
type CommandFactReader interface {
	ReadCommand(context.Context, CommandRef) (CommandGetResponse, error)
}

// ReadCommandFacts is a lower-level, unauthenticated fact-reading primitive,
// not a business entry point. It validates the read request and observations.
// accept_before belongs to this read, never the original write's lifetime.
func ReadCommandFacts(ctx context.Context, data []byte, reader CommandFactReader, now func() time.Time) (CommandGetResponse, error) {
	request, err := DecodeCommand(data)
	if err != nil {
		return CommandGetResponse{}, err
	}
	unavailable := func() CommandGetResponse {
		return NewCommandGetResponseUnavailable(CommandGetResponseUnavailable{CommandRef: request.Payload.CommandRef, Reason: "dependency_unavailable"})
	}
	if now == nil || reader == nil {
		return unavailable(), nil
	}
	cutoff, err := time.Parse("2006-01-02T15:04:05.000000Z", string(request.AcceptBefore))
	if err != nil {
		return CommandGetResponse{}, refusal("schema_invalid", err)
	}
	if !now().Before(cutoff) {
		return NewCommandGetResponseRejected(CommandGetResponseRejected{Reason: "expired"}), nil
	}
	if ctx.Err() != nil {
		return unavailable(), nil
	}
	result, err := reader.ReadCommand(ctx, request.Payload.CommandRef)
	if err != nil {
		return unavailable(), nil
	}
	if ctx.Err() != nil {
		return unavailable(), nil
	}
	encoded, err := EncodeCommandResponse(result, request.Payload.CommandRef)
	if err != nil {
		return unavailable(), nil
	}
	// Return an independent wire observation; callers cannot mutate pointers
	// retained by an injected fact reader through the returned receipt.
	return DecodeCommandResponse(encoded, request.Payload.CommandRef)
}
