// Package interaction 是默认的交互适配器：命令行（交互适配器接口，docs/architecture/ports/interaction/README.md）。
//
// 只通过公共契约命令与核心交互；如实呈现中间状态，例如"取消已保存，部分动作仍在核对"。
// 它只依赖 contracts，不依赖核心的内部实现。
package interaction

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/errs"
	"github.com/ruipengliu/lerna/contracts/fingerprint"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/contracts/ports"
)

// CLI 是命令行交互适配器。
type CLI struct {
	Core   ports.Core
	User   string
	Issuer string
	// Domain 是接收用户命令的裁决域。
	Domain string
	Out    io.Writer
	// Settle 在改变状态的命令之后推进宿主的待办工作（本地绑定）；可以为空。
	Settle func(ctx context.Context) error
}

// Run 执行一条命令行。args 不含程序名。
func (c *CLI) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		c.usage()
		return errors.New("missing command")
	}
	switch args[0] {
	case "goal":
		return c.goal(ctx, args[1:])
	case "session":
		return c.session(ctx, args[1:])
	case "task":
		return c.task(ctx, args[1:])
	case "receipt":
		return c.receipt(ctx, args[1:])
	case "grant":
		return c.grant(ctx, args[1:])
	case "revoke":
		return c.revoke(ctx, args[1:])
	case "confirm":
		return c.confirm(ctx, args[1:])
	case "budget":
		return c.budget(ctx, args[1:])
	case "modify":
		return c.modify(ctx, args[1:])
	case "answer":
		return c.answer(ctx, args[1:])
	case "run":
		return c.settle(ctx)
	case "help", "-h", "--help":
		c.usage()
		return nil
	default:
		c.usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (c *CLI) usage() {
	fmt.Fprint(c.Out, `用法：lerna [--data 目录] [--user 用户] <命令>

  goal [--session 会话] [--template 模板 --param k=v ...] [--budget 上限] [--command-id 标识] 目标文字
                                 提交新目标；重试同一命令时带上原 --command-id
  session new                    创建一个暂不关联任务的会话
  session show <会话>             查看会话的全量快照
  task show <任务>                查看任务的全量快照和当前阶段
  receipt <命令标识>              用原命令标识查询决定
  grant --capability 能力 [--days N] [--single] [--task 任务] [--param k=v ...]
                                 签发授权（处理目的：当前任务）
  grant show <授权>               查看授权和撤销进度
  revoke <授权>                   撤销授权：新准入立即被拒绝，出口封闭后才算完成
  confirm --session 会话 [--deny] <确认标识>
                                 回应核心生成的确认事项
  modify --session 会话 --task 任务 [--template 模板 --param k=v ... | --keep] 说明
                                 修改运行中的任务
  answer --session 会话 [--template 模板 --param k=v ... | --keep] <提问标识> 回答
                                 回答核心的提问
  budget [set --limit N [--version V]]
                                 查看或设定用户级费用上限
  run                            推进待办工作
`)
}

type params map[string]string

func (p params) String() string { return fmt.Sprint(map[string]string(p)) }

func (p params) Set(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok {
		return fmt.Errorf("want k=v, got %q", s)
	}
	p[k] = v
	return nil
}

func (c *CLI) goal(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("goal", flag.ContinueOnError)
	fs.SetOutput(c.Out)
	session := fs.String("session", "", "会话标识；为空时新建会话")
	template := fs.String("template", "", "受信任务模板："+"api_put、file_write")
	cmdID := fs.String("command-id", "", "命令标识；重试同一命令时复用")
	budget := fs.Int64("budget", 100_000, "当前任务的明确费用上限（micro_usd）")
	p := params{}
	fs.Var(p, "param", "模板参数 k=v，可重复")
	if err := fs.Parse(args); err != nil {
		return err
	}
	text := strings.Join(fs.Args(), " ")
	cmd := &lernav1.SubmitInputCommand{
		SessionId:  *session,
		InputKind:  lernav1.InputKind_INPUT_KIND_NEW_GOAL,
		Text:       text,
		TaskBudget: *budget,
	}
	if *template != "" {
		cmd.Requirements = &lernav1.RequirementSetDraft{TemplateId: *template, TemplateParams: p}
	}
	res := &lernav1.SubmitInputResult{}
	if err := c.submit(ctx, *cmdID, ports.CommandSubmitInput, cmd, res); err != nil {
		return err
	}
	fmt.Fprintf(c.Out, "会话 %s：输入 #%d 已记录（%s）\n", res.GetSessionId(), res.GetSessionSeq(), routing(res.GetRoutingStatus()))
	if err := c.settle(ctx); err != nil {
		return err
	}
	return c.showSession(ctx, res.GetSessionId())
}

// submit 提交命令；结果未知时用原命令身份查询，查不到再用原标识、原内容重投，不换新标识。
func (c *CLI) submit(ctx context.Context, cmdID, kind string, payload, result proto.Message) error {
	if cmdID == "" {
		cmdID = ids.New()
	}
	fmt.Fprintf(c.Out, "命令标识：%s\n", cmdID)
	id := &lernav1.CommandIdentity{UserId: c.User, IssuerId: c.Issuer, TargetDomainId: c.Domain, CommandId: cmdID}
	env, err := envelope(id, kind, payload)
	if err != nil {
		return err
	}
	var rec *lernav1.Receipt
	for attempt := 0; attempt < 3; attempt++ {
		rec, err = c.Core.Submit(ctx, env)
		if err == nil || !errs.IsIndeterminate(err) {
			break
		}
		fmt.Fprintf(c.Out, "提交结果未知（%v），按原命令标识查询\n", err)
		q, qerr := c.Core.QueryCommand(ctx, id)
		if qerr == nil && q.GetOutcome() == lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_DECIDED {
			rec, err = q.GetReceipt(), nil
			break
		}
	}
	if err != nil {
		if errs.IsIndeterminate(err) {
			return fmt.Errorf("提交结果仍未知：请稍后用 `lerna receipt %s` 查询，不要换新标识重试：%w", cmdID, err)
		}
		return err
	}
	if rec.GetDecision() != lernav1.Decision_DECISION_ACCEPTED {
		return fmt.Errorf("命令被拒绝：%v", errs.FromProto(rec.GetRejection()))
	}
	if result != nil {
		return proto.Unmarshal(rec.GetResult(), result)
	}
	return nil
}

func envelope(id *lernav1.CommandIdentity, kind string, payload proto.Message) (*lernav1.CommandEnvelope, error) {
	body, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		return nil, err
	}
	fp, err := fingerprint.Of(kind, payload)
	if err != nil {
		return nil, err
	}
	return &lernav1.CommandEnvelope{
		Identity:           id,
		CommandKind:        kind,
		ContractVersion:    ports.ContractVersion,
		SchemaId:           string(payload.ProtoReflect().Descriptor().FullName()),
		Payload:            body,
		FingerprintVersion: fingerprint.Version,
		Fingerprint:        fp,
	}, nil
}

func (c *CLI) settle(ctx context.Context) error {
	if c.Settle == nil {
		return nil
	}
	return c.Settle(ctx)
}

func (c *CLI) session(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("session: want new or show")
	}
	switch args[0] {
	case "new":
		res := &lernav1.CreateSessionResult{}
		if err := c.submit(ctx, "", ports.CommandCreateSession, &lernav1.CreateSessionCommand{}, res); err != nil {
			return err
		}
		fmt.Fprintf(c.Out, "会话已创建：%s\n", res.GetSessionId())
		return nil
	case "show":
		if len(args) < 2 {
			return errors.New("session show: want a session id")
		}
		return c.showSession(ctx, args[1])
	default:
		return fmt.Errorf("session: unknown subcommand %q", args[0])
	}
}

func (c *CLI) showSession(ctx context.Context, sid string) error {
	v, err := c.Core.Session(ctx, c.User, sid)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.Out, "会话 %s（输入 %d 条）\n", sid, len(v.GetInputs()))
	for _, in := range v.GetInputs() {
		task := ""
		if in.GetTaskId() != "" {
			task = " → 任务 " + in.GetTaskId()
		}
		fmt.Fprintf(c.Out, "  #%d %s：%s%s\n", in.GetSessionSeq(), inputKind(in.GetInputKind()), routing(in.GetRoutingStatus()), task)
	}
	for _, r := range v.GetOpenRequests() {
		fmt.Fprintf(c.Out, "  待回答 %s：%s\n", r.GetRequestId(), r.GetQuestion())
	}
	for _, cf := range v.GetPendingConfirmations() {
		fmt.Fprintf(c.Out, "  待确认 %s：%s\n", cf.GetConfirmationId(), cf.GetDescription())
	}
	for _, t := range v.GetTaskIds() {
		if err := c.showTask(ctx, t); err != nil {
			return err
		}
	}
	return nil
}

func (c *CLI) task(ctx context.Context, args []string) error {
	if len(args) < 2 || args[0] != "show" {
		return errors.New("task: want `task show <id>`")
	}
	return c.showTask(ctx, args[1])
}

func (c *CLI) showTask(ctx context.Context, taskID string) error {
	v, err := c.Core.Task(ctx, c.User, taskID)
	if err != nil {
		return err
	}
	t := v.GetTask()
	fmt.Fprintf(c.Out, "任务 %s：%s\n", taskID, phase(v.GetPhase()))
	fmt.Fprintf(c.Out, "  条件集 v%d（%s），输入版本 %d，控制代次 %d\n",
		t.GetRequirementsVersion(), reqStatus(v.GetRequirements().GetStatus()), t.GetInputVersion(), t.GetControlGeneration())
	for _, r := range v.GetRequirements().GetRequirements() {
		fmt.Fprintf(c.Out, "    - %s %s\n", r.GetRequirementId(), r.GetDescription())
	}
	for _, w := range t.GetWaitingOn() {
		fmt.Fprintf(c.Out, "  等待：%s\n", w.GetGap())
	}
	ops := append([]*lernav1.OperationView(nil), v.GetOperations()...)
	sort.Slice(ops, func(i, j int) bool { return ops[i].GetOperationId() < ops[j].GetOperationId() })
	for _, op := range ops {
		fmt.Fprintf(c.Out, "  动作 %s %s：效果%s，%s，%s\n", op.GetOperationId(), op.GetCapabilityId(),
			effect(op.GetEffect()), late(op.GetLateEffect()), settled(op))
	}
	if r := v.GetResult(); r != nil {
		fmt.Fprintf(c.Out, "  结果：%s（%s）\n", outcome(r.GetOutcome()), r.GetCloseReason())
		for _, e := range r.GetRequirementEvaluations() {
			fmt.Fprintf(c.Out, "    核验 %s：%s 证据 %v\n", e.GetRequirementId(), verdict(e.GetVerdict()), e.GetEvidenceRefs())
		}
		for _, u := range r.GetUncertainties() {
			fmt.Fprintf(c.Out, "    遗留未知：动作 %s（负责方 %s）效果%s，%s\n", u.GetOperationId(), u.GetLedgerDomainId(), effect(u.GetEffect()), late(u.GetLateEffect()))
		}
	}
	return nil
}

func (c *CLI) receipt(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return errors.New("receipt: want a command id")
	}
	q, err := c.Core.QueryCommand(ctx, &lernav1.CommandIdentity{UserId: c.User, IssuerId: c.Issuer, TargetDomainId: c.Domain, CommandId: args[0]})
	if err != nil {
		return err
	}
	switch q.GetOutcome() {
	case lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_NOT_FOUND:
		fmt.Fprintln(c.Out, "尚未发现这条命令（不等于确定没提交）")
	case lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_UNAVAILABLE:
		fmt.Fprintln(c.Out, "暂时无法查询（不等于没执行）")
	case lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_DECIDED:
		r := q.GetReceipt()
		if r.GetDecision() == lernav1.Decision_DECISION_ACCEPTED {
			fmt.Fprintf(c.Out, "已接受（%s，提交位置 %d）\n", r.GetCommandKind(), r.GetCommitPosition())
		} else {
			fmt.Fprintf(c.Out, "已拒绝：%v\n", errs.FromProto(r.GetRejection()))
		}
	default:
		fmt.Fprintln(c.Out, q.GetOutcome().String())
	}
	return nil
}
