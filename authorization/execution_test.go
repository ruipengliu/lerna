package authorization_test

import (
	"context"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"strings"
	"testing"
	"time"
)

func TestExecutionPermitAndDataCommitTogether(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	op, e := f.s.NewOperation(ctx, f.token)
	if e != nil {
		t.Fatal(e)
	}
	f.spec.Mode = "single"
	f.spec.Units = 1
	f.spec.DelegationDepth = 0
	f.spec.OperationBinding = op
	f.spec.SemanticSha256 = strings.Repeat("a", 64)
	grant, e := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
	if e != nil {
		t.Fatal(e)
	}
	p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Audience: "receiver", Presenter: "node", CertificateSHA256: f.spec.CertificateSha256, OperationID: op, SemanticSHA256: f.spec.SemanticSha256}
	action := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
	permit, e := f.g.ReserveUse(ctx, grant.Material, p, action, 1)
	if e != nil {
		t.Fatal(e)
	}
	e = f.g.UpdateExecution(ctx, func(tx authorization.ExecutionTransaction) error {
		if err := tx.ValidateUse(permit, p, action); err != nil {
			return err
		}
		if err := tx.ExecutionOperation(op, "admin", true); err != nil {
			return err
		}
		tx.SetExecutionData([]byte("started"))
		return &authorization.Error{Code: authorization.Invalid}
	})
	if !authorization.Is(e, authorization.Invalid) {
		t.Fatal(e)
	}
	e = f.g.UpdateExecution(ctx, func(tx authorization.ExecutionTransaction) error {
		if len(tx.ExecutionData()) != 0 {
			t.Fatal("partial startup")
		}
		if err := tx.ValidateUse(permit, p, action); err != nil {
			return err
		}
		if err := tx.ExecutionOperation(op, "admin", true); err != nil {
			return err
		}
		tx.SetExecutionData([]byte("started"))
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	e = f.s.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error { return tx.Operation(op, "admin", true) })
	if !authorization.Is(e, authorization.IdentityConflict) {
		t.Fatalf("execution ID reused: %v", e)
	}
}

func TestReservedExecutionIdentitySurvivesWindowWithoutSpendingPermit(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	op, err := f.s.NewOperation(ctx, f.token)
	if err != nil {
		t.Fatal(err)
	}
	reserve := func(subject string) error {
		return f.g.UpdateExecution(ctx, func(tx authorization.ExecutionTransaction) error { return tx.ReserveExecutionOperation(op, subject) })
	}
	if err = reserve("admin"); err != nil {
		t.Fatal(err)
	}
	if err = reserve("other"); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("other subject: %v", err)
	}
	if err = f.s.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error { return tx.Operation(op, "admin", true) }); !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatalf("runtime reuse: %v", err)
	}
	if err = f.g.UpdateExecution(ctx, func(tx authorization.ExecutionTransaction) error { return tx.ExecutionControlOperation(op, "admin") }); !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatalf("control reuse: %v", err)
	}
	f.clock.now = f.clock.now.Add(2 * time.Minute)
	if err = reserve("admin"); err != nil {
		t.Fatalf("reserved identity expired: %v", err)
	}
	f.spec.Mode = "single"
	f.spec.Units = 1
	f.spec.DelegationDepth = 0
	f.spec.OperationBinding = op
	f.spec.SemanticSha256 = strings.Repeat("a", 64)
	grant, err := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
	if err != nil {
		t.Fatal(err)
	}
	p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Audience: "receiver", Presenter: "node", CertificateSHA256: f.spec.CertificateSha256, OperationID: op, SemanticSHA256: f.spec.SemanticSha256}
	action := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
	use, err := f.g.ReserveUse(ctx, grant.Material, p, action, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.g.UpdateExecution(ctx, func(tx authorization.ExecutionTransaction) error {
		if e := tx.ValidateUse(use, p, action); e != nil {
			return e
		}
		return tx.ExecutionOperation(op, "admin", true)
	}); err != nil {
		t.Fatal(err)
	}
	if err = reserve("admin"); !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatalf("claimed operation reserved again: %v", err)
	}
	replay, err := f.g.ReserveUse(ctx, grant.Material, p, action, 1)
	if err != nil || replay != use {
		t.Fatalf("permit replay: %v", err)
	}
	p.SemanticSHA256 = strings.Repeat("b", 64)
	if _, err = f.g.ReserveUse(ctx, grant.Material, p, action, 1); err == nil {
		t.Fatal("changed payload accepted")
	}
}
