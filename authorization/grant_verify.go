package authorization

import (
	"bytes"
	"context"
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/internal/jsonvalue"
	"time"
)

// GrantPresentation is supplied by a trusted host after authenticating the peer,
// never decoded from the caller's ordinary request or signed material.
type GrantPresentation struct{ Namespace, Subject, Audience, Presenter, CertificateSHA256, OperationID, SemanticSHA256 string }

func (g *GrantAuthority) Verify(ctx context.Context, material string, p GrantPresentation, action *wire.AuthorizationAction) (*wire.GrantRecord, error) {
	ctx, cancelCall := context.WithTimeout(ctx, g.config.IOTimeout)
	defer cancelCall()
	if len(material) > 32768 || action == nil || !known(action) {
		return nil, fail(Invalid)
	}
	bounded, cancel := context.WithTimeout(ctx, g.config.IOTimeout)
	defer cancel()
	payload, err := g.crypto.Verify(bounded, material)
	if err != nil {
		return nil, fail(Denied)
	}
	if len(payload) > 16384 {
		return nil, fail(Invalid)
	}
	if _, err = jsonvalue.Decode(payload); err != nil {
		return nil, fail(Invalid)
	}
	var claims grantClaims
	d := json.NewDecoder(bytes.NewReader(payload))
	d.DisallowUnknownFields()
	if d.Decode(&claims) != nil {
		return nil, fail(Invalid)
	}
	spec := new(wire.SignedGrantSpec)
	if protojson.Unmarshal(claims.Spec, spec) != nil {
		return nil, fail(Unsupported)
	}
	if claims.Issuer != g.config.Issuer || claims.Namespace != p.Namespace || claims.Subject != p.Subject || claims.Audience != p.Audience || spec.Subject != p.Subject || spec.Audience != p.Audience || spec.Presenter != p.Presenter || spec.CertificateSha256 != p.CertificateSHA256 || len(claims.Confirmation) != 1 || claims.Confirmation["x5t#S256"] != p.CertificateSHA256 || claims.Issued <= 0 || claims.NotBefore != spec.NotBefore || spec.Scope == nil || claims.Expires != spec.Scope.ExpiresUnix || claims.Issued >= claims.Expires {
		return nil, fail(Denied)
	}
	if spec.Mode == "single" && (spec.OperationBinding != p.OperationID || spec.SemanticSha256 != p.SemanticSHA256) {
		return nil, fail(Denied)
	}
	var out *wire.GrantRecord
	err = g.service.update(ctx, func(st *State, now time.Time) error {
		if st.Namespace != p.Namespace {
			return fail(Denied)
		}
		if err := g.journal(st); err != nil {
			return err
		}
		if claims.Issued > now.Add(g.config.ClockSkew).Unix() || now.Unix() < claims.NotBefore || now.Unix() >= claims.Expires {
			return fail(Denied)
		}
		entry, ok := st.Signed.Grants[claims.GrantID]
		if !ok {
			return fail(Denied)
		}
		if entry.Record.Revoked || entry.Record.Revision != claims.Revision || entry.Record.Parent != claims.Parent || !proto.Equal(entry.Record.Spec, spec) {
			return fail(Denied)
		}
		if err := g.chain(st, claims.GrantID, now); err != nil {
			return err
		}
		found := false
		for _, v := range st.Principals {
			if v.Subject == p.Subject && !v.Disabled && v.Expires > now.Unix() {
				found = true
			}
		}
		if !found {
			return fail(Denied)
		}
		b := g.service.newBudget()
		matched, err := g.service.matches(st, spec.Scope, action, now, b)
		if err != nil {
			return err
		}
		if !matched {
			return fail(Denied)
		}
		allowed, _, err := g.service.policyAllows(st, action, now, b)
		if err != nil {
			return err
		}
		if !allowed {
			return fail(Denied)
		}
		if len(action.RequiredConstraints) > 0 {
			return fail(Unsupported)
		}
		out = proto.Clone(entry.Record).(*wire.GrantRecord)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
