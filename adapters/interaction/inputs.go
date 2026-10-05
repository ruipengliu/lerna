package interaction

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
)

// modify 修改运行中的任务：带上提交时看到的条件集版本和输入版本。
func (c *CLI) modify(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("modify", flag.ContinueOnError)
	fs.SetOutput(c.Out)
	session := fs.String("session", "", "会话")
	task := fs.String("task", "", "任务")
	template := fs.String("template", "", "新的任务模板")
	keep := fs.Bool("keep", false, "确认完成条件不变")
	p := params{}
	fs.Var(p, "param", "模板参数 k=v")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *session == "" || *task == "" {
		return errors.New("modify: --session and --task are required")
	}
	v, err := c.Core.Task(ctx, c.User, *task)
	if err != nil {
		return err
	}
	cmd := &lernav1.SubmitInputCommand{
		SessionId: *session, InputKind: lernav1.InputKind_INPUT_KIND_MODIFY_TASK, TaskId: *task, Text: strings.Join(fs.Args(), " "),
		KeepRequirements:            *keep,
		ExpectedRequirementsVersion: v.GetTask().GetRequirementsVersion(),
		ExpectedInputVersion:        v.GetTask().GetInputVersion(),
	}
	if *template != "" {
		cmd.Requirements = &lernav1.RequirementSetDraft{TemplateId: *template, TemplateParams: p}
	}
	if err := c.submit(ctx, "", ports.CommandSubmitInput, cmd, nil); err != nil {
		return err
	}
	fmt.Fprintln(c.Out, "修改已接纳：基于旧上下文的提议和尚未开始的动作不再执行")
	if err := c.settle(ctx); err != nil {
		return err
	}
	return c.showTask(ctx, *task)
}

// answer 回答核心发布的提问。
func (c *CLI) answer(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("answer", flag.ContinueOnError)
	fs.SetOutput(c.Out)
	session := fs.String("session", "", "会话")
	template := fs.String("template", "", "给出完成条件：任务模板")
	keep := fs.Bool("keep", false, "确认完成条件不变")
	p := params{}
	fs.Var(p, "param", "模板参数 k=v")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *session == "" || fs.NArg() < 1 {
		return errors.New("answer: want --session <会话> <提问标识> [回答]")
	}
	cmd := &lernav1.SubmitInputCommand{
		SessionId: *session, InputKind: lernav1.InputKind_INPUT_KIND_ANSWER, RequestId: fs.Arg(0),
		Text: strings.Join(fs.Args()[1:], " "), KeepRequirements: *keep,
	}
	if *template != "" {
		cmd.Requirements = &lernav1.RequirementSetDraft{TemplateId: *template, TemplateParams: p}
	}
	if err := c.submit(ctx, "", ports.CommandSubmitInput, cmd, nil); err != nil {
		return err
	}
	if err := c.settle(ctx); err != nil {
		return err
	}
	return c.showSession(ctx, *session)
}
