package development

import (
	"context"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type remoteProgressRefusal struct {
	Scope        runtime.Scope        `json:"scope"`
	Work         runtime.Work         `json:"work"`
	Participants []string             `json:"participants"`
	Status       runtime.CommitStatus `json:"status"`
	Cause        string               `json:"cause"`
}

// 只观察真实原 Job 和事务错误；保持实际 SQL、Claim、状态和原错误。
// 成功条件仍由公开 Task/Execution 和独立目标读回判断。
type remoteProgressStore struct {
	runtime.Store
	runtime.QueryBindingStore
	mu      sync.Mutex
	work    runtime.Work
	refusal *remoteProgressRefusal
}

func (s *remoteProgressStore) Claim(ctx context.Context, scope runtime.Scope, holder string, kinds []string, limit int, lease time.Duration) ([]runtime.Work, runtime.CommitStatus, error) {
	works, status, err := s.Store.Claim(ctx, scope, holder, kinds, limit, lease)
	if status == runtime.Committed && len(works) == 1 {
		s.mu.Lock()
		s.work = works[0]
		s.mu.Unlock()
	}
	return works, status, err
}

func (s *remoteProgressStore) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	status, err := s.Store.Within(ctx, scope, participants, fn)
	var reason string
	if err != nil {
		if api.IsCode(err, "dependency_unavailable") {
			reason = err.Error()
		}
	}
	if reason != "" {
		s.mu.Lock()
		if s.refusal == nil {
			s.refusal = &remoteProgressRefusal{scope, s.work, append([]string{}, participants...), status, reason}
		}
		s.mu.Unlock()
	}
	return status, err
}

func (s *remoteProgressStore) firstRefusal() *remoteProgressRefusal {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refusal == nil {
		return nil
	}
	copyRefusal := *s.refusal
	copyRefusal.Participants = append([]string{}, copyRefusal.Participants...)
	return &copyRefusal
}
