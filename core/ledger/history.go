package ledger

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type historyStore interface {
	LoadAttemptVersion(context.Context, *v1.Ref) (*v1.ExecutionAttempt, error)
	LoadSendVersion(context.Context, *v1.Ref) (*v1.PhysicalSend, error)
	LoadEffectVersion(context.Context, *v1.Ref) (*v1.Effect, error)
	LoadOperationVersion(context.Context, *v1.Ref) (*v1.Operation, error)
}

func (s *Service) checkHistory(c *v1.Caller, r *v1.Ref, kind, schema string) error {
	if r == nil || r.Revision == 0 || r.SchemaId != schema {
		return command.Fail("INVALID_REFERENCE")
	}
	return command.CheckName(c, r.Name, s.user, s.domain, kind)
}
func (s *Service) QueryAttempt(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.ExecutionAttempt, error) {
	if e := s.checkHistory(c, r, "attempt", "lerna.v1.ExecutionAttempt"); e != nil {
		return nil, e
	}
	v, e := s.store.(historyStore).LoadAttemptVersion(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
func (s *Service) QuerySend(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.PhysicalSend, error) {
	if e := s.checkHistory(c, r, "send", "lerna.v1.PhysicalSend"); e != nil {
		return nil, e
	}
	v, e := s.store.(historyStore).LoadSendVersion(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
func (s *Service) QueryEffect(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Effect, error) {
	if e := s.checkHistory(c, r, "effect", "lerna.v1.Effect"); e != nil {
		return nil, e
	}
	v, e := s.store.(historyStore).LoadEffectVersion(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
func (s *Service) QueryOperationVersion(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Operation, error) {
	if e := s.checkHistory(c, r, "operation", "lerna.v1.Operation"); e != nil {
		return nil, e
	}
	v, e := s.store.(historyStore).LoadOperationVersion(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
