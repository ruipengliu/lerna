package grants

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/gob"
	"google.golang.org/protobuf/proto"
	authlocal "lerna/adapters/authorization/local"
	"lerna/authorization"
	"lerna/sdk"
	"strings"
)

//go:embed testdata/05-authorization.gob
var legacyAuthorization []byte

func legacyUpgrade(ctx context.Context, h *harness) error {
	var previous authorization.State
	if err := gob.NewDecoder(bytes.NewReader(legacyAuthorization)).Decode(&previous); err != nil {
		return err
	}
	snapshot, err := h.db.Load(ctx)
	if err != nil {
		return err
	}
	if err = h.db.Commit(ctx, snapshot.Version, previous); err != nil {
		return err
	}
	h.token = strings.Repeat("a", 64)
	h.client = sdk.NewAuthorizationClient(authlocal.BindGrants(h.s, h.g, h.token), "local")
	old, err := h.client.GetGrant(ctx, "old-local")
	if err != nil {
		return err
	}
	if old.Mode != "continuous" {
		return require(false, "legacy grant changed")
	}
	req, err := h.request(ctx, "ISSUE", 2)
	if err != nil {
		return err
	}
	receipt, err := h.client.MutateGrant(ctx, req)
	if err != nil {
		return err
	}
	reopened, err := open(h.dir, h.token, h.c)
	if err != nil {
		return err
	}
	defer reopened.db.Close()
	if _, err = reopened.g.Get(ctx, h.token, receipt.GrantId); err != nil {
		return err
	}
	for id, op := range previous.Operations {
		original, err := reopened.s.LookupOperation(ctx, h.token, id)
		if err != nil {
			return err
		}
		if !proto.Equal(original, op.Receipt) {
			return require(false, "legacy receipt changed")
		}
	}
	decision, err := reopened.s.Evaluate(ctx, h.token, action())
	if err != nil {
		return err
	}
	return require(decision.Allowed, "legacy local authorization lost after signed upgrade")
}
