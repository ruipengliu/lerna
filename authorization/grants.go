package authorization

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/internal/randomid"
	"time"
)

// GrantCrypto is the trusted signing/verification adapter; it must honor context.
type GrantCrypto interface {
	Sign(context.Context, []byte) (string, error)
	Verify(context.Context, string) ([]byte, error)
}
type GrantConfig struct {
	Issuer                       string
	MaxTTL, IOTimeout, ClockSkew time.Duration
	MaxDepth, MaxRecords         int
}
type GrantAuthority struct {
	service *Service
	crypto  GrantCrypto
	config  GrantConfig
}

func (s *Service) SignedGrants(c GrantConfig, crypto GrantCrypto) (*GrantAuthority, error) {
	if !validName(c.Issuer) || crypto == nil || c.MaxTTL < time.Second || c.MaxTTL > time.Hour || c.IOTimeout < time.Millisecond || c.IOTimeout > time.Second || c.ClockSkew < 0 || c.ClockSkew > time.Minute || c.MaxDepth < 1 || c.MaxDepth > 16 || c.MaxRecords < 1 || c.MaxRecords > 512 {
		return nil, fail(Invalid)
	}
	return &GrantAuthority{s, crypto, c}, nil
}

type GrantEntry struct {
	Record  *wire.GrantRecord
	Creator string
}
type GrantOperation struct {
	Subject string
	Request *wire.GrantMutation
	Receipt *wire.GrantReceipt
}
type GrantJournal struct {
	Uses       map[string]UsePermit
	Config     GrantConfig
	Grants     map[string]GrantEntry
	Operations map[string]GrantOperation
}
type grantClaims struct {
	Issuer       string            `json:"iss"`
	Subject      string            `json:"sub"`
	Audience     string            `json:"aud"`
	Issued       int64             `json:"iat"`
	NotBefore    int64             `json:"nbf"`
	Expires      int64             `json:"exp"`
	Confirmation map[string]string `json:"cnf"`
	Namespace    string            `json:"namespace"`
	GrantID      string            `json:"grant"`
	Revision     uint64            `json:"revision"`
	Parent       string            `json:"parent"`
	Spec         json.RawMessage   `json:"harness"`
}

func (g *GrantAuthority) journal(st *State) error {
	if st.Signed == nil {
		st.Signed = &GrantJournal{Config: g.config, Grants: map[string]GrantEntry{}, Uses: map[string]UsePermit{}, Operations: map[string]GrantOperation{}}
	}
	if st.Signed.Uses == nil {
		st.Signed.Uses = map[string]UsePermit{}
	}
	if st.Signed.Config != g.config {
		return fail(Invalid)
	}
	return nil
}
func (g *GrantAuthority) manage(st *State, p Principal, in *wire.GrantMutation, now time.Time) error {
	if p.Administrator {
		return nil
	}
	if in.Kind == "DERIVE" {
		parent, ok := st.Signed.Grants[in.GrantId]
		if !ok || parent.Record.Spec.Subject != p.Subject || parent.Record.Spec.DelegationDepth == 0 {
			return fail(Denied)
		}
		return g.chain(st, in.GrantId, now)
	}
	if in.Kind == "REVOKE" {
		entry, ok := st.Signed.Grants[in.GrantId]
		if ok && entry.Creator == p.Subject {
			return nil
		}
	}
	if in.Kind == "ISSUE" && in.Spec != nil && in.Spec.DelegationDepth == 0 {
		return g.service.authorizeManagement(st, p, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Subject: in.Spec.Subject, Scope: in.Spec.Scope, Mode: "continuous"}}}, now)
	}
	return fail(Denied)
}
func (g *GrantAuthority) validate(st *State, in *wire.GrantMutation, now time.Time) error {
	v := in.Spec
	if in.Kind == "REBIND" {
		if in.Spec != nil {
			return fail(Invalid)
		}
		entry, ok := st.Signed.Grants[in.GrantId]
		if !ok {
			return fail(NotFound)
		}
		if entry.Record.Revision != in.ExpectedGrantRevision {
			return fail(Conflict)
		}
		if err := g.chain(st, in.GrantId, now); err != nil {
			return err
		}
		_, err := grantCertificate(st, entry.Record.Spec, now)
		return err
	}
	if in.Kind == "REVOKE" {
		if v != nil {
			return fail(Invalid)
		}
		entry, ok := st.Signed.Grants[in.GrantId]
		if !ok {
			return fail(NotFound)
		}
		if entry.Record.Revoked {
			return fail(Conflict)
		}
		if entry.Record.Revision != in.ExpectedGrantRevision {
			return fail(Conflict)
		}
		return nil
	}
	if in.Kind != "ISSUE" && in.Kind != "DERIVE" {
		return fail(Unsupported)
	}
	if (in.Kind == "ISSUE" && (in.GrantId != "" || in.ExpectedGrantRevision != 0)) || v == nil || !known(v) || !validName(v.Subject) || !validName(v.Audience) || !validName(v.Presenter) || v.Scope == nil || v.Units == 0 || v.Units > 1000000 || v.DelegationDepth > uint32(g.config.MaxDepth) || v.NotBefore <= 0 || v.NotBefore >= v.Scope.ExpiresUnix || v.Scope.ExpiresUnix > now.Add(g.config.MaxTTL).Unix() {
		return fail(Invalid)
	}

	if len(v.DelegateTargets) > 16 || (v.DelegationDepth == 0 && len(v.DelegateTargets) > 0) {
		return fail(Invalid)
	}
	for _, t := range v.DelegateTargets {
		if !validTarget(t) {
			return fail(Invalid)
		}
	}
	digest, err := base64.RawURLEncoding.Strict().DecodeString(v.CertificateSha256)
	if err != nil || len(digest) != 32 {
		return fail(Invalid)
	}
	switch v.Mode {
	case "continuous":
		if v.OperationBinding != "" || v.SemanticSha256 != "" {
			return fail(Invalid)
		}
	case "single":
		if v.Units != 1 || v.DelegationDepth != 0 || len(v.OperationBinding) == 0 || len(v.OperationBinding) > 512 || !digestValid(v.SemanticSha256) {
			return fail(Invalid)
		}
	default:
		return fail(Unsupported)
	}
	found := false
	for _, p := range st.Principals {
		if p.Subject == v.Subject && !p.Disabled && p.Expires >= v.Scope.ExpiresUnix {
			found = true
		}
	}
	if !found {
		return fail(Denied)
	}
	b := g.service.newBudget()
	if err := g.service.validateScope(st, v.Scope, now, b); err != nil {
		return err
	}
	if err := g.service.policyCovers(st, v.Scope, now, b); err != nil {
		return err
	}
	if in.Kind == "DERIVE" {
		parent, ok := st.Signed.Grants[in.GrantId]
		if !ok {
			return fail(NotFound)
		}
		if parent.Record.Revision != in.ExpectedGrantRevision {
			return fail(Conflict)
		}
		if err := g.chain(st, in.GrantId, now); err != nil {
			return err
		}
		p := parent.Record.Spec
		if parent.Record.Allocated > p.Units || v.Units > p.Units-parent.Record.Allocated {
			return fail(Denied)
		}
		if err := g.attenuates(st, p, v, b); err != nil {
			return err
		}
	}
	return nil
}
func (g *GrantAuthority) Mutate(ctx context.Context, token string, input *wire.GrantMutation) (*wire.GrantReceipt, error) {
	ctx, cancelCall := context.WithTimeout(ctx, g.config.IOTimeout)
	defer cancelCall()
	if input == nil || !known(input) || proto.Size(input) > 16384 {
		return nil, fail(Invalid)
	}
	in := proto.Clone(input).(*wire.GrantMutation)
	if in.Spec != nil {
		normalizeCommand(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Scope: in.Spec.Scope}}})
	}
	var old *wire.GrantReceipt
	var claims grantClaims
	var subject string
	id, err := randomid.New()
	if err != nil {
		return nil, err
	}
	check := func(st *State, now time.Time) error {
		p, err := authenticate(st, token, now)
		if err != nil {
			return err
		}
		subject = p.Subject
		if err = g.journal(st); err != nil {
			return err
		}
		if err = g.manage(st, p, in, now); err != nil {
			return err
		}
		if _, ok := st.Signed.Uses[in.OperationId]; ok {
			return fail(IdentityConflict)
		}
		epoch, err := windowOf(st, in.OperationId)
		if err != nil {
			return err
		}
		if _, ok := st.Operations[in.OperationId]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := st.MemoryOperations[in.OperationId]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := st.ExecutionOperations[in.OperationId]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := st.ContentOperations[in.OperationId]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := st.RuntimeOperations[in.OperationId]; ok {
			return fail(IdentityConflict)
		}
		if op, ok := st.Signed.Operations[in.OperationId]; ok {
			if op.Subject != p.Subject {
				return fail(Denied)
			}
			if !proto.Equal(op.Request, in) {
				return fail(IdentityConflict)
			}
			old = proto.Clone(op.Receipt).(*wire.GrantReceipt)
			return nil
		}
		if epoch <= st.ClosedThrough || epoch != st.Window || now.UnixNano() >= st.WindowExpires {
			return fail(Expired)
		}
		if st.Revision != in.ExpectedRevision {
			return fail(Conflict)
		}
		if len(st.Signed.Operations) >= 2*g.config.MaxRecords || (in.Kind != "REVOKE" && in.Kind != "REBIND" && len(st.Signed.Grants) >= g.config.MaxRecords) {
			return fail(Unavailable)
		}
		return g.validate(st, in, now)
	}
	err = g.service.update(ctx, func(st *State, now time.Time) error {
		old = nil
		if err := check(st, now); err != nil {
			return err
		}
		if old != nil {
			return nil
		}
		if in.Kind == "REVOKE" {
			return nil
		}
		signSpec := in.Spec
		grantID, revision, parent := id, st.Revision+1, in.GrantId
		if in.Kind == "REBIND" {
			entry := st.Signed.Grants[in.GrantId].Record
			signSpec, grantID, revision, parent = entry.Spec, entry.Id, entry.Revision, entry.Parent
		}
		certificate, err := grantCertificate(st, signSpec, now)
		if err != nil {
			return err
		}
		spec, err := protojson.Marshal(signSpec)
		if err != nil {
			return fail(Invalid)
		}
		claims = grantClaims{Issuer: g.config.Issuer, Subject: signSpec.Subject, Audience: signSpec.Audience, Issued: now.Unix(), NotBefore: signSpec.NotBefore, Expires: signSpec.Scope.ExpiresUnix, Confirmation: map[string]string{"x5t#S256": certificate}, Namespace: st.Namespace, GrantID: grantID, Revision: revision, Parent: parent, Spec: spec}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if old != nil {
		return old, nil
	}
	material := ""
	if in.Kind != "REVOKE" {
		payload, e := json.Marshal(claims)
		if e != nil {
			return nil, fail(Invalid)
		}
		bounded, cancel := context.WithTimeout(ctx, g.config.IOTimeout)
		defer cancel()
		material, err = g.crypto.Sign(bounded, payload)
		if err != nil || bounded.Err() != nil {
			return nil, fail(Unavailable)
		}
		if len(material) == 0 || len(material) > 32768 {
			return nil, fail(Invalid)
		}
	}

	var receipt *wire.GrantReceipt
	err = g.service.update(ctx, func(st *State, now time.Time) error {
		old = nil
		if err := check(st, now); err != nil {
			return err
		}
		if old != nil {
			receipt = old
			return nil
		}
		st.Revision++
		if in.Kind == "REVOKE" {
			id = in.GrantId
			entry := st.Signed.Grants[id]
			entry.Record.Revoked = true
			entry.Record.Revision = st.Revision
			entry.Record.Notices = nil
			recipients := map[string]bool{}
			for target, e := range st.Signed.Grants {
				if g.descends(st, target, id) && !recipients[e.Record.Spec.Audience] {
					recipients[e.Record.Spec.Audience] = true
					entry.Record.Notices = append(entry.Record.Notices, &wire.GrantNotice{GrantId: id, Recipient: e.Record.Spec.Audience, Revision: st.Revision})
				}
			}
			st.Signed.Grants[id] = entry
		} else if in.Kind == "REBIND" {
			id = in.GrantId
		} else {
			record := &wire.GrantRecord{Id: id, Parent: in.GrantId, Spec: in.Spec, Revision: st.Revision, Notices: []*wire.GrantNotice{{GrantId: id, Recipient: in.Spec.Audience, Revision: st.Revision}}}
			st.Signed.Grants[id] = GrantEntry{Record: record, Creator: subject}
			if in.Kind == "DERIVE" {
				parent := st.Signed.Grants[in.GrantId]
				parent.Record.Allocated += in.Spec.Units
				st.Signed.Grants[in.GrantId] = parent
			}
		}
		receipt = &wire.GrantReceipt{OperationId: in.OperationId, GrantId: id, Revision: st.Revision, Material: material}

		st.Signed.Operations[in.OperationId] = GrantOperation{Subject: subject, Request: in, Receipt: receipt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return proto.Clone(receipt).(*wire.GrantReceipt), nil
}
func (g *GrantAuthority) Get(ctx context.Context, token, id string) (*wire.GrantRecord, error) {
	ctx, cancelCall := context.WithTimeout(ctx, g.config.IOTimeout)
	defer cancelCall()
	var out *wire.GrantRecord
	err := g.service.update(ctx, func(st *State, now time.Time) error {
		p, err := authenticate(st, token, now)
		if err != nil {
			return err
		}
		if err = g.journal(st); err != nil {
			return err
		}
		entry, ok := st.Signed.Grants[id]
		if !ok {
			return fail(NotFound)
		}
		if !p.Administrator && p.Subject != entry.Record.Spec.Subject && p.Subject != entry.Creator {
			return fail(Denied)
		}
		out = proto.Clone(entry.Record).(*wire.GrantRecord)
		for ancestor, depth := out.Parent, 0; ancestor != "" && depth <= g.config.MaxDepth; depth++ {
			e, ok := st.Signed.Grants[ancestor]
			if !ok {
				return fail(Unavailable)
			}
			if e.Record.Revoked {
				out.RevokedAncestors = append(out.RevokedAncestors, ancestor)
			}
			ancestor = e.Record.Parent
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
