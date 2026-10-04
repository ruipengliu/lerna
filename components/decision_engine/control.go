package decision_engine

import (
	"context"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

// Cancel adopts trusted stop control for this Decision owner only. It does not
// claim that an already in-flight independent publisher has physically stopped.
func (s *Service) Cancel(context.Context, []byte, *v.SubjectBinding) (v.TransportOutcome, error) {
	return v.TransportOutcome{}, ErrUnavailable
}
