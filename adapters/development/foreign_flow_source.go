package development

import (
	"context"

	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 各个明确配对来源共用本次载体；只有实际远端Current返回的整个证明
// 被保存。载体不授权限，Memory继续核签名、期限、holder及当前known-deny。
type flowSources struct{ source memory.ForeignContentPort }

func (s flowSources) stage(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref memory.ForeignReference, proof memory.ForeignProof, err error) (memory.ForeignProof, error) {
	if err == nil {
		err = stageForeignUse(ctx, scope, auth, ref, proof)
	}
	if err != nil {
		if c := currentForeignCarrier(ctx); c != nil {
			c.discard(ref)
		}
	}
	return proof, err
}

func (s flowSources) RegisterCopy(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref memory.ForeignReference) (memory.ForeignProof, error) {
	proof, err := s.source.RegisterCopy(ctx, scope, auth, ref)
	return s.stage(ctx, scope, auth, ref, proof, err)
}

func (s flowSources) Current(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref memory.ForeignReference) (memory.ForeignProof, error) {
	proof, err := s.source.Current(ctx, scope, auth, ref)
	return s.stage(ctx, scope, auth, ref, proof, err)
}

func (s flowSources) Read(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref memory.ForeignReference, proof memory.ForeignProof) ([]byte, error) {
	return s.source.Read(ctx, scope, auth, ref, proof)
}

func (s flowSources) Control(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref memory.ForeignReference) (memory.ForeignProof, error) {
	if c := currentForeignCarrier(ctx); c != nil {
		c.discard(ref)
	}
	return s.source.Control(ctx, scope, auth, ref)
}

func (s flowSources) Release(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref memory.ForeignReference, report memory.ReleaseCopyInput) (memory.ForeignProof, error) {
	if c := currentForeignCarrier(ctx); c != nil {
		c.discard(ref)
	}
	return s.source.Release(ctx, scope, auth, ref, report)
}

func (s flowSources) VerifyTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref memory.ForeignReference, proof memory.ForeignProof) error {
	return s.source.VerifyTx(ctx, tx, auth, ref, proof)
}

var _ memory.ForeignContentPort = flowSources{}
