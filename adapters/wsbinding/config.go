// Package wsbinding binds read-only capability discovery and invocation views to
// a bounded, authenticated edge/cloud WebSocket connection.
package wsbinding

import (
	"lerna/authorization"
	"time"
)

type Config struct {
	MaxConnections, Window, MaxMessage, Chunk int
	Timeout, Lifetime, Poll                   time.Duration
}

func (c Config) Validate() error {
	if c.MaxConnections < 1 || c.MaxConnections > 32 || c.Window < 1 || c.Window > 32 || c.MaxMessage < 4096 || c.MaxMessage > 1<<20 || c.Chunk < 512 || c.Chunk > c.MaxMessage/2 || c.Timeout < 10*time.Millisecond || c.Timeout > 5*time.Second || c.Lifetime < c.Timeout || c.Lifetime > 10*time.Minute || c.Poll < 10*time.Millisecond || c.Poll > time.Second {
		return failure(authorization.Invalid)
	}
	return nil
}
func failure(c authorization.Code) error { return &authorization.Error{Code: c} }
