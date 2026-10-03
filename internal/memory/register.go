package memory

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func command[I, O any](s *Service, registry *runtime.Registry, name, owner string, cas bool, apply func(context.Context, runtime.Tx, runtime.Auth, api.Command, I) (O, error)) {
	registry.MustRegister(runtime.Method{Contract: api.Contract[I, O](name, owner, "command", cas, false), Participants: s.participants(), Apply: func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command) (runtime.Outcome, error) {
		if _, err := loadHead(ctx, tx); err != nil {
			return runtime.Outcome{}, err
		}
		var in I
		if err := api.Decode(c.Payload, &in); err != nil {
			return runtime.Outcome{}, err
		}
		out, err := apply(ctx, tx, auth, c, in)
		if err != nil {
			return runtime.Outcome{}, err
		}
		return runtime.Applied(out), nil
	}})
}
func query[I, O any](s *Service, registry *runtime.Registry, name, owner string, apply func(context.Context, runtime.Scope, runtime.Auth, api.Query, I) (O, error)) {
	registry.MustRegister(runtime.Method{Contract: api.Contract[I, O](name, owner, "query", false, false), Participants: s.participants(), Query: func(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query) (any, error) {
		var in I
		if err := api.Decode(q.Payload, &in); err != nil {
			return nil, err
		}
		return apply(ctx, scope, auth, q, in)
	}})
}

type PolicyOutput struct {
	PolicyRef api.ComponentRef `json:"policy_ref"`
}
type TransferStatusInput struct {
	TransferID string `json:"transfer_id"`
}
type TransferStatus struct {
	TransferID string         `json:"transfer_id"`
	ContentRef api.ContentRef `json:"content_ref"`
	Phase      string         `json:"phase"`
	ExpiresAt  string         `json:"expires_at"`
	Durability string         `json:"durability"`
}

// Register 只登记实际实现的方法；未登记的方法由 Runtime 返回 unsupported。
func (s *Service) Register(registry *runtime.Registry) {
	query(s, registry, "memory.index.inspect", "memory", func(ctx context.Context, scope runtime.Scope, auth runtime.Auth, q api.Query, in struct{}) (IndexStatus, error) {
		if q.TargetID != scope.OwnerID {
			return IndexStatus{}, api.E("invalid_request", "target_mismatch")
		}
		return s.IndexStatus(ctx, scope, auth)
	})
	s.registerMemory(registry)
	s.registerQueries(registry)
	s.registerExtraction(registry)
	s.registerJobs(registry)
	s.registerViews(registry)
	command(s, registry, "content.policy.install", "content", false, func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in Policy) (PolicyOutput, error) {
		if c.TargetID != in.PolicyRef.ComponentID {
			return PolicyOutput{}, api.E("invalid_request", "target_mismatch")
		}
		err := s.InstallPolicyTx(ctx, tx, auth, in)
		return PolicyOutput{in.PolicyRef}, err
	})
	command(s, registry, "content.upload_reserve", "content", false, s.reserve)
	command(s, registry, "content.put", "content", false, func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in PutInput) (PutOutput, error) {
		if c.TargetID != in.ContentRef.ContentID {
			return PutOutput{}, api.E("invalid_request", "target_mismatch")
		}
		ref, err := s.PublishInTx(ctx, tx, auth, in)
		return PutOutput{ref}, err
	})
	command(s, registry, "content.register_copy", "content", false, func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in RegisterCopyInput) (CopyOutput, error) {
		if c.TargetID != in.ContentRef.ContentID || in.HolderRef.ObjectID != auth.SubjectID || in.HolderRef.Revision != auth.CredentialGeneration {
			return CopyOutput{}, api.E("forbidden", "copy_holder_mismatch")
		}
		return s.RegisterCopyTx(ctx, tx, auth, in)
	})
	command(s, registry, "content.close", "content", true, func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in CloseInput) (CloseOutput, error) {
		if c.TargetID != in.ContentRef.ContentID {
			return CloseOutput{}, api.E("invalid_request", "target_mismatch")
		}
		return s.close(ctx, tx, auth, c.ExpectedRevision, in)
	})
	command(s, registry, "content.release_copy", "content", false, func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ReleaseCopyInput) (CopyOutput, error) {
		if c.TargetID != in.ContentRef.ContentID {
			return CopyOutput{}, api.E("invalid_request", "target_mismatch")
		}
		return s.releaseCopy(ctx, tx, auth, in)
	})
	command(s, registry, "content.mirror_reserve", "content", false, s.mirror)
	command(s, registry, "content.mirror_complete", "content", false, s.completeMirror)
	query(s, registry, "content.get", "content", func(ctx context.Context, scope runtime.Scope, auth runtime.Auth, q api.Query, in GetContentInput) (GetContentOutput, error) {
		if q.TargetID != in.ContentRef.ContentID {
			return GetContentOutput{}, api.E("invalid_request", "target_mismatch")
		}
		return s.getContent(ctx, scope, auth, in)
	})
	query(s, registry, "content.transfer.read", "content", func(ctx context.Context, scope runtime.Scope, auth runtime.Auth, q api.Query, in TransferStatusInput) (TransferStatus, error) {
		var out TransferStatus
		err := s.within(ctx, scope, func(tx runtime.Tx) error {
			if err := checkAuth(scope, auth); err != nil {
				return err
			}
			var t Transfer
			_, err := tx.Get(ctx, "content.transfers", in.TransferID, &t)
			if err != nil {
				return err
			}
			if q.TargetID != in.TransferID || t.PublisherID != auth.SubjectID {
				return api.E("forbidden", "transfer_principal_mismatch")
			}
			out = TransferStatus{t.TransferID, t.ContentRef, t.Phase, t.ExpiresAt, t.ObjectLocation.Durability}
			return nil
		})
		return out, err
	})
}
