package harness

import (
	"context"
	"errors"
	"os"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
)

// Invocation只读原责任；不更新原输入、期限或执行阶段。
func (j *ReplyJournal) Invocation(ctx context.Context, id string) (ReplyEntry, error) {
	if err := ctx.Err(); err != nil {
		return ReplyEntry{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.read(id)
}

// PrepareInvocation在任何接收handler执行前fsync原输入与单调本机sequence。
func (j *ReplyJournal) PrepareInvocation(ctx context.Context, d grpcwire.Delivery, inv EndpointInvocation) (ReplyEntry, error) {
	if err := ctx.Err(); err != nil {
		return ReplyEntry{}, err
	}
	if _, err := grpcwire.DecodeFrame(api.Raw(d)); err != nil {
		return ReplyEntry{}, err
	}
	if d.RecipientEndpointID != j.endpoint || d.RecipientInstanceID != j.instance || inv.IssuerServiceID != d.SenderServiceID || !api.ValidID(inv.RecipientServiceID) || inv.Protocol != api.Protocol || inv.Profile == "" || len(inv.SchemaDigest) != 71 || len(inv.MethodSchemaDigest) != 71 || inv.IdentityRevision == 0 {
		return ReplyEntry{}, api.E("forbidden", "endpoint_invocation_scope_mismatch")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	old, err := j.read(d.DeliveryID)
	if err == nil {
		if !api.Equal(old.Delivery, d) || old.Invocation == nil {
			return old, api.E("idempotency_conflict", "endpoint_invocation_changed")
		}
		comparison := inv
		comparison.Sequence, comparison.Phase = old.Invocation.Sequence, old.Invocation.Phase
		if !api.Equal(comparison, *old.Invocation) {
			return old, api.E("idempotency_conflict", "endpoint_invocation_binding_changed")
		}
		return old, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return old, err
	}
	names, err := j.names()
	if err != nil || len(names) >= maxReplyJournalRecords {
		if err == nil {
			err = api.E("overloaded", "reply_journal_identity_limit")
		}
		return ReplyEntry{}, err
	}
	var last uint64
	for _, name := range names {
		entry, err := j.read(name)
		if err != nil {
			return ReplyEntry{}, err
		}
		if entry.Invocation != nil && entry.Invocation.Sequence > last {
			last = entry.Invocation.Sequence
		}
	}
	if last >= api.MaxSafeInteger {
		return ReplyEntry{}, api.E("overloaded", "endpoint_sequence_exhausted")
	}
	inv.Sequence, inv.Phase = last+1, "prepared"
	entry := ReplyEntry{IdentityScope: j.scope, EndpointID: j.endpoint, InstanceID: j.instance, Generation: j.generation, Delivery: d, Invocation: &inv}
	return entry, j.write(entry)
}
func (j *ReplyJournal) StartInvocation(ctx context.Context, id string) (ReplyEntry, error) {
	if err := ctx.Err(); err != nil {
		return ReplyEntry{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, err := j.read(id)
	if err != nil {
		return entry, err
	}
	if entry.Invocation == nil {
		return entry, api.E("invalid_state", "endpoint_invocation_missing")
	}
	if entry.Invocation.Phase == "started" {
		return entry, nil
	}
	entry.Invocation.Phase = "started"
	return entry, j.write(entry)
}

// Incomplete包含未知执行及已存未Ack的Reply；旧started只Lookup，不能再Invoke。
func (j *ReplyJournal) Incomplete(ctx context.Context, limit int) ([]ReplyEntry, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if limit < 1 || limit > maxReplyJournalRecords {
		return nil, false, api.E("invalid_request", "invalid_reply_page_limit")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	names, err := j.names()
	if err != nil {
		return nil, false, err
	}
	out := []ReplyEntry{}
	for _, name := range names {
		entry, err := j.read(name)
		if err != nil {
			return nil, false, err
		}
		if entry.Ack != nil {
			continue
		}
		if len(out) == limit {
			return out, true, nil
		}
		out = append(out, entry)
	}
	return out, false, nil
}
