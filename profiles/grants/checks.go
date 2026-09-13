package grants

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"math/big"
	"os"
	"strings"
	"time"
)

var checks = []string{"legacy-storage-upgrade", "sign-revoke-race", "parallel-derive", "parent-resource-scope", "signer-configuration", "management-permissions", "time-rollback", "sdk-replay", "presenter-binding", "independent-jose", "strict-material", "claim-times", "policy-recheck", "delegation-attenuation", "quota-sharing", "single-operation", "ancestor-revocation", "recipient-applied", "unknown-issue", "sign-policy-race", "parallel-allocation", "key-unavailable", "legacy-local-grant", "cross-domain-identity", "finite-capacity"}

func check(ctx context.Context, name string) error {
	dir, err := os.MkdirTemp("", "lerna-grants-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	h, err := setup(ctx, dir)
	if err != nil {
		return err
	}
	defer h.db.Close()
	switch name {
	case "legacy-storage-upgrade":
		return legacyUpgrade(ctx, h)
	case "sign-revoke-race":
		return signingRevokeRace(ctx, h)
	case "parallel-derive":
		return parallelDerive(ctx, h)
	case "parent-resource-scope":
		return resourceAttenuation(ctx, h)
	case "signer-configuration":
		return signerConfiguration(h)

	case "management-permissions":
		return managementPermissions(ctx, h)
	case "time-rollback":
		r, err := h.issue(ctx)
		if err != nil {
			return err
		}
		h.c.advance(-time.Second)
		_, err = h.g.Verify(ctx, r.Material, h.present(), action())
		return expect(err, authorization.TimeUntrusted)

	case "sdk-replay":
		req, err := h.request(ctx, "ISSUE", 1)
		if err != nil {
			return err
		}
		r, err := h.client.MutateGrant(ctx, req)
		if err != nil {
			return err
		}
		replay, err := h.client.MutateGrant(ctx, req)
		if err != nil {
			return err
		}
		if !proto.Equal(r, replay) {
			return fmt.Errorf("replay changed signed material")
		}
		lookup, err := h.client.LookupGrantOperation(ctx, req.OperationId)
		if err != nil {
			return err
		}
		return require(proto.Equal(r, lookup), "lost operation")
	case "presenter-binding":
		r, err := h.issue(ctx)
		if err != nil {
			return err
		}
		if _, err = h.g.Verify(ctx, r.Material, h.present(), action()); err != nil {
			return err
		}
		for _, field := range []string{"subject", "audience", "presenter", "certificate", "namespace"} {
			p := h.present()
			switch field {
			case "subject":
				p.Subject = "other"
			case "audience":
				p.Audience = "other"
			case "presenter":
				p.Presenter = "other"
			case "certificate":
				p.CertificateSHA256 = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
			case "namespace":
				p.Namespace = "other"
			}
			_, err = h.g.Verify(ctx, r.Material, p, action())
			if err == nil {
				return fmt.Errorf("accepted wrong %s", field)
			}
		}
		return nil
	case "independent-jose":
		r, err := h.issue(ctx)
		if err != nil {
			return err
		}
		parts := strings.Split(r.Material, ".")
		signature, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if len(signature) != 64 || !ecdsa.Verify(&h.key.PublicKey, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
			return fmt.Errorf("JOSE signature not raw R||S")
		}
		rr, ss, err := ecdsa.Sign(rand.Reader, h.key, digest[:])
		if err != nil {
			return err
		}
		raw := make([]byte, 64)
		rr.FillBytes(raw[:32])
		ss.FillBytes(raw[32:])
		independent := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(raw)
		_, err = h.g.Verify(ctx, independent, h.present(), action())
		return err
	case "strict-material":
		r, err := h.issue(ctx)
		if err != nil {
			return err
		}
		parts := strings.Split(r.Material, ".")
		headers := []string{`{"alg":"none","typ":"harness-grant+jwt;v=1","kid":"signer-1"}`, `{"alg":"ES256","typ":"JWT","kid":"signer-1"}`, `{"alg":"ES256","typ":"harness-grant+jwt;v=1","kid":"unknown"}`, `{"alg":"ES256","typ":"harness-grant+jwt;v=1","kid":"signer-1","jku":"https://untrusted.invalid"}`, `{"alg":"ES256","alg":"ES256","typ":"harness-grant+jwt;v=1","kid":"signer-1"}`}
		for _, header := range headers {
			material, err := signRaw(h, []byte(header), parts[1])
			if err != nil {
				return err
			}
			if _, err = h.g.Verify(ctx, material, h.present(), action()); err == nil {
				return fmt.Errorf("accepted invalid protected header")
			}
		}
		for _, field := range []string{"iss", "aud", "sub", "extra"} {
			raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
			var v map[string]any
			if err = json.Unmarshal(raw, &v); err != nil {
				return err
			}
			v[field] = "wrong"
			raw, _ = json.Marshal(v)
			material, err := h.crypto.Sign(ctx, raw)
			if err != nil {
				return err
			}
			if _, err = h.g.Verify(ctx, material, h.present(), action()); err == nil {
				return fmt.Errorf("accepted wrong claim %s", field)
			}
		}
		return nil
	case "claim-times":
		h.spec.NotBefore += 10
		r, err := h.issue(ctx)
		if err != nil {
			return err
		}
		if _, err = h.g.Verify(ctx, r.Material, h.present(), action()); err == nil {
			return fmt.Errorf("accepted before nbf")
		}
		h.c.advance(time.Hour)
		_, err = h.g.Verify(ctx, r.Material, h.present(), action())
		return expect(err, authorization.Denied)
	case "policy-recheck":
		r, err := h.issue(ctx)
		if err != nil {
			return err
		}
		if err = h.denyPolicy(ctx, 2); err != nil {
			return err
		}
		_, err = h.g.Verify(ctx, r.Material, h.present(), action())
		return expect(err, authorization.Denied)
	case "delegation-attenuation":
		h.spec.Scope.ExpiresUnix -= 1800
		root, err := h.issue(ctx)
		if err != nil {
			return err
		}
		for _, field := range []string{"action", "purpose", "location", "time", "depth", "presenter", "unknown"} {
			req, err := h.request(ctx, "DERIVE", 2)
			if err != nil {
				return err
			}
			req.GrantId = root.GrantId
			req.ExpectedGrantRevision = 2
			req.Spec.DelegationDepth = 2
			req.Spec.Units = 1
			switch field {
			case "action":
				req.Spec.Scope.Actions = []string{"write"}
			case "purpose":
				req.Spec.Scope.Purposes = []string{"advertising"}
			case "location":
				req.Spec.Scope.Locations = []string{"cloud"}
			case "time":
				req.Spec.Scope.ExpiresUnix++
			case "depth":
				req.Spec.DelegationDepth = 3
			case "presenter":
				req.Spec.Presenter = "other"
			case "unknown":
				req.Spec.Scope.RequiredConstraints = []string{"unimplemented"}
			}
			if _, err = h.g.Mutate(ctx, h.token, req); err == nil {
				return fmt.Errorf("expanded %s", field)
			}
		}
		return nil
	case "quota-sharing":
		return quotaCheck(ctx, h)
	case "single-operation":
		h.spec.Mode = "single"
		h.spec.Units = 1
		h.spec.DelegationDepth = 0
		h.spec.OperationBinding = "business"
		h.spec.SemanticSha256 = strings.Repeat("a", 64)
		r, err := h.issue(ctx)
		if err != nil {
			return err
		}
		p := h.present()
		p.OperationID = "business"
		p.SemanticSHA256 = h.spec.SemanticSha256
		one, err := h.g.ReserveUse(ctx, r.Material, p, action(), 1)
		if err != nil {
			return err
		}
		two, err := h.g.ReserveUse(ctx, r.Material, p, action(), 1)
		if err != nil {
			return err
		}
		if one != two {
			return fmt.Errorf("double allocation")
		}
		p.OperationID = "other"
		_, err = h.g.ReserveUse(ctx, r.Material, p, action(), 1)
		return expect(err, authorization.Denied)
	case "ancestor-revocation", "recipient-applied":
		return revokeCheck(ctx, h, name)
	case "unknown-issue":
		return unknownCheck(ctx, h)
	case "sign-policy-race":
		return signingRace(ctx, h)
	case "parallel-allocation":
		return parallelCheck(ctx, h)
	case "key-unavailable":
		cfg := grantConfig()
		g, err := h.s.SignedGrants(cfg, unavailableSigner{h.crypto})
		if err != nil {
			return err
		}
		req, err := h.request(ctx, "ISSUE", 1)
		if err != nil {
			return err
		}
		_, err = g.Mutate(ctx, h.token, req)
		if err = expect(err, authorization.Unavailable); err != nil {
			return err
		}
		_, err = g.LookupOperation(ctx, h.token, req.OperationId)
		return expect(err, authorization.NotFound)
	case "legacy-local-grant":
		req, err := h.request(ctx, "ISSUE", 1)
		if err != nil {
			return err
		}
		_, err = h.s.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: req.OperationId, Command: &wire.AuthorizationCommand{ExpectedRevision: 1, Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "legacy", Subject: "admin", Scope: h.spec.Scope, Mode: "continuous"}}}})
		if err != nil {
			return err
		}
		req, err = h.request(ctx, "ISSUE", 2)
		if err != nil {
			return err
		}
		if _, err = h.client.MutateGrant(ctx, req); err != nil {
			return err
		}
		decision, err := h.s.Evaluate(ctx, h.token, action())
		if err != nil {
			return err
		}
		return require(decision.Allowed, "new grants broke local continuous grant")
	case "cross-domain-identity":
		req, err := h.request(ctx, "ISSUE", 1)
		if err != nil {
			return err
		}
		if _, err = h.client.MutateGrant(ctx, req); err != nil {
			return err
		}
		_, err = h.s.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: req.OperationId, Command: &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_CloseWindows{CloseWindows: true}}})
		return expect(err, authorization.IdentityConflict)
	case "finite-capacity":
		cfg := grantConfig()
		cfg.MaxRecords = 1
		g, err := h.s.SignedGrants(cfg, h.crypto)
		if err != nil {
			return err
		}
		req, err := h.request(ctx, "ISSUE", 1)
		if err != nil {
			return err
		}
		first := proto.Clone(req).(*wire.GrantMutation)
		if _, err = g.Mutate(ctx, h.token, req); err != nil {
			return err
		}
		req, err = h.request(ctx, "ISSUE", 2)
		if err != nil {
			return err
		}
		_, err = g.Mutate(ctx, h.token, req)
		if err = expect(err, authorization.Unavailable); err != nil {
			return err
		}
		original, err := g.LookupOperation(ctx, h.token, first.OperationId)
		if err != nil {
			return err
		}
		revoke, err := h.request(ctx, "REVOKE", 2)
		if err != nil {
			return err
		}
		revoke.Spec = nil
		revoke.GrantId = original.GrantId
		revoke.ExpectedGrantRevision = 2
		_, err = g.Mutate(ctx, h.token, revoke)
		return err
	}
	return fmt.Errorf("unknown check")
}
func signRaw(h *harness, header []byte, payload string) (string, error) {
	input := base64.RawURLEncoding.EncodeToString(header) + "." + payload
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, h.key, digest[:])
	if err != nil {
		return "", err
	}
	raw := make([]byte, 64)
	r.FillBytes(raw[:32])
	s.FillBytes(raw[32:])
	return input + "." + base64.RawURLEncoding.EncodeToString(raw), nil
}
func (h *harness) denyPolicy(ctx context.Context, revision uint64) error {
	id, err := h.s.NewOperation(ctx, h.token)
	if err != nil {
		return err
	}
	_, err = h.s.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: id, Command: &wire.AuthorizationCommand{ExpectedRevision: revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{}}}})
	return err
}
