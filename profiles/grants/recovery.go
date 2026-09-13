package grants

import (
	"context"
	"fmt"
	"lerna/authorization"
	"strings"
	"sync"
)

func quotaCheck(ctx context.Context, h *harness) error {
	root, err := h.issue(ctx)
	if err != nil {
		return err
	}
	child, err := h.request(ctx, "DERIVE", 2)
	if err != nil {
		return err
	}
	child.GrantId = root.GrantId
	child.ExpectedGrantRevision = 2
	child.Spec.Units = 5
	child.Spec.DelegationDepth = 2
	childReceipt, err := h.client.MutateGrant(ctx, child)
	if err != nil {
		return err
	}
	grandchild, err := h.request(ctx, "DERIVE", 3)
	if err != nil {
		return err
	}
	grandchild.GrantId = childReceipt.GrantId
	grandchild.ExpectedGrantRevision = 3
	grandchild.Spec.Units = 3
	grandchild.Spec.DelegationDepth = 1
	grandReceipt, err := h.client.MutateGrant(ctx, grandchild)
	if err != nil {
		return err
	}
	cp := h.present()
	cp.OperationID = "child-own"
	cp.SemanticSHA256 = strings.Repeat("b", 64)
	if _, err = h.g.ReserveUse(ctx, childReceipt.Material, cp, action(), 3); !authorization.Is(err, authorization.Denied) {
		return fmt.Errorf("grandchild allocation ignored: %v", err)
	}
	childUse, err := h.g.ReserveUse(ctx, childReceipt.Material, cp, action(), 2)
	if err != nil {
		return err
	}
	if err = consumeFixture(ctx, h.dir, childUse); err != nil {
		return err
	}
	cp.OperationID = "grandchild-own"
	grandUse, err := h.g.ReserveUse(ctx, grandReceipt.Material, cp, action(), 3)
	if err != nil {
		return err
	}
	if err = consumeFixture(ctx, h.dir, grandUse); err != nil {
		return err
	}

	p := h.present()
	p.OperationID = "own"
	p.SemanticSHA256 = strings.Repeat("a", 64)
	if _, err = h.g.ReserveUse(ctx, root.Material, p, action(), 4); !authorization.Is(err, authorization.Denied) {
		return fmt.Errorf("delegated allocation not subtracted: %v", err)
	}
	permit, err := h.g.ReserveUse(ctx, root.Material, p, action(), 3)
	if err != nil {
		return err
	}
	// A separately reopened recipient journal records the permit before any effect.
	return consumeFixture(ctx, h.dir, permit)
}
func revokeCheck(ctx context.Context, h *harness, name string) error {
	root, err := h.issue(ctx)
	if err != nil {
		return err
	}
	child, err := h.request(ctx, "DERIVE", 2)
	if err != nil {
		return err
	}
	child.GrantId = root.GrantId
	child.ExpectedGrantRevision = 2
	child.Spec.Units = 4
	child.Spec.DelegationDepth = 2
	r, err := h.client.MutateGrant(ctx, child)
	if err != nil {
		return err
	}
	revoke, err := h.request(ctx, "REVOKE", 3)
	if err != nil {
		return err
	}
	revoke.Spec = nil
	revoke.GrantId = root.GrantId
	revoke.ExpectedGrantRevision = 2
	receipt, err := h.client.MutateGrant(ctx, revoke)
	if err != nil {
		return err
	}
	if _, err = h.g.Verify(ctx, r.Material, h.present(), action()); !authorization.Is(err, authorization.Denied) {
		return fmt.Errorf("ancestor bypass: %v", err)
	}
	if name == "ancestor-revocation" {
		leaf, err := h.client.GetSignedGrant(ctx, r.GrantId)
		if err != nil {
			return err
		}
		return require(len(leaf.RevokedAncestors) == 1 && leaf.RevokedAncestors[0] == root.GrantId, "leaf read hid ancestor revocation")
	}
	record, err := h.client.GetSignedGrant(ctx, root.GrantId)
	if err != nil {
		return err
	}
	if len(record.Notices) != 1 || record.Notices[0].Applied {
		return fmt.Errorf("authority commit fabricated application")
	}
	wrong := h.present()
	wrong.Presenter = "other"
	if err = h.g.ConfirmApplied(ctx, wrong, root.GrantId, receipt.Revision); err == nil {
		return fmt.Errorf("forged applied")
	}
	if err = h.g.ConfirmApplied(ctx, h.present(), root.GrantId, 2); !authorization.Is(err, authorization.Conflict) {
		return fmt.Errorf("stale confirmation: %v", err)
	}
	// This call models a trusted adapter after committing its applied revision.
	if err = h.g.ConfirmApplied(ctx, h.present(), root.GrantId, receipt.Revision); err != nil {
		return err
	}
	if err = h.g.ConfirmApplied(ctx, h.present(), root.GrantId, receipt.Revision); err != nil {
		return err
	}
	record, err = h.client.GetSignedGrant(ctx, root.GrantId)
	if err != nil {
		return err
	}
	return require(record.Notices[0].Applied, "application lost")
}

type unavailableSigner struct{ authorization.GrantCrypto }

func (unavailableSigner) Sign(context.Context, []byte) (string, error) {
	return "", &authorization.Error{Code: authorization.Unavailable}
}

type afterSigner struct {
	authorization.GrantCrypto
	after func() error
}

func (s afterSigner) Sign(ctx context.Context, p []byte) (string, error) {
	out, err := s.GrantCrypto.Sign(ctx, p)
	if err != nil {
		return "", err
	}
	if err = s.after(); err != nil {
		return "", err
	}
	return out, nil
}

type lostStore struct {
	authorization.Store
	armed bool
}

func (s *lostStore) Commit(ctx context.Context, v uint64, state authorization.State) error {
	err := s.Store.Commit(ctx, v, state)
	if err == nil && s.armed {
		s.armed = false
		return &authorization.Error{Code: authorization.OutcomeUnknown}
	}
	return err
}
func unknownCheck(ctx context.Context, h *harness) error {
	fault := &lostStore{Store: h.db}
	other, err := assemble(fault, h.dir, h.token, h.c)
	if err != nil {
		return err
	}
	g, err := other.s.SignedGrants(grantConfig(), afterSigner{GrantCrypto: h.crypto, after: func() error { fault.armed = true; return nil }})
	if err != nil {
		return err
	}
	req, err := h.request(ctx, "ISSUE", 1)
	if err != nil {
		return err
	}
	out, err := g.Mutate(ctx, h.token, req)
	if err = expect(err, authorization.OutcomeUnknown); err != nil {
		return err
	}
	if out != nil {
		return fmt.Errorf("delivered unknown material")
	}
	replay, err := h.g.LookupOperation(ctx, h.token, req.OperationId)
	if err != nil {
		return err
	}
	return require(replay.Material != "" && replay.Revision == 2, "lost committed material")
}
func signingRace(ctx context.Context, h *harness) error {
	g, err := h.s.SignedGrants(grantConfig(), afterSigner{GrantCrypto: h.crypto, after: func() error { return h.denyPolicy(ctx, 1) }})
	if err != nil {
		return err
	}
	req, err := h.request(ctx, "ISSUE", 1)
	if err != nil {
		return err
	}
	out, err := g.Mutate(ctx, h.token, req)
	if err = expect(err, authorization.Conflict); err != nil {
		return err
	}
	return require(out == nil, "delivered stale signed authorization")
}
func parallelCheck(ctx context.Context, h *harness) error {
	root, err := h.issue(ctx)
	if err != nil {
		return err
	}
	other, err := open(h.dir, h.token, h.c)
	if err != nil {
		return err
	}
	defer other.db.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, g := range []*authorization.GrantAuthority{h.g, other.g} {
		wg.Add(1)
		go func(i int, g *authorization.GrantAuthority) {
			defer wg.Done()
			p := h.present()
			p.OperationID = fmt.Sprintf("business-%d", i)
			p.SemanticSHA256 = strings.Repeat("a", 64)
			_, err := g.ReserveUse(ctx, root.Material, p, action(), 5)
			results <- err
		}(i, g)
	}
	wg.Wait()
	close(results)
	successful := 0
	for err := range results {
		if err == nil {
			successful++
		} else if !authorization.Is(err, authorization.Denied) {
			return err
		}
	}
	return require(successful == 1, "concurrent allocation exceeded pool")
}
