package content

import (
	"context"
	contentlocal "lerna/adapters/content/local"
	contentpolicy "lerna/adapters/content/policy"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"time"
)

var lifecycleNames = []string{"read-policy-race", "cleanup-unavailable", "delete-version-conflict", "retention-limit", "processing-location", "hidden-existence", "capacity-reserves-deletion", "cancelled-call"}

func lifecycle(ctx context.Context, h *harness, name string) error {
	if name == "capacity-reserves-deletion" {
		h.config.MaxRecords = 1
		h.bind(h.auth, h.blob)
	}
	out, in, err := h.put(ctx, []byte("abcdefghijklmnopqrstuvwxyz"))
	if err != nil {
		return err
	}
	switch name {
	case "read-policy-race":
		h.bind(h.auth, &readHook{Blobs: h.blob, after: func() { h.policy.Replace(nil) }})
		_, err = h.read(ctx, out.Record.Ref, 5)
		return demand(artifacts.Code(err) == "PERMISSION_DENIED")
	case "cleanup-unavailable":
		if _, err = h.delete(ctx, out.Record.Ref); err != nil {
			return err
		}
		h.bind(h.auth, &unavailableRemove{h.blob})
		if err = h.service.Clean(ctx); err == nil {
			return artifacts.Error("CHECK_FAILED")
		}
		r, err := h.client.Call(ctx, &wire.ContentRequest{Method: "GET", Ref: out.Record.Ref, Purpose: "research"})
		if err != nil {
			return err
		}
		if r.Record.State != "cleaning" {
			return artifacts.Error("CHECK_FAILED")
		}
		h.bind(h.auth, h.blob)
		if err = h.service.Clean(ctx); err != nil {
			return err
		}
		r, err = h.client.Call(ctx, &wire.ContentRequest{Method: "GET", Ref: out.Record.Ref, Purpose: "research"})
		if err != nil {
			return err
		}
		return demand(r.Record.State == "cleaned")
	case "delete-version-conflict":
		op, err := h.auth.NewOperation(ctx, h.token)
		if err != nil {
			return err
		}
		_, err = h.client.Call(ctx, &wire.ContentRequest{Method: "DELETE", OperationId: op, Ref: out.Record.Ref, Purpose: "research", ExpectedRevision: 2})
		if artifacts.Code(err) != "VERSION_CONFLICT" {
			return artifacts.Error("CHECK_FAILED")
		}
		_, err = h.read(ctx, out.Record.Ref, 5)
		return err
	case "retention-limit":
		r := h.rule
		r.RetainUntil = in.Spec.RetainUntil - 1
		h.policy.Replace([]contentpolicy.Rule{r})
		_, _, err = h.put(ctx, []byte("again"))
		return demand(artifacts.Code(err) == "PERMISSION_DENIED")
	case "processing-location":
		b := h.binding
		b.Location = "cloud"
		client := sdk.NewContentClient(contentlocal.Bind(h.service, b), "local")
		_, err = client.Call(ctx, &wire.ContentRequest{Method: "READ", Ref: out.Record.Ref, Purpose: "research", Limit: 5})
		return demand(artifacts.Code(err) == "PERMISSION_DENIED")
	case "hidden-existence":
		h.policy.Replace(nil)
		_, a := h.client.Call(ctx, &wire.ContentRequest{Method: "GET", Ref: out.Record.Ref, Purpose: "research"})
		_, b := h.client.Call(ctx, &wire.ContentRequest{Method: "GET", Ref: &wire.ContentRef{Namespace: "local", Key: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", Revision: 1}, Purpose: "research"})
		return demand(artifacts.Code(a) == "PERMISSION_DENIED" && artifacts.Code(b) == "PERMISSION_DENIED")
	case "capacity-reserves-deletion":
		_, _, err = h.put(ctx, []byte("again"))
		if artifacts.Code(err) != "CAPACITY_EXCEEDED" {
			return artifacts.Error("CHECK_FAILED")
		}
		_, err = h.delete(ctx, out.Record.Ref)
		if err != nil {
			return err
		}
		return h.service.Clean(ctx)
	case "cancelled-call":
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()
		start := time.Now()
		_, err = h.read(cancelCtx, out.Record.Ref, 5)
		return demand(err != nil && time.Since(start) < time.Second)
	}
	return artifacts.Error("UNKNOWN_CHECK")
}

type readHook struct {
	artifacts.Blobs
	after func()
}

func (b *readHook) Read(ctx context.Context, k string, o uint64, n uint32, size uint64, sha string) ([]byte, error) {
	out, err := b.Blobs.Read(ctx, k, o, n, size, sha)
	if err == nil {
		b.after()
	}
	return out, err
}

type unavailableRemove struct{ artifacts.Blobs }

func (b *unavailableRemove) Remove(context.Context, string) error {
	return artifacts.Error("UNAVAILABLE")
}
