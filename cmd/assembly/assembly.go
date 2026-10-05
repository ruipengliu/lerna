// Package assembly 供命令行宿主与一致性测试共用，只连接固定模块。
package assembly

import (
	"context"
	"time"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/content"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/sessions"
	"github.com/ruipengliu/lerna/core/tasks"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

type Harness struct {
	Sessions *sessions.Service
	Tasks    *tasks.Service
	Durable  *durable.Service
	Content  *content.Service
	store    *sqlite.Store
}

func Open(path, user, domain string) (*Harness, error) {
	s, err := sqlite.Open(path, user, domain)
	if err != nil {
		return nil, err
	}
	d := durable.New(s, user, domain)
	t := tasks.New(s, user, domain)
	c := content.New(s, user, domain+"/content")
	h := &Harness{Sessions: sessions.New(s, d, t, c, user, domain), Tasks: t, Durable: d, Content: c, store: s}
	// 固定受信宿主身份仅驱动已保存的责任，不替换原命令身份。
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	if err := h.Sessions.RecoverPending(ctx, &v1.Caller{UserId: user, IssuerId: "host-recovery"}); err != nil {
		s.Close()
		return nil, err
	}
	return h, nil
}
func (h *Harness) Close() error                     { return h.store.Close() }
func (h *Harness) StorageSettings() sqlite.Settings { return h.store.Settings() }
