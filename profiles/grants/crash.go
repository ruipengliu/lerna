package grants

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var crashPoints = []string{"signed-before-commit", "grant-committed", "allocation-committed", "revoke-committed", "application-committed"}

type plan struct {
	Token   string
	Now     time.Time
	Request *wire.GrantMutation
	Spec    *wire.SignedGrantSpec
	Receipt *wire.GrantReceipt
}
type crashStore struct {
	authorization.Store
	point, id string
}

func (s *crashStore) Commit(ctx context.Context, v uint64, state authorization.State) error {
	err := s.Store.Commit(ctx, v, state)
	if err != nil {
		return err
	}
	if state.Signed != nil {
		switch s.point {
		case "grant-committed", "revoke-committed":
			if _, ok := state.Signed.Operations[s.id]; ok {
				os.Exit(73)
			}
		case "allocation-committed":
			if _, ok := state.Signed.Uses["business"]; ok {
				os.Exit(73)
			}
		case "application-committed":
			e, ok := state.Signed.Grants[s.id]
			if ok && len(e.Record.Notices) > 0 && e.Record.Notices[0].Applied {
				os.Exit(73)
			}
		}
	}
	return nil
}
func RunProbe(ctx context.Context, dir, point string) error {
	if !slices.Contains(crashPoints, point) {
		return fmt.Errorf("unknown probe")
	}
	data, err := os.ReadFile(filepath.Join(dir, "plan.json"))
	if err != nil {
		return err
	}
	var p plan
	if err = json.Unmarshal(data, &p); err != nil {
		return err
	}
	h, err := open(dir, p.Token, &clock{now: p.Now})
	if err != nil {
		return err
	}
	defer h.db.Close()
	h.spec = p.Spec
	id := p.Request.OperationId
	if point == "application-committed" {
		id = p.Receipt.GrantId
	}
	fault := &crashStore{Store: h.db, point: point, id: id}
	other, err := assemble(fault, dir, p.Token, h.c)
	if err != nil {
		return err
	}
	if point == "signed-before-commit" {
		other.g, err = other.s.SignedGrants(grantConfig(), afterSigner{GrantCrypto: h.crypto, after: func() error { os.Exit(73); return nil }})
		if err != nil {
			return err
		}
	}
	switch point {
	case "allocation-committed":
		present := h.present()
		present.OperationID = "business"
		present.SemanticSHA256 = strings.Repeat("a", 64)
		_, err = other.g.ReserveUse(ctx, p.Receipt.Material, present, action(), 3)
	case "application-committed":
		// Persist an independent receiver's applied revision before acknowledging it.
		data, _ := json.Marshal(p.Receipt.Revision)
		if err = os.WriteFile(filepath.Join(dir, "receiver-applied.json"), data, 0600); err != nil {
			return err
		}
		err = other.g.ConfirmApplied(ctx, h.present(), p.Receipt.GrantId, p.Receipt.Revision)
	default:
		_, err = other.g.Mutate(ctx, p.Token, p.Request)
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("probe did not terminate")
}
func crashCheck(ctx context.Context, executable, point string) error {
	dir, err := os.MkdirTemp("", "lerna-grant-crash-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	h, err := setup(ctx, dir)
	if err != nil {
		return err
	}
	req, err := h.request(ctx, "ISSUE", 1)
	if err != nil {
		h.db.Close()
		return err
	}
	now, _ := h.c.Now()
	p := plan{Token: h.token, Now: now, Spec: h.spec, Request: req}
	if point == "allocation-committed" || point == "revoke-committed" || point == "application-committed" {
		p.Receipt, err = h.g.Mutate(ctx, h.token, req)
		if err != nil {
			h.db.Close()
			return err
		}
	}
	if point == "revoke-committed" || point == "application-committed" {
		p.Request, err = h.request(ctx, "REVOKE", 2)
		if err != nil {
			h.db.Close()
			return err
		}
		p.Request.Spec = nil
		p.Request.GrantId = p.Receipt.GrantId
		p.Request.ExpectedGrantRevision = 2
		if point == "application-committed" {
			p.Receipt, err = h.g.Mutate(ctx, h.token, p.Request)
			if err != nil {
				h.db.Close()
				return err
			}
		}
	}
	// Proto JSON handles resource-selector oneofs; plan.json uses raw proto bytes
	// in a separate request/spec file instead of encoding Go interface objects.
	if err = writePlan(dir, p); err != nil {
		h.db.Close()
		return err
	}
	h.db.Close()
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, executable, "grant-crash-probe", dir, point)
	output, err := cmd.CombinedOutput()
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 73 {
		return fmt.Errorf("probe exit: %v %s", err, output)
	}
	h, err = open(dir, p.Token, &clock{now: p.Now})
	if err != nil {
		return err
	}
	defer h.db.Close()
	h.spec = p.Spec
	if point == "allocation-committed" {
		present := h.present()
		present.OperationID = "business"
		present.SemanticSHA256 = strings.Repeat("a", 64)
		use, err := h.g.LookupUse(ctx, present)
		if err != nil {
			return err
		}
		replay, err := h.g.ReserveUse(ctx, p.Receipt.Material, present, action(), 3)
		if err != nil {
			return err
		}
		if use != replay {
			return fmt.Errorf("allocation changed after crash")
		}
		return consumeFixture(ctx, dir, use)
	}
	if point == "application-committed" {
		data, err := os.ReadFile(filepath.Join(dir, "receiver-applied.json"))
		if err != nil {
			return err
		}
		var revision uint64
		if json.Unmarshal(data, &revision) != nil || revision != p.Receipt.Revision {
			return fmt.Errorf("missing applied fact")
		}
		if err = h.g.ConfirmApplied(ctx, h.present(), p.Receipt.GrantId, revision); err != nil {
			return err
		}
		record, err := h.g.Get(ctx, h.token, p.Receipt.GrantId)
		if err != nil {
			return err
		}
		return require(record.Revoked && record.Notices[0].Applied, "lost application")
	}
	receipt, err := h.g.LookupOperation(ctx, h.token, p.Request.OperationId)
	if point == "signed-before-commit" {
		if err = expect(err, authorization.NotFound); err != nil {
			return err
		}
		receipt, err = h.g.Mutate(ctx, h.token, p.Request)
	}
	if err != nil {
		return err
	}
	replay, err := h.g.Mutate(ctx, h.token, p.Request)
	if err != nil {
		return err
	}
	if receipt.Material != replay.Material || receipt.GrantId != replay.GrantId {
		return fmt.Errorf("recovery reissued grant")
	}
	if point == "revoke-committed" {
		record, err := h.g.Get(ctx, h.token, receipt.GrantId)
		if err != nil {
			return err
		}
		return require(record.Revoked && !record.Notices[0].Applied, "revoke lost or falsely applied")
	}
	_, err = h.g.Verify(ctx, receipt.Material, h.present(), action())
	return err
}
