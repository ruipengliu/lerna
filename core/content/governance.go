package content

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type governanceStore interface {
	SaveContentRegistration(context.Context, *v1.ContentRegistration) error
	LoadContentRegistration(context.Context, *v1.Ref) (*v1.ContentRegistration, error)
	PendingContentRegistrations(context.Context) ([]*v1.ContentRegistration, error)
	NextContentVersion(context.Context, string) (uint64, error)
	ContentTime(context.Context) (int64, error)
	AcceptContentBody(context.Context, *v1.BodyDeposit, []byte) (*v1.BodyReceipt, error)
	QueryContentBodyReceipt(context.Context, *v1.BodyDeposit) (*v1.BodyReceipt, error)
	ReadContentBody(context.Context, *v1.BodyDeposit) ([]byte, error)
}

// Register 固定来源与暂存责任；接纳回执不代表版本可用。
func (s *Service) Register(ctx context.Context, caller *v1.Caller, c *v1.RegisterContentCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "host" && caller.GetIssuerId() != "local-cli" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("content-register", append([]byte{}, c.Body...), c.MediaType, c.SourceDescriptor, c.PreviousVersionRef, c.TaskId, c.OperationId, c.AttemptId), "content.register", func(tx context.Context) (*v1.Ref, error) {
		if c.SourceDescriptor == nil || c.SourceDescriptor.Kind != "HOST_IMPORT" || c.SourceDescriptor.Locator == "" || c.SourceDescriptor.AcquisitionMethod == "" || c.SourceDescriptor.ProviderVersion == "" || c.SourceDescriptor.ObservationRef != nil || c.MediaType == "" {
			return nil, command.Fail("INVALID_CONTENT_SOURCE")
		}
		if e := s.checkAssociation(tx, caller, c.TaskId, c.OperationId, c.AttemptId); e != nil {
			return nil, e
		}
		v := &v1.Content{Ref: command.NewRef(s.user, s.domain, "content", "lerna.v1.Content"), Source: c.Header.Identity, SourceDescriptor: c.SourceDescriptor, MediaType: c.MediaType, TaskId: c.TaskId, OperationId: c.OperationId, AttemptId: c.AttemptId, Kind: "ORIGINAL", ProcessingPurposes: []string{"CURRENT_TASK"}}
		v.ContentId = v.Ref.Name.LocalId
		v.ContentVersion = 1
		if c.PreviousVersionRef != nil {
			old, e := s.Read(tx, caller, c.PreviousVersionRef)
			if e != nil {
				return nil, e
			}
			if old == nil {
				return nil, command.Fail("INVALID_REFERENCE")
			}
			v.ContentId = old.ContentId
			v.ContentVersion, e = s.store.NextContentVersion(tx, old.ContentId)
			if e != nil {
				return nil, e
			}
			v.PreviousVersionRefs = []*v1.Ref{old.Ref}
		}
		return v.Ref, s.prepareRegistration(tx, v, c.Body, c.Header.Identity)
	})
}
func (s *Service) prepareRegistration(ctx context.Context, v *v1.Content, body []byte, id *v1.CommandIdentity) error {
	if e := checkMedia(v.MediaType, body); e != nil {
		return e
	}
	store := s.store
	now, e := store.ContentTime(ctx)
	if e != nil {
		return e
	}
	v.AcquiredAtUnixMs = now
	v.Status = "STAGED"
	v.PublishStatus = "STAGED"
	v.UseStatus = "UNUSABLE"
	v.CleanupStatus = "NONE"
	v.ByteSize = uint64(len(body))
	v.DigestAlgorithm = "SHA256"
	v.Digest = fmt.Sprintf("%x", sha256.Sum256(body))
	if v.LocationRef == nil {
		v.LocationRef = command.NewRef(s.user, s.domain+"/body", "body", "lerna.v1.BodyReceipt")
	}
	d := &v1.BodyDeposit{Identity: &v1.CommandIdentity{UserId: s.user, IssuerId: "content-holder", TargetDomainId: s.domain + "/body", CommandId: v.Ref.Name.LocalId}, ContentRef: v.Ref, LocationRef: v.LocationRef, Digest: v.Digest, ByteSize: v.ByteSize}
	return s.saveRegistration(ctx, &v1.ContentRegistration{Content: v, Identity: id, StagedBody: body, Deposit: d, State: "STAGED", StagingLocationRef: &v1.Ref{Name: &v1.GlobalName{UserId: s.user, AuthorityDomainId: s.domain, ObjectKind: "content-staging", LocalId: v.Ref.Name.LocalId}, Revision: 1, SchemaId: "lerna.v1.ContentRegistration"}})
}
func (s *Service) QueryRegistration(ctx context.Context, caller *v1.Caller, ref *v1.Ref) (*v1.ContentRegistration, error) {
	if ref == nil {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(caller, ref.Name, s.user, s.domain, "content"); e != nil {
		return nil, e
	}
	r, e := s.store.LoadContentRegistration(ctx, ref)
	if r != nil {
		if !proto.Equal(r.Content.Ref, ref) {
			return nil, command.Fail("INVALID_REFERENCE")
		}
		r.StagedBody = nil
	}
	return r, e
}

// ProcessRegistrations 在重发前查询原持有方回执，然后单独提交发布。
func (s *Service) ProcessRegistrations(ctx context.Context, caller *v1.Caller) error {
	if e := command.CheckCaller(caller, s.user); e != nil {
		return e
	}
	store := s.store
	all, e := store.PendingContentRegistrations(ctx)
	if e != nil {
		return e
	}
	for _, r := range all {
		receipt, e := store.QueryContentBodyReceipt(ctx, r.Deposit)
		if e != nil {
			return e
		}
		if receipt == nil {
			receipt, e = store.AcceptContentBody(ctx, r.Deposit, r.StagedBody)
			if e != nil {
				return e
			}
		}
		body, e := store.ReadContentBody(ctx, r.Deposit)
		if e != nil {
			return e
		}
		if r.Content.Digest != fmt.Sprintf("%x", sha256.Sum256(body)) || uint64(len(body)) != r.Content.ByteSize {
			return command.Fail("CONTENT_INTEGRITY")
		}
		actor := &v1.Caller{UserId: s.user, IssuerId: "content-publisher"}
		h := observationHeader(s.user, actor.IssuerId, s.domain, "publish:"+r.Content.Ref.Name.LocalId)
		result, e := s.work.Execute(ctx, actor, h, command.SemanticFingerprint("content-publish", r.Content.Ref, receipt), "content.publish", func(tx context.Context) (*v1.Ref, error) {
			current, e := store.LoadContentRegistration(tx, r.Content.Ref)
			if e != nil {
				return nil, e
			}
			if current.State == "PUBLISHED" {
				return current.Content.Ref, nil
			}
			if current.Content.ProducerRef != nil {
				d, e := s.QueryDerivation(tx, actor, current.Content.ProducerRef)
				if e != nil {
					return nil, e
				}
				if d == nil || d.State != "STAGED" || !proto.Equal(d.OutputRef, current.Content.Ref) {
					return nil, command.Fail("INVALID_DERIVATION")
				}
				for _, ref := range d.SealedInputRefs {
					if e = s.CheckUsable(tx, actor, ref); e != nil {
						return nil, e
					}
				}
				d.State = "COMMITTED"
				d.BodyReceipt = receipt
				if e = s.saveDerivation(tx, d); e != nil {
					return nil, e
				}
			}
			current.Content.Status = "AVAILABLE"
			current.Content.PublishStatus = "PUBLISHED"
			current.Content.UseStatus = "USABLE"
			current.BodyReceipt = receipt
			current.StagedBody = nil
			current.State = "PUBLISHED"
			return current.Content.Ref, s.saveRegistration(tx, current)
		})
		if e != nil {
			return e
		}
		if result.Decision != v1.Decision_DECISION_ACCEPTED {
			return command.Fail(result.Error.Code)
		}
	}
	return nil
}
func (s *Service) readRegistered(ctx context.Context, r *v1.Ref) (*v1.Content, error) {
	store := s.store
	v, e := store.LoadContentRegistration(ctx, r)
	if e != nil || v == nil {
		return nil, e
	}
	if !proto.Equal(v.Content.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if v.State != "PUBLISHED" {
		return nil, command.Fail("CONTENT_UNUSABLE")
	}
	body, e := store.ReadContentBody(ctx, v.Deposit)
	if e != nil {
		return nil, e
	}
	if v.Content.Digest != fmt.Sprintf("%x", sha256.Sum256(body)) || v.Content.ByteSize != uint64(len(body)) {
		return nil, command.Fail("CONTENT_INTEGRITY")
	}
	v.Content.RawBody = append([]byte{}, body...)
	if v.Content.Kind == "USER_INPUT" {
		v.Content.Text = string(body)
	}
	return v.Content, nil
}

// Delete 明确拒绝 M1 尚未实现的清理，不改变正文与使用状态。
func (s *Service) Delete(ctx context.Context, caller *v1.Caller, c *v1.DeleteContentCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if e := command.CheckIdentity(caller, c.Header.Identity, s.user, s.domain); e != nil {
		return nil, e
	}
	if c.ContentRef == nil {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(caller, c.ContentRef.Name, s.user, s.domain, "content"); e != nil {
		return nil, e
	}
	return nil, command.Fail("UNSUPPORTED")
}

func (s *Service) QueryReceipt(ctx context.Context, caller *v1.Caller, id *v1.CommandIdentity) (*v1.ReceiptQuery, error) {
	return s.work.QueryReceipt(ctx, caller, id)
}

func checkMedia(media string, body []byte) error {
	switch media {
	case "application/octet-stream":
		return nil
	case "text/plain":
		if utf8.Valid(body) {
			return nil
		}
	case "application/json":
		if utf8.Valid(body) && json.Valid(body) {
			return nil
		}
	default:
		return command.Fail("UNSUPPORTED_MEDIA_TYPE")
	}
	return command.Fail("CONTENT_TYPE_MISMATCH")
}
