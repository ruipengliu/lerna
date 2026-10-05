// Package content 保存输入正文及其可信来源，其他模块只保存引用。
package content

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type Store interface {
	StageContent(context.Context, *v1.Content, string) (*v1.Ref, error)
	ReadContent(context.Context, *v1.Ref) (*v1.Content, error)
}
type Service struct {
	store        Store
	user, domain string
}

func New(s Store, user, domain string) *Service { return &Service{s, user, domain} }
func (s *Service) Stage(ctx context.Context, caller *v1.Caller, c *v1.SubmitGoalCommand) (*v1.Ref, error) {
	if err := command.ValidateGoal(c); err != nil {
		return nil, err
	}
	if err := command.CheckIdentity(caller, c.Identity, s.user, c.Identity.TargetDomainId); err != nil {
		return nil, err
	}
	return s.store.StageContent(ctx, &v1.Content{Ref: command.NewRef(s.user, s.domain, "content", "lerna.v1.Content"), Text: c.Goal, Source: c.Identity, MediaType: "text/plain", Status: "AVAILABLE", ProcessingPurposes: []string{"CURRENT_TASK"}}, command.FingerprintV1(c))
}
func (s *Service) Read(ctx context.Context, caller *v1.Caller, ref *v1.Ref) (*v1.Content, error) {
	if ref == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if err := command.CheckName(caller, ref.Name, s.user, s.domain, "content"); err != nil {
		return nil, err
	}
	if ref.Revision != 1 || ref.SchemaId != "lerna.v1.Content" {
		return nil, command.Fail("UNSUPPORTED_CONTRACT")
	}
	return s.store.ReadContent(ctx, ref)
}

// CheckUsable 检查精确内容版本、可信来源和当前用途；不得凭一个形状正确的引用放行。
func (s *Service) CheckUsable(ctx context.Context, caller *v1.Caller, ref *v1.Ref) error {
	c, e := s.Read(ctx, caller, ref)
	if e != nil {
		return e
	}
	if c == nil || c.Source == nil || c.Source.UserId != s.user || c.Source.IssuerId == "" || c.Source.CommandId == "" || c.Status != "AVAILABLE" || !proto.Equal(c.Ref, ref) {
		return command.Fail("CONTENT_UNUSABLE")
	}
	for _, purpose := range c.ProcessingPurposes {
		if purpose == "CURRENT_TASK" {
			return nil
		}
	}
	return command.Fail("CONTENT_USE_DENIED")
}
