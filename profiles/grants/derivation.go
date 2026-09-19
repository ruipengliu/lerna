package grants

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/authorization/josegrant"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"sync"
	"time"
)

func signerConfiguration(h *harness) error {
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	for _, keys := range []map[string]*ecdsa.PublicKey{nil, {}, {"other": &h.key.PublicKey}, {"signer-1": &other.PublicKey}} {
		if _, err = josegrant.New("signer-1", h.key, keys); err == nil {
			return fmt.Errorf("invalid key mapping enabled")
		}
	}
	return nil
}
func signingRevokeRace(ctx context.Context, h *harness) error {
	root, err := h.issue(ctx)
	if err != nil {
		return err
	}
	other, err := open(h.dir, h.token, h.c)
	if err != nil {
		return err
	}
	defer other.db.Close()
	revoke, err := h.request(ctx, "REVOKE", 2)
	if err != nil {
		return err
	}
	revoke.Spec = nil
	revoke.GrantId = root.GrantId
	revoke.ExpectedGrantRevision = 2
	g, err := h.s.SignedGrants(grantConfig(), afterSigner{GrantCrypto: h.crypto, after: func() error { _, err := other.g.Mutate(ctx, h.token, revoke); return err }})
	if err != nil {
		return err
	}
	req, err := h.request(ctx, "DERIVE", 2)
	if err != nil {
		return err
	}
	req.GrantId = root.GrantId
	req.ExpectedGrantRevision = 2
	req.Spec.Units = 5
	req.Spec.DelegationDepth = 2
	out, err := g.Mutate(ctx, h.token, req)
	if err = expect(err, authorization.Conflict); err != nil {
		return err
	}
	if out != nil {
		return fmt.Errorf("delivered revoked-parent material")
	}
	parent, err := h.g.Get(ctx, h.token, root.GrantId)
	if err != nil {
		return err
	}
	if !parent.Revoked || parent.Allocated != 0 {
		return fmt.Errorf("partial revoked-parent allocation")
	}
	_, err = h.g.LookupOperation(ctx, h.token, req.OperationId)
	return expect(err, authorization.NotFound)
}
func parallelDerive(ctx context.Context, h *harness) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	root, err := h.issue(ctx)
	if err != nil {
		return err
	}
	other, err := open(h.dir, h.token, h.c)
	if err != nil {
		return err
	}
	defer other.db.Close()
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	sign := afterSigner{GrantCrypto: h.crypto, after: func() error {
		ready <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	reqs := []*wire.GrantMutation{}
	authorities := []*authorization.GrantAuthority{}
	for _, s := range []*authorization.Service{h.s, other.s} {
		g, err := s.SignedGrants(grantConfig(), sign)
		if err != nil {
			return err
		}
		authorities = append(authorities, g)
		req, err := h.request(ctx, "DERIVE", 2)
		if err != nil {
			return err
		}
		req.GrantId = root.GrantId
		req.ExpectedGrantRevision = 2
		req.Spec.Units = 5
		req.Spec.DelegationDepth = 2
		reqs = append(reqs, req)
	}
	type result struct {
		out *wire.GrantReceipt
		err error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i, g := range authorities {
		wg.Add(1)
		go func(i int, g *authorization.GrantAuthority) {
			defer wg.Done()
			out, err := g.Mutate(ctx, h.token, reqs[i])
			results <- result{out, err}
		}(i, g)
	}
	for range 2 {
		select {
		case <-ready:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	close(release)
	wg.Wait()
	close(results)
	success := 0
	for r := range results {
		if r.err == nil {
			success++
			if r.out == nil || r.out.Material == "" {
				return fmt.Errorf("missing confirmed material")
			}
		} else {
			if !authorization.Is(r.err, authorization.Conflict) || r.out != nil {
				return fmt.Errorf("unexpected concurrent derivation: %v", r.err)
			}
		}
	}
	record, err := h.g.Get(ctx, h.token, root.GrantId)
	if err != nil {
		return err
	}
	return require(success == 1 && record.Allocated == 5, "concurrent derive overallocated or delivered uncertain material")
}
func resourceAttenuation(ctx context.Context, h *harness) error {
	mutate := func(cmd *wire.AuthorizationCommand) error {
		id, err := h.s.NewOperation(ctx, h.token)
		if err != nil {
			return err
		}
		_, err = h.s.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: id, Command: cmd})
		return err
	}
	if err := mutate(&wire.AuthorizationCommand{ExpectedRevision: 1, Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: "child", Parent: "root"}}}); err != nil {
		return err
	}
	broad := proto.Clone(h.spec.Scope).(*wire.AuthorizationScope)
	broad.Resources = &wire.ResourceSelector{Selection: &wire.ResourceSelector_Subtree{Subtree: "root"}}
	if err := mutate(&wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "broad", Scope: broad}}}}}); err != nil {
		return err
	}
	req, err := h.request(ctx, "ISSUE", 3)
	if err != nil {
		return err
	}
	root, err := h.g.Mutate(ctx, h.token, req)
	if err != nil {
		return err
	}
	req, err = h.request(ctx, "DERIVE", 4)
	if err != nil {
		return err
	}
	req.GrantId = root.GrantId
	req.ExpectedGrantRevision = 4
	req.Spec.DelegationDepth = 2
	req.Spec.Units = 1
	req.Spec.Scope.Resources = &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "child"}}
	_, err = h.g.Mutate(ctx, h.token, req)
	return expect(err, authorization.Denied)
}
