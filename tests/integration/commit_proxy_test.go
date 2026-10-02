package integration_test

import (
	"encoding/binary"
	"io"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/internal/durable"
	pg "github.com/ruipengliu/lerna/internal/storage/postgres"
)

// This proxy closes the client socket after the server has actually completed
// COMMIT, before forwarding that acknowledgement. The database is real.
func commitProxy(t *testing.T, original string) (string, *atomic.Bool) {
	t.Helper()
	u, err := url.Parse(original)
	if err != nil {
		t.Fatal("invalid proxy configuration")
	}
	target := u.Host
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	u.Host = listener.Addr().String()
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	armed := &atomic.Bool{}
	var wg sync.WaitGroup
	var connections sync.Map
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			server, err := net.DialTimeout("tcp", target, time.Second)
			if err != nil {
				client.Close()
				continue
			}
			connections.Store(client, server)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer connections.Delete(client)
				defer client.Close()
				defer server.Close()
				forwarded := make(chan struct{})
				go func() { _, _ = io.Copy(server, client); server.Close(); close(forwarded) }()
				for {
					var header [5]byte
					if _, err := io.ReadFull(server, header[:]); err != nil {
						break
					}
					length := int(binary.BigEndian.Uint32(header[1:]))
					if length < 4 || length > 1<<20 {
						break
					}
					payload := make([]byte, length-4)
					if _, err := io.ReadFull(server, payload); err != nil {
						break
					}
					if header[0] == 'C' && string(payload) == "COMMIT\x00" && armed.CompareAndSwap(true, false) {
						break
					}
					if _, err := client.Write(header[:]); err != nil {
						break
					}
					if _, err := client.Write(payload); err != nil {
						break
					}
				}
				client.Close()
				server.Close()
				<-forwarded
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		connections.Range(func(client, server any) bool { client.(net.Conn).Close(); server.(net.Conn).Close(); return true })
		wg.Wait()
	})
	return u.String(), armed
}
func TestPostgresRealCommitAcknowledgementLoss(t *testing.T) {
	s := newSuite(t, "postgres")
	proxy, armed := commitProxy(t, s.appURL)
	backend, err := pg.Open(ctx(), proxy, []durable.Scope{scope}, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	e, err := durable.New(backend, durable.Options{Kinds: []string{kind}})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := e.Register("probe")
	s.e, s.p = e, p
	i := intent(t, "task.submit", time.Now().Add(time.Hour), `{}`)
	armed.Store(true)
	record, result := e.Admit(ctx(), scope, "service", i, s.admission("wire-loss", false, false))
	if armed.Load() || result.Outcome != durable.CommitUnknown || record.Receipt != nil {
		t.Fatal("COMMIT acknowledgement was not lost", result.Outcome, result.Err)
	}
	if revision, _ := s.value(t, "wire-loss"); revision != 1 || s.job(t, "wire-loss").WorkRevision != 1 {
		t.Fatal("server did not durably commit before loss")
	}
	_, result = e.Admit(ctx(), scope, "service", i, s.admission("wire-loss", false, false))
	mustCommit(t, result)
	armed.Store(true)
	claims, result := e.Claim(ctx(), scope, kind, durable.NewID("boot"), 1, time.Second)
	if armed.Load() || result.Outcome != durable.CommitUnknown || len(claims) != 1 || claims[0].ObservedRevision() != 1 {
		t.Fatal("unknown real claim was not independently confirmed", result)
	}
	mustCommit(t, s.finish(t, claims[0], durable.Done(), false))
	if _, value := s.value(t, "wire-loss"); value != 1 {
		t.Fatal("lost acknowledgement repeated action")
	}
}
