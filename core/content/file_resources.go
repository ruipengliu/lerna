package content

import (
	"context"
	"net/url"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type fileResourceStore interface {
	SaveManagedFileRoot(context.Context, *v1.ManagedFileRoot) error
	LoadManagedFileRoot(context.Context, string, string) (*v1.ManagedFileRoot, error)
	SaveFileResources(context.Context, *v1.FileResources) error
	LoadFileResources(context.Context, *v1.Ref) (*v1.FileResources, error)
	FileResourcesForSend(context.Context, *v1.Ref) (*v1.FileResources, error)
}

// RegisterFileResources 仅为原发送登记精确的副本名称；登记不证明文件已写或已发布。
func (s *Service) RegisterFileResources(ctx context.Context, caller *v1.Caller, c *v1.RegisterFileResourcesCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "egress-io" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("file-resources", c.DerivationRef, c.SendRef), "content.file_resources", func(tx context.Context) (*v1.Ref, error) {
		d, e := s.QueryDerivation(tx, caller, c.DerivationRef)
		if e != nil {
			return nil, e
		}
		if d == nil || d.State != "COMMITTED" || d.GeneratorVersion != "managed-file-v1" || d.OperationId == nil || d.AttemptId == nil {
			return nil, command.Fail("INVALID_FILE_RESOURCES")
		}
		x, e := s.ledger.QuerySendExecution(tx, caller, d.OperationId, c.SendRef)
		if e != nil {
			return nil, e
		}
		if x == nil || x.CallDescriptor.Protocol != "FILE" || (x.CallDescriptor.Method != "CREATE" && x.CallDescriptor.Method != "REPLACE") || !proto.Equal(x.Send.Ref, c.SendRef) || !proto.Equal(x.Attempt.Ref.Name, d.AttemptId) || x.Send.Phase != "DISPATCH_POSSIBLE" || len(d.ActualInputRefs) != 1 || !proto.Equal(d.ActualInputRefs[0], x.CallDescriptor.ParametersRef) {
			return nil, command.Fail("INVALID_FILE_RESOURCES")
		}
		body, e := s.Read(tx, caller, d.OutputRef)
		if e != nil {
			return nil, e
		}
		if body == nil || body.Status != "AVAILABLE" {
			return nil, command.Fail("CONTENT_UNUSABLE")
		}
		existing, e := s.store.FileResourcesForSend(tx, c.SendRef)
		if e != nil {
			return nil, e
		}
		if existing != nil {
			if !proto.Equal(existing.DerivationRef, c.DerivationRef) {
				return nil, command.Fail("IDEMPOTENCY_CONFLICT")
			}
			return existing.Ref, nil
		}
		name := command.SemanticFingerprint("managed-file-resource-v1", c.SendRef.Name)
		r := &v1.FileResources{Ref: command.NewRef(s.user, s.domain, "file-resources", "lerna.v1.FileResources"), DerivationRef: d.Ref, ContentRef: d.OutputRef, OperationId: d.OperationId, AttemptId: d.AttemptId, SendRef: c.SendRef, Target: x.CallDescriptor.Target, ObjectName: name + ".data", PointerName: "." + name + ".pending", Digest: body.Digest, ByteSize: body.ByteSize, CleanupStatus: "RETAINED"}
		return r.Ref, s.saveFileResources(tx, caller, r, "FILE_RESOURCES_REGISTERED", c.Header.Identity, nil)
	})
}
func (s *Service) QueryFileResources(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.FileResources, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.FileResources" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(caller, r.Name, s.user, s.domain, "file-resources"); e != nil {
		return nil, e
	}
	v, e := s.store.LoadFileResources(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
func (s *Service) QueryFileResourcesForSend(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.FileResources, error) {
	if r == nil || r.SchemaId != "lerna.v1.PhysicalSend" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckCaller(caller, s.user); e != nil {
		return nil, e
	}
	if r.GetName().GetUserId() != s.user || r.GetName().GetObjectKind() != "send" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	v, e := s.store.FileResourcesForSend(ctx, r)
	if v != nil && !proto.Equal(v.SendRef.Name, r.Name) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}

func (s *Service) recordFileResourceObservation(ctx context.Context, caller *v1.Caller, o *v1.RawObservation, origin *v1.CommandIdentity) error {
	ev := o.FileEvidence
	if ev == nil || ev.ResourcesRef == nil {
		return nil
	}
	r, e := s.QueryFileResources(ctx, caller, ev.ResourcesRef)
	if e != nil {
		return e
	}
	if r == nil || r.Target != o.Target {
		return command.Fail("INVALID_FILE_RESOURCES")
	}
	if !proto.Equal(r.OperationId, o.OperationId) {
		return nil
	}
	if !proto.Equal(r.AttemptId, o.AttemptId) || !proto.Equal(r.SendRef, o.SendRef) {
		return command.Fail("INVALID_FILE_RESOURCES")
	}
	if r.ObjectIdentity != "" && r.ObjectIdentity != ev.ObjectIdentity || r.PointerIdentity != "" && r.PointerIdentity != ev.PointerIdentity {
		return command.Fail("FILE_RESOURCE_IDENTITY_MISMATCH")
	}
	r.ObjectIdentity = ev.ObjectIdentity
	r.PointerIdentity = ev.PointerIdentity
	return s.saveFileResources(ctx, caller, r, "FILE_RESOURCES_OBSERVED", origin, o.Ref)
}
func (s *Service) acceptFileResourceCleanup(ctx context.Context, caller *v1.Caller, o *v1.RawObservation, origin *v1.CommandIdentity) error {
	ev := o.FileEvidence
	if ev == nil || ev.ResourcesRef == nil || ev.Stage != "CLEANED" || !ev.DurabilityConfirmed || !ev.ReadbackVerified || ev.ErrorCode != "" {
		return nil
	}
	op, e := s.ledger.QueryOperation(ctx, caller, o.OperationId)
	if e != nil {
		return e
	}
	if op == nil || op.Execution == nil || op.Execution.CallDescriptor.Protocol != "FILE" || op.Execution.CallDescriptor.Method != "CLEANUP" {
		return command.Fail("INVALID_FILE_CLEANUP")
	}
	r, e := s.QueryFileResources(ctx, caller, ev.ResourcesRef)
	if e != nil {
		return e
	}
	if r == nil || r.Target != o.Target {
		return command.Fail("INVALID_FILE_CLEANUP")
	}
	r.CleanupStatus = "CLEANED"
	r.CleanupObservationRef = o.Ref
	return s.saveFileResources(ctx, caller, r, "FILE_RESOURCES_CLEANED", origin, o.Ref)
}

// BindFileRoot 的绑定跨进程保留；同名目录被替换后不能继承原资源权限。
func (s *Service) BindFileRoot(ctx context.Context, caller *v1.Caller, c *v1.BindFileRootCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "egress-io" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("file-root", c.OperationId, c.SendRef, c.RootId, c.NativeIdentity, c.Platform), "content.file_root", func(tx context.Context) (*v1.Ref, error) {
		if c.RootId == "" || c.NativeIdentity == "" || c.Platform == "" {
			return nil, command.Fail("INVALID_FILE_ROOT")
		}
		x, e := s.ledger.QuerySendExecution(tx, caller, c.OperationId, c.SendRef)
		if e != nil {
			return nil, e
		}
		if x == nil || x.Send.Phase != "DISPATCH_POSSIBLE" || !proto.Equal(x.Send.Ref, c.SendRef) || x.CallDescriptor.Protocol != "FILE" {
			return nil, command.Fail("INVALID_FILE_ROOT")
		}
		target, e := url.Parse(x.CallDescriptor.Target)
		if e != nil || target.Scheme != "managed" || target.Host != c.RootId {
			return nil, command.Fail("INVALID_FILE_ROOT")
		}
		store := s.store
		old, e := store.LoadManagedFileRoot(tx, s.user, c.RootId)
		if e != nil {
			return nil, e
		}
		if old != nil {
			if old.NativeIdentity != c.NativeIdentity || old.Platform != c.Platform || old.ExecutorEndpointId != "local-file" {
				return nil, command.Fail("FILE_ROOT_CHANGED")
			}
			return old.Ref, nil
		}
		root := &v1.ManagedFileRoot{Ref: command.NewRef(s.user, s.domain, "file-root", "lerna.v1.ManagedFileRoot"), RootId: c.RootId, NativeIdentity: c.NativeIdentity, Platform: c.Platform, ExecutorEndpointId: "local-file"}
		if e = store.SaveManagedFileRoot(tx, root); e != nil {
			return nil, e
		}
		op, e := s.ledger.QueryOperation(tx, caller, c.OperationId)
		if e != nil {
			return nil, e
		}
		if op == nil {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		admission, e := s.facts.QueryAdmission(tx, caller, op.AdmissionRef)
		if e != nil {
			return nil, e
		}
		if admission == nil {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		return root.Ref, s.store.SaveTraceSource(tx, "content", &v1.TraceEvent{EventType: "FILE_ROOT_BOUND", SourceRecordRef: root.Ref, TaskId: admission.TaskId, OperationId: c.OperationId, AttemptId: x.Attempt.Ref.Name, SendRef: c.SendRef, OriginCommand: c.Header.Identity, RelatedRefs: []*v1.Ref{x.Attempt.Ref}})
	})
}

// QueryManagedFileRoot 只读取受治理的资源身份；存在绑定不证明任意文件已发布。
func (s *Service) QueryManagedFileRoot(ctx context.Context, caller *v1.Caller, rootID string) (*v1.ManagedFileRoot, error) {
	if e := command.CheckCaller(caller, s.user); e != nil {
		return nil, e
	}
	if rootID == "" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return s.store.LoadManagedFileRoot(ctx, s.user, rootID)
}

// saveFileResources 与资源责任同事务保存引用，不将原生名字或字节复制给追踪。
func (s *Service) saveFileResources(ctx context.Context, caller *v1.Caller, r *v1.FileResources, kind string, origin *v1.CommandIdentity, observation *v1.Ref) error {
	op, e := s.ledger.QueryOperation(ctx, caller, r.OperationId)
	if e != nil {
		return e
	}
	if op == nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	admission, e := s.facts.QueryAdmission(ctx, caller, op.AdmissionRef)
	if e != nil {
		return e
	}
	if admission == nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	if e = s.store.SaveFileResources(ctx, r); e != nil {
		return e
	}
	return s.store.SaveTraceSource(ctx, "content", &v1.TraceEvent{EventType: kind, SourceRecordRef: r.Ref, TaskId: admission.TaskId, OperationId: r.OperationId, AttemptId: r.AttemptId, SendRef: r.SendRef, BodyRef: r.ContentRef, OriginCommand: origin, RelatedRefs: []*v1.Ref{r.DerivationRef, r.CleanupObservationRef, observation}})
}
