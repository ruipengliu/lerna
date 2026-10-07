// Package content 保存输入正文及其可信来源，其他模块只保存引用。
package content

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
)

// Store 声明内容登记、正文、派生、观察与文件资源的全部持久能力。
type Store interface {
	TraceSource
	governanceStore
	derivationStore
	observationStore
	fileResourceStore
	ReadContent(context.Context, *v1.Ref) (*v1.Content, error)
}
type Service struct {
	facts        AssociationFacts
	store        Store
	work         ObservationWork
	ledger       ObservationLedger
	user, domain string
}

func New(s Store, work ObservationWork, user, domain string) (*Service, error) {
	if err := durable.RequireDependencies("content", durable.Dependency{Name: "store", Value: s}, durable.Dependency{Name: "work", Value: work}); err != nil {
		return nil, err
	}
	return &Service{store: s, work: work, user: user, domain: domain}, nil
}

// ValidateDependencies 检查基础依赖与两阶段连接，宿主必须在恢复前完成。
func (s *Service) ValidateDependencies() error {
	return durable.RequireDependencies("content",
		durable.Dependency{Name: "store", Value: s.store},
		durable.Dependency{Name: "work", Value: s.work},
		durable.Dependency{Name: "associations", Value: s.facts},
		durable.Dependency{Name: "ledger", Value: s.ledger},
	)
}

func (s *Service) Stage(ctx context.Context, caller *v1.Caller, c *v1.SubmitGoalCommand) (*v1.Ref, error) {
	if err := command.ValidateGoal(c); err != nil {
		return nil, err
	}
	if err := command.CheckIdentity(caller, c.Identity, s.user, c.Identity.TargetDomainId); err != nil {
		return nil, err
	}
	h := observationHeader(s.user, caller.IssuerId, s.domain, "stage:"+command.SemanticFingerprint("source", c.Identity))
	r, e := s.work.Execute(ctx, caller, h, command.FingerprintV1(c), "content.stage", func(tx context.Context) (*v1.Ref, error) {
		v := &v1.Content{Ref: command.NewRef(s.user, s.domain, "content", "lerna.v1.Content"), Source: c.Identity, MediaType: "text/plain", ProcessingPurposes: []string{"CURRENT_TASK"}, Kind: "USER_INPUT", ContentVersion: 1, SourceDescriptor: &v1.ContentSourceDescriptor{Kind: "USER_INPUT", Locator: c.Identity.CommandId, AcquisitionMethod: "AUTHENTICATED_COMMAND", ProviderVersion: "local-v1"}}
		v.ContentId = v.Ref.Name.LocalId
		return v.Ref, s.prepareRegistration(tx, v, []byte(c.Goal), c.Identity)
	})
	if e != nil {
		return nil, e
	}
	if r.Decision != v1.Decision_DECISION_ACCEPTED {
		return nil, &command.Failure{Detail: r.Error}
	}
	if e = s.ProcessRegistrations(ctx, caller); e != nil {
		return nil, e
	}
	return r.ResultRef, nil
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
	v, e := s.readRegistered(ctx, ref)
	if e == nil && v == nil {
		v, e = s.store.ReadContent(ctx, ref)
	}
	if v != nil && !proto.Equal(v.Ref, ref) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
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
