package content

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/gob"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"strings"
)

//go:embed testdata/05-authorization.gob
var oldState []byte

func legacy(ctx context.Context, h *harness) error {
	var old authorization.State
	if err := gob.NewDecoder(bytes.NewReader(oldState)).Decode(&old); err != nil {
		return err
	}
	snap, err := h.store.Load(ctx)
	if err != nil {
		return err
	}
	if err = h.store.Commit(ctx, snap.Version, old); err != nil {
		return err
	}
	h.auth, err = authorization.New(h.store, h.clock, old.Config)
	if err != nil {
		return err
	}
	h.token = strings.Repeat("a", 64)
	h.binding.Token = h.token
	h.bind(h.auth, h.blob)
	grant, err := h.auth.GetGrant(ctx, h.token, "old-local")
	if err != nil {
		return err
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"content.store", "content.process", "content.retain", "content.discover", "content.disclose", "content.delete"}, Purposes: []string{"research"}, Locations: []string{"device"}, ExpiresUnix: h.rule.RetainUntil}
	if err = h.mutate(ctx, &wire.AuthorizationCommand{ExpectedRevision: old.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "legacy", Scope: grant.Scope}, {Id: "content", Scope: scope}}}}}); err != nil {
		return err
	}
	if err = h.mutate(ctx, &wire.AuthorizationCommand{ExpectedRevision: old.Revision + 1, Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "content", Subject: "admin", Mode: "continuous", Scope: scope}}}); err != nil {
		return err
	}
	out, _, err := h.put(ctx, []byte("hello"))
	if err != nil {
		return err
	}
	again, err := authorization.New(h.store, h.clock, old.Config)
	if err != nil {
		return err
	}
	h.bind(again, h.blob)
	if _, err = h.read(ctx, out.Record.Ref, 5); err != nil {
		return err
	}
	for id, op := range old.Operations {
		receipt, err := again.LookupOperation(ctx, h.token, id)
		if err != nil {
			return err
		}
		if !proto.Equal(receipt, op.Receipt) {
			return demand(false)
		}
	}
	restored, err := again.GetGrant(ctx, h.token, "old-local")
	if err != nil {
		return err
	}
	return demand(proto.Equal(grant, restored))
}
