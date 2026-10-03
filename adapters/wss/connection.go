package wss

import (
	"context"
	"encoding/json"

	"github.com/ruipengliu/lerna/runtime"
)

// ConnectionInfo 固定原外socket；Emit只进入该socket的既有有界唯一发送循环。
// Stop取消实际外连接，内部重绑不能另发外Ready。
type ConnectionInfo struct {
	ConnectionID  string
	OwnerID       string
	MethodsDigest string
	Emit          func(json.RawMessage, bool) error
	EmitChecked   func(json.RawMessage, bool, func(context.Context) (func(), error)) error
	Stop          func()
}
type Connection interface {
	Processor
	Close() error
}
type ConnectionProcessor interface {
	Open(context.Context, runtime.Auth, ConnectionInfo) (Connection, error)
}
type PendingResponse interface {
	Wait(context.Context) (string, json.RawMessage, error)
}
type DisclosureGate interface {
	BeginDisclosure(context.Context) (func(), error)
}

// SequentialConnection 的Begin在外读循环内调用；Wait可并发，原外seq不乱序入队。
type SequentialConnection interface {
	Begin(context.Context, runtime.Auth, uint64, string, json.RawMessage) (PendingResponse, error)
}
type InboundConnection interface {
	Receive(context.Context, json.RawMessage) error
}
