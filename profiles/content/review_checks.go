package content

import (
	"bytes"
	"context"
	"google.golang.org/protobuf/proto"
	contentlocal "lerna/adapters/content/local"
	"lerna/artifacts"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"os"
	"path/filepath"
	"time"
)

func credentialExistence(ctx context.Context, h *harness) error {
	out, in, err := h.put(ctx, []byte("hello"))
	if err != nil {
		return err
	}
	missing := &wire.ContentRef{Namespace: "local", Key: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", Revision: 1}
	op, err := h.auth.NewOperation(ctx, h.token)
	if err != nil {
		return err
	}
	disabled, err := authorization.NewCredential()
	if err != nil {
		return err
	}
	now, _ := h.clock.Now()
	if err = h.mutate(ctx, &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_RegisterPrincipal{RegisterPrincipal: &wire.RegisterPrincipal{Subject: "disabled", CredentialSha256: authorization.CredentialDigest(disabled), ExpiresUnix: now.Add(time.Hour).Unix(), Disabled: true}}}); err != nil {
		return err
	}
	for _, token := range []string{"invalid", disabled, h.token} {
		if token == h.token {
			h.clock.advance(25 * time.Hour)
		}
		b := h.binding
		b.Token = token
		client := sdk.NewContentClient(contentlocal.Bind(h.service, b), "local")
		for _, method := range []string{"GET", "READ", "DELETE", "LOOKUP"} {
			a := &wire.ContentRequest{Method: method, Ref: out.Record.Ref, Purpose: "research"}
			z := proto.Clone(a).(*wire.ContentRequest)
			z.Ref = missing
			switch method {
			case "READ":
				a.Limit = 1
				z.Limit = 1
			case "DELETE":
				a.OperationId = op
				z.OperationId = op
				a.ExpectedRevision = 1
				z.ExpectedRevision = 1
			case "LOOKUP":
				a.Ref = nil
				z.Ref = nil
				a.OperationId = in.OperationId
				z.OperationId = op
			}
			_, ea := client.Call(ctx, a)
			_, ez := client.Call(ctx, z)
			if artifacts.Code(ea) != "UNAUTHENTICATED" || artifacts.Code(ez) != "UNAUTHENTICATED" {
				return artifacts.Error("EXISTENCE_LEAK")
			}
		}
	}
	return nil
}
func metadataAvailability(ctx context.Context, h *harness) error {
	out, in, err := h.put(ctx, bytes.Repeat([]byte("a"), 100))
	if err != nil {
		return err
	}
	path := filepath.Join(h.root, "content", out.Record.Ref.Key)
	for _, state := range []string{"corrupt", "missing", "unavailable"} {
		switch state {
		case "corrupt":
			if err = os.WriteFile(path, bytes.Repeat([]byte("b"), 100), 0600); err != nil {
				return err
			}
		case "missing":
			if err = os.Remove(path); err != nil {
				return err
			}
		case "unavailable":
			if err = os.Mkdir(path, 0700); err != nil {
				return err
			}
		}
		for _, req := range []*wire.ContentRequest{{Method: "GET", Ref: out.Record.Ref, Purpose: "research"}, {Method: "LOOKUP", OperationId: in.OperationId, Purpose: "research"}, in} {
			r, err := h.client.Call(ctx, req)
			if err != nil {
				return err
			}
			if r.Record.State != state {
				return artifacts.Error("FALSE_AVAILABILITY")
			}
		}
	}
	return nil
}
