// Package durablework assembles the internal durable work demonstration ports.
package durablework

import (
	"github.com/ruipengliu/lerna/contract"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

// Host exposes internal demonstration seams, not a public SDK contract.
type Host = demo.Service
type Permission = demo.Permission
type Permissions = demo.Permissions

func NewPermissions(entries []Permission) *Permissions { return demo.NewPermissions(entries) }

// Storage is the actual same-database bundle explicitly injected by the host.
type Storage interface {
	runtime.Clock
	demo.Repository
	contract.CommandFactReader
}

func New(owner contract.OwnerRef, runner runtime.TxRunner, commands runtime.CommandStore, jobs runtime.JobStore, storage Storage, permissions *Permissions) *Host {
	service := &demo.Service{Owner: owner, Runner: runner, Commands: commands, Jobs: jobs, Clock: storage, Repository: storage, Permissions: permissions, Reader: storage}
	service.Retention, _ = storage.(demo.RetentionRepository)
	return service
}
