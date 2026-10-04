package development

import (
	"context"
	"errors"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 只透传真实 Store；不改事务、返回值、holder 或输入。用于观察原 Job
// 将 dependency_unavailable 转为 waiting 前的业务错误，不记录正文或凭证。
type originalInputStoreObserver struct {
	runtime.Store
	t    *testing.T
	mu   sync.Mutex
	seen map[string]bool
}

func (s *originalInputStoreObserver) Within(ctx context.Context, scope runtime.Scope, parts []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	pc, _, _, _ := goruntime.Caller(1)
	caller := goruntime.FuncForPC(pc)
	status, err := s.Store.Within(ctx, scope, parts, fn)
	if caller != nil && strings.Contains(caller.Name(), "remoteTaskGate.PrepareTaskInput") {
		s.mu.Lock()
		if !s.seen["original_preparer_metadata"] {
			s.seen["original_preparer_metadata"] = true
			s.t.Logf("original input preparer metadata owner=%s status=%s error=%v", scope.OwnerID, status, err)
		}
		s.mu.Unlock()
	}
	var business *api.Error
	if errors.As(err, &business) {
		key := business.Code + "/" + business.Reason
		s.mu.Lock()
		if !s.seen[key] {
			s.seen[key] = true
			s.t.Logf("original input transaction owner=%s participants=%v status=%s code=%s reason=%s", scope.OwnerID, parts, status, business.Code, business.Reason)
		}
		s.mu.Unlock()
	}
	return status, err
}
