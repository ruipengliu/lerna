package interaction

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
)

// grant 签发授权：持续授权需要明确的主体、资源、动作、范围和期限。
func (c *CLI) grant(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "show" {
		if len(args) < 2 {
			return errors.New("grant show: want a grant id")
		}
		return c.showGrant(ctx, args[1])
	}
	fs := flag.NewFlagSet("grant", flag.ContinueOnError)
	fs.SetOutput(c.Out)
	capability := fs.String("capability", "", "能力标识（资源）")
	days := fs.Int("days", 7, "有效期（天）")
	single := fs.Bool("single", false, "单次授权（只准入一次动作）")
	task := fs.String("task", "", "只限这个任务")
	p := params{}
	fs.Var(p, "param", "参数必须等于 k=v，可重复")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *capability == "" {
		return errors.New("grant: --capability is required")
	}
	mode := lernav1.UseMode_USE_MODE_STANDING
	if *single {
		mode = lernav1.UseMode_USE_MODE_SINGLE
	}
	cmd := &lernav1.IssueGrantCommand{
		Clauses: []*lernav1.GrantClause{{
			Resource:           *capability,
			Actions:            []string{"invoke", "query"},
			UseRights:          []lernav1.UseRight{lernav1.UseRight_USE_RIGHT_ACT, lernav1.UseRight_USE_RIGHT_READ},
			ProcessingPurposes: []lernav1.ProcessingPurpose{lernav1.ProcessingPurpose_PROCESSING_PURPOSE_CURRENT_TASK},
			ParamEquals:        p,
			TaskId:             *task,
		}},
		UseMode:    mode,
		ValidUntil: timestamppb.New(time.Now().Add(time.Duration(*days) * 24 * time.Hour)),
	}
	res := &lernav1.IssueGrantResult{}
	if err := c.submit(ctx, "", ports.CommandIssueGrant, cmd, res); err != nil {
		return err
	}
	fmt.Fprintf(c.Out, "授权已签发：%s（%s，%s 的调用和核对查询，处理目的：当前任务）\n", res.GetGrantId(), mode, *capability)
	return c.settle(ctx)
}

func (c *CLI) revoke(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return errors.New("revoke: want a grant id")
	}
	res := &lernav1.RevocationProgress{}
	if err := c.submit(ctx, "", ports.CommandRevokeGrant, &lernav1.RevokeGrantCommand{GrantId: args[0]}, res); err != nil {
		return err
	}
	if err := c.settle(ctx); err != nil {
		return err
	}
	return c.showGrant(ctx, args[0])
}

func (c *CLI) showGrant(ctx context.Context, id string) error {
	v, err := c.Core.Grant(ctx, c.User, id)
	if err != nil {
		return err
	}
	g := v.GetGrant()
	state := "有效"
	switch {
	case g.GetStatus() == lernav1.GrantStatus_GRANT_STATUS_REVOKED &&
		v.GetRevocation().GetCompletion() == lernav1.RevocationCompletion_REVOCATION_COMPLETION_COMPLETE:
		state = "已撤销"
	case g.GetStatus() == lernav1.GrantStatus_GRANT_STATUS_REVOKED:
		state = fmt.Sprintf("撤销处理中（仍有 %d 个出口待封闭）", len(v.GetRevocation().GetOpenOperations()))
	case g.GetExpired():
		state = "已过期"
	case g.GetMaxAdmissions() > 0 && g.GetUses() >= g.GetMaxAdmissions():
		state = "已用完"
	}
	fmt.Fprintf(c.Out, "授权 %s：%s，已使用 %d 次\n", id, state, g.GetUses())
	return nil
}

// confirm 回应一个确认事项：展示核心生成的描述，提交时带上看到的事项摘要。
func (c *CLI) confirm(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("confirm", flag.ContinueOnError)
	fs.SetOutput(c.Out)
	session := fs.String("session", "", "确认所在的会话")
	deny := fs.Bool("deny", false, "拒绝")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 || *session == "" {
		return errors.New("confirm: want --session <会话> <确认标识>")
	}
	v, err := c.Core.Session(ctx, c.User, *session)
	if err != nil {
		return err
	}
	var item *lernav1.Confirmation
	for _, x := range v.GetPendingConfirmations() {
		if x.GetConfirmationId() == fs.Arg(0) {
			item = x
		}
	}
	if item == nil {
		return fmt.Errorf("confirm: no pending confirmation %s in session %s", fs.Arg(0), *session)
	}
	fmt.Fprintf(c.Out, "确认事项：%s\n", item.GetDescription())
	cmd := &lernav1.SubmitInputCommand{
		SessionId: *session, InputKind: lernav1.InputKind_INPUT_KIND_CONFIRMATION, RequestId: item.GetConfirmationId(),
		Approve: !*deny, IntentFingerprint: item.GetIntentFingerprint(),
	}
	if err := c.submit(ctx, "", ports.CommandSubmitInput, cmd, nil); err != nil {
		return err
	}
	if *deny {
		fmt.Fprintln(c.Out, "已拒绝")
	} else {
		fmt.Fprintln(c.Out, "已批准（批准不等于已准入或已执行）")
	}
	if err := c.settle(ctx); err != nil {
		return err
	}
	return c.showSession(ctx, *session)
}

func (c *CLI) budget(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "set" {
		fs := flag.NewFlagSet("budget set", flag.ContinueOnError)
		fs.SetOutput(c.Out)
		limit := fs.Int64("limit", 0, "用户级上限（micro_usd）")
		version := fs.Int64("version", 0, "预期的额度版本；新建时为 0")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		res := &lernav1.SetBudgetResult{}
		if err := c.submit(ctx, "", ports.CommandSetBudget, &lernav1.SetBudgetCommand{
			Scope: lernav1.BudgetScope_BUDGET_SCOPE_USER, Limit: *limit, ExpectedLimitVersion: *version,
		}, res); err != nil {
			return err
		}
		fmt.Fprintf(c.Out, "预算 %s 的额度版本 %d\n", res.GetBudgetId(), res.GetLimitVersion())
	}
	v, err := c.Core.Budget(ctx, c.User)
	if err != nil {
		return err
	}
	for _, b := range v.GetBudgets() {
		scope := "用户"
		if b.GetScope() == lernav1.BudgetScope_BUDGET_SCOPE_TASK {
			scope = "任务 " + b.GetTaskId()
		}
		fmt.Fprintf(c.Out, "%s：上限 %d，已消耗 %d，预留 %d，可用 %d，超支 %d，未知费用来源 %d（%s）\n",
			scope, b.GetLimit(), b.GetUsed(), b.GetHeld(), b.GetAvailable(), b.GetDeficit(), b.GetUnknownSources(), b.GetUnit())
	}
	return nil
}
