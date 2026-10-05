// Package assembly 供命令行宿主与一致性测试共用，只连接固定模块。
package assembly

import (
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
	return &Harness{Sessions: sessions.New(s, d, t, c, user, domain), Tasks: t, Durable: d, Content: c, store: s}, nil
}
func (h *Harness) Close() error                     { return h.store.Close() }
func (h *Harness) StorageSettings() sqlite.Settings { return h.store.Settings() }
