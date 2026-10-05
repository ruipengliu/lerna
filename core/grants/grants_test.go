package grants_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/grants"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

func domain(t *testing.T) (*durable.Domain, *clock) {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "adj.db"), "adjudication", sqlite.LocalProfile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	c := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	d := durable.NewDomain("adjudication", db, c)
	(&grants.Module{Domain: d}).Register()
	return d, c
}

func clause(purposes ...lernav1.ProcessingPurpose) *lernav1.GrantClause {
	return &lernav1.GrantClause{
		Resource: "api.put", Actions: []string{grants.ActionInvoke},
		UseRights: []lernav1.UseRight{lernav1.UseRight_USE_RIGHT_ACT}, ProcessingPurposes: purposes,
	}
}

func issue(t *testing.T, d *durable.Domain, c *clock, cmd *lernav1.IssueGrantCommand) *lernav1.Receipt {
	t.Helper()
	if cmd.ValidUntil == nil {
		cmd.ValidUntil = timestamppb.New(c.t.Add(time.Hour))
	}
	env, err := durable.NewEnvelope(&lernav1.CommandIdentity{UserId: "u", IssuerId: "cli", TargetDomainId: "adjudication", CommandId: t.Name() + time.Now().String()},
		ports.CommandIssueGrant, cmd)
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.Execute(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// 委派、组合多个来源返回"不支持"；处理目的只用"当前任务"，其他取值和未知值拒绝。
//
// 规则：G7、G8
func TestM1RejectsDelegationAndUnsupportedPurposes(t *testing.T) {
	d, c := domain(t)
	cur := lernav1.ProcessingPurpose_PROCESSING_PURPOSE_CURRENT_TASK
	for name, tc := range map[string]struct {
		cmd  *lernav1.IssueGrantCommand
		code lernav1.ErrorCode
	}{
		"delegation": {&lernav1.IssueGrantCommand{Clauses: []*lernav1.GrantClause{clause(cur)}, UseMode: lernav1.UseMode_USE_MODE_STANDING, ParentGrantRef: "g0"}, lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_FEATURE},
		"combine":    {&lernav1.IssueGrantCommand{Clauses: []*lernav1.GrantClause{clause(cur)}, UseMode: lernav1.UseMode_USE_MODE_STANDING, CombineFrom: []string{"a", "b"}}, lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_FEATURE},
		"evaluation": {&lernav1.IssueGrantCommand{Clauses: []*lernav1.GrantClause{clause(lernav1.ProcessingPurpose_PROCESSING_PURPOSE_EVALUATION)}, UseMode: lernav1.UseMode_USE_MODE_STANDING}, lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_FEATURE},
		"unknown":    {&lernav1.IssueGrantCommand{Clauses: []*lernav1.GrantClause{clause(lernav1.ProcessingPurpose(42))}, UseMode: lernav1.UseMode_USE_MODE_STANDING}, lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT},
		"no purpose": {&lernav1.IssueGrantCommand{Clauses: []*lernav1.GrantClause{clause()}, UseMode: lernav1.UseMode_USE_MODE_STANDING}, lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT},
	} {
		r := issue(t, d, c, tc.cmd)
		if r.GetDecision() != lernav1.Decision_DECISION_REJECTED || r.GetRejection().GetCode() != tc.code {
			t.Fatalf("%s: want %s, got %v", name, tc.code, r)
		}
	}
}

// 单次授权在准入时被占用一次；出口核验不再要求剩余次数，否则一次性授权会被自己的占用拒绝。
//
// 规则：准入-7、开始-3
func TestSingleGrantOccupiedOnceAndEgressIgnoresRemainingCount(t *testing.T) {
	d, c := domain(t)
	r := issue(t, d, c, &lernav1.IssueGrantCommand{
		Clauses: []*lernav1.GrantClause{clause(lernav1.ProcessingPurpose_PROCESSING_PURPOSE_CURRENT_TASK)}, UseMode: lernav1.UseMode_USE_MODE_SINGLE,
	})
	res := &lernav1.IssueGrantResult{}
	if err := durable.ResultOf(r, res); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	req := func(op string) grants.UseRequest {
		return grants.UseRequest{User: "u", TaskID: "t", OperationID: op, AdmissionID: "a-" + op, Resource: "api.put",
			Action: grants.ActionInvoke, RequestDigest: []byte("d")}
	}
	var cred string
	if err := d.Write(ctx, "x", func(tx *durable.Tx) error {
		g, use, err := grants.OccupyForAdmission(tx, req("op1"))
		if err != nil {
			return err
		}
		cred, err = grants.IssueCredential(tx, "u", "op1", g, use, "local", []byte("d"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err := d.Write(ctx, "y", func(tx *durable.Tx) error {
		_, _, err := grants.OccupyForAdmission(tx, req("op2"))
		return err
	})
	if !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED) {
		t.Fatalf("a used-up single grant must not admit a second operation: %v", err)
	}
	if err := d.Read(ctx, func(tx *durable.Tx) error {
		return grants.EgressCheck(tx, "u", cred, "op1", "local", []byte("d"))
	}); err != nil {
		t.Fatalf("egress for the occupied operation must pass: %v", err)
	}
	if err := d.Read(ctx, func(tx *durable.Tx) error {
		return grants.EgressCheck(tx, "u", cred, "op1", "local", []byte("other"))
	}); !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED) {
		t.Fatalf("egress with a different request digest must fail: %v", err)
	}
	c.t = c.t.Add(2 * time.Hour)
	if err := d.Read(ctx, func(tx *durable.Tx) error {
		return grants.EgressCheck(tx, "u", cred, "op1", "local", []byte("d"))
	}); err == nil {
		t.Fatal("egress after the grant expired must fail")
	}
}
