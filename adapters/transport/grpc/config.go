// Package grpc binds the existing capability contract to authenticated
// same-side gRPC processes. Hosts retain domain services and their storage.
package grpc

import (
	"lerna/authorization"
	"time"
)

type Config struct {
	MaxConnections, MaxSessions, MaxStreams, MaxMessageBytes int
	SessionTTL, IOTimeout, PollInterval                      time.Duration
}

func (c Config) Validate() error {
	if c.MaxConnections < 1 || c.MaxConnections > 32 || c.MaxSessions < 1 || c.MaxSessions > 128 || c.MaxStreams < 1 || c.MaxStreams > 32 || c.MaxMessageBytes < 4096 || c.MaxMessageBytes > 1<<20 || c.SessionTTL < time.Second || c.SessionTTL > 10*time.Minute || c.IOTimeout < time.Millisecond || c.IOTimeout > 5*time.Second || c.PollInterval < 10*time.Millisecond || c.PollInterval > time.Second {
		return failure(authorization.Invalid)
	}
	return nil
}
func failure(c authorization.Code) error { return &authorization.Error{Code: c} }
