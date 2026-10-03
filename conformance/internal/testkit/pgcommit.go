//go:build integration

// Package testkit supplies bounded database-boundary fault fixtures.
package testkit

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
)

// CommitProxy drops one complete server COMMIT confirmation. It forwards the
// COMMIT request and waits for CommandComplete(COMMIT) and ReadyForQuery before
// disconnecting, so this fault does not stand in for process termination.
type CommitProxy struct {
	listener      net.Listener
	dsn, upstream string
	armed         atomic.Bool
	acknowledged  chan struct{}
	mu            sync.Mutex
	connections   []net.Conn
	wg            sync.WaitGroup
	ctx           context.Context
}

func NewCommitProxy(ctx context.Context, dsn string) (*CommitProxy, error) {
	if _, finite := ctx.Deadline(); !finite {
		return nil, errors.New("finite proxy deadline required")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid proxy database configuration")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	target := &url.URL{Scheme: "postgres", User: url.UserPassword(cfg.User, cfg.Password), Host: listener.Addr().String(), Path: "/" + cfg.Database, RawQuery: "sslmode=disable"}
	proxy := &CommitProxy{listener: listener, dsn: target.String(), upstream: net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))), acknowledged: make(chan struct{}), ctx: ctx}
	proxy.wg.Add(1)
	go func() {
		defer proxy.wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			proxy.mu.Lock()
			proxy.connections = append(proxy.connections, client)
			proxy.mu.Unlock()
			proxy.wg.Add(1)
			go func() { defer proxy.wg.Done(); proxy.serve(client) }()
		}
	}()
	return proxy, nil
}
func (p *CommitProxy) DSN() string                   { return p.dsn }
func (p *CommitProxy) Arm()                          { p.armed.Store(true) }
func (p *CommitProxy) Acknowledged() <-chan struct{} { return p.acknowledged }
func (p *CommitProxy) Close() {
	p.listener.Close()
	p.mu.Lock()
	for _, connection := range p.connections {
		connection.Close()
	}
	p.mu.Unlock()
	p.wg.Wait()
}
func readFrame(reader io.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(header[1:])
	if length < 4 || length > 1<<22 {
		return 0, nil, errors.New("invalid PostgreSQL fixture frame")
	}
	frame := make([]byte, int(length)+1)
	copy(frame, header[:])
	_, err := io.ReadFull(reader, frame[5:])
	return header[0], frame, err
}
func (p *CommitProxy) serve(client net.Conn) {
	defer client.Close()
	server, err := (&net.Dialer{}).DialContext(p.ctx, "tcp", p.upstream)
	if err != nil {
		return
	}
	defer server.Close()
	p.mu.Lock()
	p.connections = append(p.connections, server)
	p.mu.Unlock()
	deadline, _ := p.ctx.Deadline()
	client.SetDeadline(deadline)
	server.SetDeadline(deadline)
	var header [4]byte
	if _, err = io.ReadFull(client, header[:]); err != nil {
		return
	}
	length := binary.BigEndian.Uint32(header[:])
	if length < 8 || length > 1<<20 {
		return
	}
	startup := make([]byte, int(length))
	copy(startup, header[:])
	if _, err = io.ReadFull(client, startup[4:]); err != nil {
		return
	}
	if _, err = server.Write(startup); err != nil {
		return
	}
	var suppress atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer client.Close()
		committed := false
		for {
			kind, frame, err := readFrame(server)
			if err != nil {
				return
			}
			if suppress.Load() {
				if kind == 'C' && bytes.Equal(frame[5:], []byte("COMMIT\x00")) {
					committed = true
				}
				if kind == 'Z' {
					if committed {
						close(p.acknowledged)
					}
					return
				}
				continue
			}
			if _, err = client.Write(frame); err != nil {
				return
			}
		}
	}()
	for {
		kind, frame, err := readFrame(client)
		if err != nil {
			break
		}
		if kind == 'Q' && strings.EqualFold(strings.TrimSuffix(string(frame[5:]), "\x00"), "commit") && p.armed.CompareAndSwap(true, false) {
			suppress.Store(true)
		}
		if _, err = server.Write(frame); err != nil {
			break
		}
	}
	server.Close()
	<-done
}
