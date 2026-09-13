package content

import (
	"bytes"
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/contentlocal"
	"lerna/adapters/contentpolicy"
	"lerna/artifacts"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var names = append(lifecycleNames, []string{"credential-existence", "metadata-availability", "legacy-upgrade", "inline-reopen", "large-chunks", "metadata-not-body", "read-not-store", "placement", "source-intersection", "semantic-replay", "delete-no-resurrection", "missing-corrupt", "bounded-input", "root-confinement", "expiry", "parallel-publication", "publication-policy-race", "cleanup-race", "isolated-partition", "cross-domain-operation", "sdk-invalid-wire", "source-stale", "unknown-publication"}...)

func check(ctx context.Context, name string) error {
	h, err := fresh(ctx)
	if err != nil {
		return err
	}
	defer h.destroy()
	for _, candidate := range lifecycleNames {
		if candidate == name {
			return lifecycle(ctx, h, name)
		}
	}
	switch name {
	case "credential-existence":
		return credentialExistence(ctx, h)
	case "metadata-availability":
		return metadataAvailability(ctx, h)
	case "legacy-upgrade":
		return legacy(ctx, h)
	case "inline-reopen":
		out, in, err := h.put(ctx, []byte("hello"))
		if err != nil {
			return err
		}
		other, err := open(h.root, h.token)
		if err != nil {
			return err
		}
		defer other.close()
		again, err := other.client.Call(ctx, in)
		if err != nil {
			return err
		}
		read, err := other.read(ctx, out.Record.Ref, 5)
		if err != nil {
			return err
		}
		return demand(proto.Equal(out.Record.Ref, again.Record.Ref) && string(read.Data) == "hello")
	case "large-chunks":
		data := bytes.Repeat([]byte("abcdef"), 20000)
		out, _, err := h.put(ctx, data)
		if err != nil {
			return err
		}
		got := []byte{}
		for offset := 0; offset < len(data); {
			n := min(32768, len(data)-offset)
			r, err := h.client.Call(ctx, &wire.ContentRequest{Method: "READ", Ref: out.Record.Ref, Purpose: "research", Offset: uint64(offset), Limit: uint32(n)})
			if err != nil {
				return err
			}
			got = append(got, r.Data...)
			offset += n
		}
		return demand(bytes.Equal(data, got))
	case "metadata-not-body", "read-not-store", "placement", "source-intersection", "source-stale":
		out, _, err := h.put(ctx, []byte("hello"))
		if err != nil {
			return err
		}
		switch name {
		case "metadata-not-body":
			h.rule.Actions = []string{"discover"}
			h.policy.Replace([]contentpolicy.Rule{h.rule})
			meta, e := h.client.Call(ctx, &wire.ContentRequest{Method: "GET", Ref: out.Record.Ref, Purpose: "research"})
			if e != nil || meta.Record == nil {
				return artifacts.Error("CHECK_FAILED")
			}
			_, err = h.read(ctx, out.Record.Ref, 5)
		case "read-not-store":
			h.rule.Actions = []string{"discover", "process", "disclose"}
			h.policy.Replace([]contentpolicy.Rule{h.rule})
			if _, e := h.read(ctx, out.Record.Ref, 5); e != nil {
				return e
			}
			_, _, err = h.put(ctx, []byte("again"))
		case "placement":
			b := h.binding
			b.Recipient = "cloud"
			client := sdk.NewContentClient(contentlocal.Bind(h.service, b), "local")
			_, err = client.Call(ctx, &wire.ContentRequest{Method: "READ", Ref: out.Record.Ref, Purpose: "research", Limit: 5})
		case "source-intersection":
			r := h.rule
			r.Key = "private"
			r.Actions = []string{"discover"}
			h.policy.Replace([]contentpolicy.Rule{h.rule, r})
			in, e := h.request(ctx, []byte("mixed"))
			if e != nil {
				return e
			}
			in.Spec.Sources = append(in.Spec.Sources, &wire.ContentSource{Kind: "input", Key: "private", Revision: 1})
			_, err = h.client.Call(ctx, in)
		case "source-stale":
			h.rule.Revision = 2
			h.policy.Replace([]contentpolicy.Rule{h.rule})
			_, err = h.read(ctx, out.Record.Ref, 5)
		}
		return demand(artifacts.Code(err) == "PERMISSION_DENIED")
	case "semantic-replay":
		out, in, err := h.put(ctx, []byte("hello"))
		if err != nil {
			return err
		}
		again, err := h.client.Call(ctx, in)
		if err != nil || !proto.Equal(out.Record.Ref, again.GetRecord().GetRef()) {
			return artifacts.Error("CHECK_FAILED")
		}
		in.Spec.MediaType = "text/other"
		_, err = h.client.Call(ctx, in)
		return demand(artifacts.Code(err) == "IDENTITY_CONFLICT")
	case "delete-no-resurrection":
		out, in, err := h.put(ctx, bytes.Repeat([]byte("s"), 100))
		if err != nil {
			return err
		}
		del, err := h.delete(ctx, out.Record.Ref)
		if err != nil {
			return err
		}
		if _, err = h.read(ctx, out.Record.Ref, 5); artifacts.Code(err) != "CONTENT_UNAVAILABLE" {
			return artifacts.Error("CHECK_FAILED")
		}
		if err = h.service.Clean(ctx); err != nil {
			return err
		}
		r, err := h.client.Call(ctx, &wire.ContentRequest{Method: "LOOKUP", OperationId: in.OperationId, Purpose: "research"})
		if err != nil {
			return err
		}
		if r.Record.State != "cleaned" || r.Record.Spec.Sha256 != "" {
			return artifacts.Error("CHECK_FAILED")
		}
		if _, err = h.client.Call(ctx, del); err != nil {
			return err
		}
		_, err = h.client.Call(ctx, in)
		return demand(artifacts.Code(err) == "CONTENT_UNAVAILABLE")
	case "missing-corrupt":
		out, _, err := h.put(ctx, bytes.Repeat([]byte("z"), 100))
		if err != nil {
			return err
		}
		path := filepath.Join(h.root, "content", out.Record.Ref.Key)
		if err = os.WriteFile(path, bytes.Repeat([]byte("x"), 100), 0600); err != nil {
			return err
		}
		_, err = h.read(ctx, out.Record.Ref, 5)
		if artifacts.Code(err) != "CONTENT_CORRUPT" {
			return artifacts.Error("CHECK_FAILED")
		}
		os.Remove(path)
		_, err = h.read(ctx, out.Record.Ref, 5)
		return demand(artifacts.Code(err) == "CONTENT_MISSING")
	case "bounded-input":
		out, _, err := h.put(ctx, []byte("hello"))
		if err != nil {
			return err
		}
		for _, in := range []*wire.ContentRequest{{Method: "READ", Ref: out.Record.Ref, Purpose: "research", Limit: 65537}, {Method: "READ", Ref: out.Record.Ref, Purpose: "research", Offset: 4, Limit: 2}} {
			if _, e := h.client.Call(ctx, in); artifacts.Code(e) != "INVALID_ARGUMENT" {
				return artifacts.Error("CHECK_FAILED")
			}
		}
		in, err := h.request(ctx, bytes.Repeat([]byte("a"), int(h.config.MaxObject)+1))
		if err != nil {
			return err
		}
		_, err = h.client.Call(ctx, in)
		return demand(artifacts.Code(err) == "INVALID_ARGUMENT")
	case "root-confinement":
		path := filepath.Join(h.root, "outside")
		os.WriteFile(path, []byte("private"), 0600)
		for _, k := range []string{path, "../outside", "https://host/private"} {
			if _, e := h.blob.Read(ctx, k, 0, 1, 7, ""); artifacts.Code(e) != "INVALID_ARGUMENT" {
				return artifacts.Error("CHECK_FAILED")
			}
		}
		k := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		os.Symlink(path, filepath.Join(h.root, "content", k))
		_, err = h.blob.Read(ctx, k, 0, 1, 7, "")
		return demand(err != nil)
	case "expiry":
		out, _, err := h.put(ctx, []byte("hello"))
		if err != nil {
			return err
		}
		h.clock.advance(time.Minute * 6)
		_, err = h.read(ctx, out.Record.Ref, 5)
		if artifacts.Code(err) != "CONTENT_UNAVAILABLE" {
			return artifacts.Error("CHECK_FAILED")
		}
		if err = h.service.Clean(ctx); err != nil {
			return err
		}
		r, err := h.client.Call(ctx, &wire.ContentRequest{Method: "GET", Ref: out.Record.Ref, Purpose: "research"})
		if err != nil {
			return err
		}
		return demand(r.Record.State == "cleaned")
	case "parallel-publication", "cleanup-race":
		other, err := open(h.root, h.token)
		if err != nil {
			return err
		}
		defer other.close()
		in, err := h.request(ctx, bytes.Repeat([]byte("v"), 100))
		if err != nil {
			return err
		}
		var wg sync.WaitGroup
		var a, b *wire.ContentResponse
		var ea, eb error
		wg.Add(2)
		go func() { defer wg.Done(); a, ea = h.client.Call(ctx, in) }()
		go func() {
			defer wg.Done()
			if name == "cleanup-race" {
				eb = other.service.Clean(ctx)
			} else {
				b, eb = other.client.Call(ctx, in)
			}
		}()
		wg.Wait()
		if ea != nil {
			return ea
		}
		if eb != nil {
			return eb
		}
		if name == "parallel-publication" && !proto.Equal(a.Record.Ref, b.Record.Ref) {
			return artifacts.Error("CHECK_FAILED")
		}
		r, err := other.read(ctx, a.Record.Ref, 5)
		if err != nil {
			return err
		}
		return demand(string(r.Data) == "vvvvv")
	case "publication-policy-race":
		hook := &afterPut{Blobs: h.blob, after: func() { h.policy.Replace(nil) }}
		h.bind(h.auth, hook)
		_, _, err := h.put(ctx, bytes.Repeat([]byte("s"), 100))
		if artifacts.Code(err) != "PERMISSION_DENIED" {
			return artifacts.Error("CHECK_FAILED")
		}
		return h.service.Clean(ctx)
	case "isolated-partition":
		err = h.auth.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error { tx.SetData([]byte("old-runtime")); return nil })
		if err != nil {
			return err
		}
		if _, _, err = h.put(ctx, []byte("hello")); err != nil {
			return err
		}
		return h.auth.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error { return demand(string(tx.Data()) == "old-runtime") })
	case "cross-domain-operation":
		_, in, err := h.put(ctx, []byte("hello"))
		if err != nil {
			return err
		}
		err = h.auth.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error { return tx.Operation(in.OperationId, "admin", true) })
		return demand(authorization.Is(err, authorization.IdentityConflict))
	case "sdk-invalid-wire":
		in, err := h.request(ctx, []byte("hello"))
		if err != nil {
			return err
		}
		in.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		_, err = h.client.Call(ctx, in)
		return demand(artifacts.Code(err) == "INVALID_ARGUMENT")
	case "unknown-publication":
		faulty := &unknownAuthority{Authority: h.auth}
		h.bind(faulty, h.blob)
		_, in, err := h.put(ctx, []byte("hello"))
		if artifacts.Code(err) != "OUTCOME_UNKNOWN" {
			return artifacts.Error("CHECK_FAILED")
		}
		h.bind(h.auth, h.blob)
		out, err := h.client.Call(ctx, &wire.ContentRequest{Method: "LOOKUP", OperationId: in.OperationId, Purpose: "research"})
		if err != nil {
			return err
		}
		return demand(out.Record.State == "available")
	}
	return artifacts.Error("UNKNOWN_CHECK")
}

type afterPut struct {
	artifacts.Blobs
	after func()
}

func (b *afterPut) Put(ctx context.Context, k string, data []byte) error {
	if err := b.Blobs.Put(ctx, k, data); err != nil {
		return err
	}
	b.after()
	return nil
}

type unknownAuthority struct {
	artifacts.Authority
	n int
}

func (a *unknownAuthority) UpdateContent(ctx context.Context, fn func(authorization.ContentTransaction) error) error {
	err := a.Authority.UpdateContent(ctx, fn)
	a.n++
	if err == nil && a.n == 2 {
		return artifacts.Error("OUTCOME_UNKNOWN")
	}
	return err
}
