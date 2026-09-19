package grpc_test

import (
	grpcbinding "lerna/adapters/transport/grpc"
	"testing"
	"time"
)

func TestFiniteConfiguration(t *testing.T) {
	good := grpcbinding.Config{MaxConnections: 4, MaxSessions: 8, MaxStreams: 4, MaxMessageBytes: 65536, SessionTTL: time.Minute, IOTimeout: time.Second, PollInterval: 20 * time.Millisecond}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := good
	bad.MaxStreams = 0
	if err := bad.Validate(); err == nil {
		t.Fatal("unbounded streams accepted")
	}
	bad = good
	bad.IOTimeout = 0
	if err := bad.Validate(); err == nil {
		t.Fatal("unbounded I/O accepted")
	}
}
