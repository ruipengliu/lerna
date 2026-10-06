package content

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type derivationStore interface {
	SaveContentDerivation(context.Context, *v1.ContentDerivation) error
	LoadContentDerivation(context.Context, *v1.Ref) (*v1.ContentDerivation, error)
}

// PrepareDerivation 先固定生产责任和输出位置，实例标识由宿主分配。
func (s *Service) PrepareDerivation(ctx context.Context, caller *v1.Caller, c *v1.PrepareDerivationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "host" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("derive-prepare", c.TaskId, c.OperationId, c.AttemptId, c.GeneratorVersion, c.OutputKind, c.MediaType, c.PreviousVersionRef), "content.derivation", func(tx context.Context) (*v1.Ref, error) {
		if c.TaskId == nil || c.TaskId.UserId != s.user || c.TaskId.ObjectKind != "task" || c.TaskId.LocalId == "" || c.GeneratorVersion == "" || c.MediaType == "" {
			return nil, command.Fail("INVALID_DERIVATION")
		}
		if e := s.checkAssociation(tx, caller, c.TaskId, c.OperationId, c.AttemptId); e != nil {
			return nil, e
		}
		switch c.OutputKind {
		case "CONTEXT", "MODEL_OUTPUT", "SUMMARY", "AUXILIARY", "INTERPRETATION":
		default:
			return nil, command.Fail("UNSUPPORTED")
		}
		if c.PreviousVersionRef != nil {
			if e := s.CheckUsable(tx, caller, c.PreviousVersionRef); e != nil {
				return nil, e
			}
		}
		d := &v1.ContentDerivation{Ref: command.NewRef(s.user, s.domain, "derivation", "lerna.v1.ContentDerivation"), Responsibility: c.Header.Identity, TaskId: c.TaskId, OperationId: c.OperationId, AttemptId: c.AttemptId, GeneratorVersion: c.GeneratorVersion, OutputKind: c.OutputKind, MediaType: c.MediaType, HostInstanceId: command.NewRef(s.user, s.domain, "producer", "producer").Name.LocalId, Generation: 1, OutputRef: command.NewRef(s.user, s.domain, "content", "lerna.v1.Content"), LocationRef: command.NewRef(s.user, s.domain+"/body", "body", "lerna.v1.BodyReceipt"), State: "PREPARED", PreviousVersionRef: c.PreviousVersionRef}
		return d.Ref, s.saveDerivation(tx, d)
	})
}
func (s *Service) QueryDerivation(ctx context.Context, caller *v1.Caller, ref *v1.Ref) (*v1.ContentDerivation, error) {
	if ref == nil || ref.Revision != 1 || ref.SchemaId != "lerna.v1.ContentDerivation" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(caller, ref.Name, s.user, s.domain, "derivation"); e != nil {
		return nil, e
	}
	d, e := s.store.(derivationStore).LoadContentDerivation(ctx, ref)
	if d != nil && !proto.Equal(d.Ref, ref) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return d, e
}
func (s *Service) currentDerivation(ctx context.Context, caller *v1.Caller, ref *v1.Ref, instance string, generation uint64) (*v1.ContentDerivation, error) {
	d, e := s.QueryDerivation(ctx, caller, ref)
	if e != nil {
		return nil, e
	}
	if d == nil {
		return nil, command.Fail("NOT_FOUND")
	}
	if d.HostInstanceId != instance || d.Generation != generation {
		return nil, command.Fail("STALE_PRODUCER")
	}
	return d, nil
}
func includes(refs []*v1.Ref, ref *v1.Ref) bool {
	for _, r := range refs {
		if proto.Equal(r, ref) {
			return true
		}
	}
	return false
}

// ReadDerivationInput 由受信宿主调用，先登记实际输入再把正文交给计算。
func (s *Service) ReadDerivationInput(ctx context.Context, caller *v1.Caller, c *v1.ReadDerivationInputCommand) (*v1.Content, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "host" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	r, e := s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("derive-read", c.DerivationRef, c.HostInstanceId, c.Generation, c.InputRef), "content.derivation_input", func(tx context.Context) (*v1.Ref, error) {
		d, e := s.currentDerivation(tx, caller, c.DerivationRef, c.HostInstanceId, c.Generation)
		if e != nil {
			return nil, e
		}
		if d.State != "PREPARED" && d.State != "COMPUTING" {
			return nil, command.Fail("INPUT_SET_SEALED")
		}
		if e = s.CheckUsable(tx, caller, c.InputRef); e != nil {
			return nil, e
		}
		if !includes(d.ActualInputRefs, c.InputRef) {
			d.ActualInputRefs = append(d.ActualInputRefs, c.InputRef)
		}
		d.State = "COMPUTING"
		return c.InputRef, s.saveDerivation(tx, d)
	})
	if e != nil {
		return nil, e
	}
	if r.Decision != v1.Decision_DECISION_ACCEPTED {
		return nil, &command.Failure{Detail: r.Error}
	}
	d, e := s.currentDerivation(ctx, caller, c.DerivationRef, c.HostInstanceId, c.Generation)
	if e != nil {
		return nil, e
	}
	if d.State != "PREPARED" && d.State != "COMPUTING" {
		return nil, command.Fail("INPUT_SET_SEALED")
	}
	return s.Read(ctx, caller, r.ResultRef)
}
func (s *Service) SealDerivation(ctx context.Context, caller *v1.Caller, c *v1.SealDerivationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "host" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("derive-seal", c.DerivationRef, c.HostInstanceId, c.Generation, c.InputRefs), "content.derivation_seal", func(tx context.Context) (*v1.Ref, error) {
		d, e := s.currentDerivation(tx, caller, c.DerivationRef, c.HostInstanceId, c.Generation)
		if e != nil {
			return nil, e
		}
		if d.State != "COMPUTING" && d.State != "PREPARED" {
			return nil, command.Fail("INPUT_SET_SEALED")
		}
		if len(d.ActualInputRefs) == 0 {
			return nil, command.Fail("MISSING_ACTUAL_INPUT")
		}
		for _, r := range d.ActualInputRefs {
			if !includes(c.InputRefs, r) {
				return nil, command.Fail("INCOMPLETE_INPUT_SET")
			}
		}
		for _, r := range c.InputRefs {
			if e = s.CheckUsable(tx, caller, r); e != nil {
				return nil, e
			}
			if !includes(d.SealedInputRefs, r) {
				d.SealedInputRefs = append(d.SealedInputRefs, r)
			}
		}
		d.State = "SEALED"
		return d.Ref, s.saveDerivation(tx, d)
	})
}
func (s *Service) CommitDerivation(ctx context.Context, caller *v1.Caller, c *v1.CommitDerivationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "host" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("derive-commit", c.DerivationRef, c.HostInstanceId, c.Generation, append([]byte{}, c.Body...)), "content.derivation_commit", func(tx context.Context) (*v1.Ref, error) {
		d, e := s.currentDerivation(tx, caller, c.DerivationRef, c.HostInstanceId, c.Generation)
		if e != nil {
			return nil, e
		}
		if d.State != "SEALED" {
			return nil, command.Fail("INPUT_SET_NOT_SEALED")
		}
		for _, r := range d.SealedInputRefs {
			if e = s.CheckUsable(tx, caller, r); e != nil {
				return nil, e
			}
		}
		v := &v1.Content{Ref: d.OutputRef, ContentId: d.OutputRef.Name.LocalId, ContentVersion: 1, Source: c.Header.Identity, SourceDescriptor: &v1.ContentSourceDescriptor{Kind: "DERIVED", Locator: d.Ref.Name.LocalId, AcquisitionMethod: "HOST_DERIVATION", ProviderVersion: d.GeneratorVersion}, TaskId: d.TaskId, OperationId: d.OperationId, AttemptId: d.AttemptId, ProducerRef: d.Ref, MediaType: d.MediaType, Kind: d.OutputKind, DerivedFrom: d.SealedInputRefs, ProcessingPurposes: []string{"CURRENT_TASK"}, LocationRef: d.LocationRef}
		if d.PreviousVersionRef != nil {
			old, e := s.Read(tx, caller, d.PreviousVersionRef)
			if e != nil {
				return nil, e
			}
			if old == nil {
				return nil, command.Fail("INVALID_REFERENCE")
			}
			v.ContentId = old.ContentId
			v.ContentVersion, e = s.store.(governanceStore).NextContentVersion(tx, old.ContentId)
			if e != nil {
				return nil, e
			}
			v.PreviousVersionRefs = []*v1.Ref{old.Ref}
		}
		if e = s.prepareRegistration(tx, v, c.Body, c.Header.Identity); e != nil {
			return nil, e
		}
		d.State = "STAGED"
		return v.Ref, s.saveDerivation(tx, d)
	})
}

// TakeoverDerivation 保留原责任与已捕获输入，只替换计算实例并递增围栏。
func (s *Service) TakeoverDerivation(ctx context.Context, caller *v1.Caller, c *v1.TakeoverDerivationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "host" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("derive-takeover", c.DerivationRef, c.ExpectedGeneration), "content.derivation_takeover", func(tx context.Context) (*v1.Ref, error) {
		d, e := s.QueryDerivation(tx, caller, c.DerivationRef)
		if e != nil {
			return nil, e
		}
		if d == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		if d.Generation != c.ExpectedGeneration {
			return nil, command.Fail("STALE_PRODUCER")
		}
		if d.State == "STAGED" || d.State == "COMMITTED" {
			return nil, command.Fail("PRODUCTION_CLOSED")
		}
		d.Generation++
		d.HostInstanceId = command.NewRef(s.user, s.domain, "producer", "producer").Name.LocalId
		return d.Ref, s.saveDerivation(tx, d)
	})
}
