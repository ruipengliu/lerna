package harness

import (
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
)

const maxReplyJournalRecords = 128

type ReplyEntry struct {
	IdentityScope string              `json:"identity_scope"`
	EndpointID    string              `json:"endpoint_id"`
	InstanceID    string              `json:"instance_id"`
	Generation    uint64              `json:"generation"`
	Delivery      grpcwire.Delivery   `json:"delivery"`
	Reply         grpcwire.Reply      `json:"reply"`
	ReplyDigest   string              `json:"reply_digest"`
	Ack           *grpcwire.ReplyAck  `json:"ack,omitempty"`
	Invocation    *EndpointInvocation `json:"invocation,omitempty"`
}
type ReplyJournal struct {
	root                      *os.Root
	mu                        sync.Mutex
	scope, endpoint, instance string
	generation                uint64
}

func OpenReplyJournal(path, identityScope, endpoint, instance string, generation uint64) (*ReplyJournal, error) {
	if identityScope == "" || !api.ValidID(endpoint) || !api.ValidID(instance) || generation == 0 {
		return nil, api.E("invalid_request", "invalid_reply_journal_scope")
	}
	if e := os.MkdirAll(path, 0700); e != nil {
		return nil, e
	}
	root, e := os.OpenRoot(path)
	if e != nil {
		return nil, e
	}
	return &ReplyJournal{root: root, scope: identityScope, endpoint: endpoint, instance: instance, generation: generation}, nil
}
func (j *ReplyJournal) Close() error { j.mu.Lock(); defer j.mu.Unlock(); return j.root.Close() }
func (j *ReplyJournal) read(id string) (ReplyEntry, error) {
	var entry ReplyEntry
	if !api.ValidID(id) {
		return entry, api.E("invalid_request", "invalid_delivery_id")
	}
	f, e := j.root.Open(id + ".json")
	if e != nil {
		return entry, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, grpcwire.MaxFrameBytes+1))
	if e != nil {
		return entry, e
	}
	if e = api.DecodeLimit(b, &entry, grpcwire.MaxFrameBytes); e != nil {
		return entry, e
	}
	if entry.IdentityScope != j.scope || entry.EndpointID != j.endpoint || entry.InstanceID != j.instance || entry.Generation != j.generation || entry.Delivery.DeliveryID != id {
		return entry, api.E("forbidden", "reply_journal_identity_changed")
	}
	if _, e = grpcwire.DecodeFrame(api.Raw(entry.Delivery)); e != nil {
		return entry, e
	}
	if entry.Invocation != nil {
		inv := entry.Invocation
		if inv.Sequence == 0 || inv.Sequence > api.MaxSafeInteger || inv.Phase != "prepared" && inv.Phase != "started" || !api.ValidID(inv.IssuerServiceID) || !api.ValidID(inv.RecipientServiceID) || inv.IssuerServiceID != entry.Delivery.SenderServiceID || inv.Protocol != api.Protocol || inv.Profile == "" || len(inv.SchemaDigest) != 71 || len(inv.MethodSchemaDigest) != 71 || inv.IdentityRevision == 0 {
			return entry, api.E("invalid_request", "invalid_endpoint_invocation")
		}
		if entry.ReplyDigest == "" {
			if !api.Equal(entry.Reply, grpcwire.Reply{}) || entry.Ack != nil {
				return entry, api.E("invalid_request", "unfinished_endpoint_invocation_has_reply")
			}
			return entry, nil
		}
		if inv.Phase != "started" {
			return entry, api.E("invalid_request", "endpoint_reply_without_started_invocation")
		}
	}
	if _, e = grpcwire.DecodeFrame(api.Raw(entry.Reply)); e != nil {
		return entry, e
	}
	digest, e := api.Digest(entry.Reply)
	if e != nil || digest != entry.ReplyDigest {
		return entry, api.E("invalid_request", "reply_journal_digest_mismatch")
	}
	if e = grpcwire.ValidateReply(entry.Delivery, entry.Reply); e != nil {
		return entry, e
	}
	if entry.Ack != nil && (entry.Ack.DeliveryID != id || entry.Ack.RequestDigest != entry.Reply.RequestDigest || !entry.Ack.Stored) {
		return entry, api.E("invalid_request", "reply_journal_ack_mismatch")
	}
	return entry, nil
}
func (j *ReplyJournal) names() ([]string, error) {
	dir, e := j.root.Open(".")
	if e != nil {
		return nil, e
	}
	defer dir.Close()
	entries, e := dir.ReadDir(maxReplyJournalRecords*2 + 1)
	if e != nil && !errors.Is(e, io.EOF) {
		return nil, e
	}
	if len(entries) > maxReplyJournalRecords*2 {
		return nil, api.E("overloaded", "reply_journal_directory_limit")
	}
	names := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			id := strings.TrimSuffix(entry.Name(), ".json")
			if !api.ValidID(id) {
				return nil, api.E("invalid_request", "invalid_reply_journal_file")
			}
			names = append(names, id)
		}
	}
	if len(names) > maxReplyJournalRecords {
		return nil, api.E("overloaded", "reply_journal_identity_limit")
	}
	sort.Strings(names)
	return names, nil
}
func (j *ReplyJournal) write(entry ReplyEntry) error {
	b := api.Raw(entry)
	if len(b) > grpcwire.MaxFrameBytes {
		return api.E("invalid_request", "reply_journal_entry_too_large")
	}
	name := entry.Delivery.DeliveryID + ".json"
	temp := entry.Delivery.DeliveryID + "." + api.NewID("write") + ".tmp"
	f, e := j.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer j.root.Remove(temp)
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	closed := f.Close()
	if e != nil {
		return e
	}
	if closed != nil {
		return closed
	}
	if e = j.root.Rename(temp, name); e != nil {
		return e
	}
	dir, e := j.root.Open(".")
	if e != nil {
		return e
	}
	return errors.Join(dir.Sync(), dir.Close())
}
func (j *ReplyJournal) Save(ctx context.Context, d grpcwire.Delivery, reply grpcwire.Reply) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if _, e := grpcwire.DecodeFrame(api.Raw(d)); e != nil {
		return e
	}
	if _, e := grpcwire.DecodeFrame(api.Raw(reply)); e != nil {
		return e
	}
	if d.RecipientEndpointID != j.endpoint || d.RecipientInstanceID != j.instance {
		return api.E("forbidden", "reply_journal_endpoint_mismatch")
	}
	if e := grpcwire.ValidateReply(d, reply); e != nil {
		return e
	}
	digest, e := api.Digest(reply)
	if e != nil {
		return e
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	old, e := j.read(d.DeliveryID)
	if e == nil {
		if old.Invocation != nil && old.ReplyDigest == "" {
			if old.Invocation.Phase != "started" || !api.Equal(old.Delivery, d) {
				return api.E("invalid_state", "endpoint_invocation_not_started")
			}
			old.Reply, old.ReplyDigest = reply, digest
			return j.write(old)
		}
		if old.ReplyDigest != digest || !api.Equal(old.Delivery, d) {
			return api.E("idempotency_conflict", "original_reply_changed")
		}
		return nil
	}
	if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	names, e := j.names()
	if e != nil {
		return e
	}
	if len(names) >= maxReplyJournalRecords {
		return api.E("overloaded", "reply_journal_identity_limit")
	}
	return j.write(ReplyEntry{IdentityScope: j.scope, EndpointID: j.endpoint, InstanceID: j.instance, Generation: j.generation, Delivery: d, Reply: reply, ReplyDigest: digest})
}
func (j *ReplyJournal) Acknowledge(ctx context.Context, ack grpcwire.ReplyAck) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if _, e := grpcwire.DecodeFrame(api.Raw(ack)); e != nil {
		return e
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, e := j.read(ack.DeliveryID)
	if e != nil {
		return e
	}
	if ack.RequestDigest != entry.Reply.RequestDigest {
		return api.E("idempotency_conflict", "reply_ack_digest_mismatch")
	}
	if entry.Ack != nil {
		if !api.Equal(*entry.Ack, ack) {
			return api.E("idempotency_conflict", "reply_ack_changed")
		}
		return nil
	}
	entry.Ack = &ack
	return j.write(entry)
}
func (j *ReplyJournal) Pending(ctx context.Context, limit int) ([]ReplyEntry, bool, error) {
	if e := ctx.Err(); e != nil {
		return nil, false, e
	}
	if limit < 1 || limit > maxReplyJournalRecords {
		return nil, false, api.E("invalid_request", "invalid_reply_page_limit")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	names, e := j.names()
	if e != nil {
		return nil, false, e
	}
	out := []ReplyEntry{}
	partial := false
	for _, name := range names {
		if e = ctx.Err(); e != nil {
			return nil, false, e
		}
		entry, e := j.read(name)
		if e != nil {
			return nil, false, e
		}
		if entry.Ack != nil {
			continue
		}
		if entry.ReplyDigest == "" {
			continue
		}
		if len(out) >= limit {
			partial = true
			break
		}
		out = append(out, entry)
	}
	return out, partial, nil
}
