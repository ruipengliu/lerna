package sqlite

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) saveExecutionVersion(ctx context.Context, ref *v1.Ref, message proto.Message) error {
	old := message.ProtoReflect().New().Interface()
	ok, e := s.loadExecutionVersion(ctx, ref, old)
	if e != nil {
		return e
	}
	if ok {
		if !proto.Equal(old, message) {
			return command.Fail("IMMUTABLE_REFERENCE_CONFLICT")
		}
		return nil
	}
	return s.saveRecord(ctx, "ledger", "INSERT INTO ledger_versions VALUES(?,?,?,?,?,?)", message, ref.Name.UserId, ref.Name.AuthorityDomainId, ref.Name.ObjectKind, ref.Name.LocalId, ref.Revision)
}
func (s *Store) saveExecutionVersions(ctx context.Context, op *v1.Operation) error {
	if e := s.saveExecutionVersion(ctx, op.Ref, op); e != nil {
		return e
	}
	if e := s.saveExecutionVersion(ctx, op.Effect.Ref, op.Effect); e != nil {
		return e
	}
	if op.Execution != nil {
		for _, send := range op.Execution.PreviousSends {
			if e := s.saveExecutionVersion(ctx, send.Ref, send); e != nil {
				return e
			}
		}
		if e := s.saveExecutionVersion(ctx, op.Execution.Attempt.Ref, op.Execution.Attempt); e != nil {
			return e
		}
		if e := s.saveExecutionVersion(ctx, op.Execution.Send.Ref, op.Execution.Send); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) loadExecutionVersion(ctx context.Context, r *v1.Ref, message proto.Message) (bool, error) {
	return s.load(ctx, message, "SELECT record FROM ledger_versions WHERE user_id=? AND domain_id=? AND object_kind=? AND id=? AND revision=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.ObjectKind, r.Name.LocalId, r.Revision)
}
func (s *Store) LoadAttemptVersion(ctx context.Context, r *v1.Ref) (*v1.ExecutionAttempt, error) {
	v := new(v1.ExecutionAttempt)
	ok, e := s.loadExecutionVersion(ctx, r, v)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) LoadSendVersion(ctx context.Context, r *v1.Ref) (*v1.PhysicalSend, error) {
	v := new(v1.PhysicalSend)
	ok, e := s.loadExecutionVersion(ctx, r, v)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) LoadEffectVersion(ctx context.Context, r *v1.Ref) (*v1.Effect, error) {
	v := new(v1.Effect)
	ok, e := s.loadExecutionVersion(ctx, r, v)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) LoadOperationVersion(ctx context.Context, r *v1.Ref) (*v1.Operation, error) {
	v := new(v1.Operation)
	ok, e := s.loadExecutionVersion(ctx, r, v)
	if !ok {
		return nil, e
	}
	return v, e
}
