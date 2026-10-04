package content

import (
	"context"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_2"
)

var ErrOrphanReferenced = errors.New("Content orphan candidate already has a published reference")

// SealOrphan consumes only an original registered, unpublished version. A
// published reference wins the same version lock and cannot be orphan-deleted.
func (l *Lifecycle) SealOrphan(ctx context.Context, subject *v.SubjectBinding, request SealRequest) (BodyCleanupObservation, error) {
	return l.seal(ctx, subject, request, true)
}
