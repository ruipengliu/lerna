package authorization_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"golang.org/x/net/netutil"
	"google.golang.org/protobuf/proto"
	"io"
	"lerna/adapters/josegrant"
	"lerna/adapters/sqliteauth"
	wire "lerna/gen/harness/v1"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"lerna/adapters/nodetls"
	"lerna/authorization"
)

func TestNodeEnrollmentDisableAndReplay(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	ca, err := nodetls.GenerateCA(f.clock.now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := nodetls.NewIssuer(ca)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := f.s.Nodes(authorization.NodeConfig{MaxTTL: time.Hour, RequestTTL: time.Minute, IOTimeout: time.Second, MaxRecords: 16, MaxOperations: 64}, signer)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	op, _ := f.s.NewOperation(ctx, f.token)
	in := authorization.NodeMutation{OperationID: op, Namespace: "local", Node: "node", Kind: "ENROLL", ExpectedRevision: 1, Subjects: []string{"admin"}, RequestExpires: f.clock.now.Add(time.Minute).Unix(), Expires: f.clock.now.Add(time.Hour).Unix()}
	in.CSR, err = nodetls.Request(key, in)
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := nodes.Mutate(ctx, f.token, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := nodes.Check(ctx, "local", "node", enrolled.CertificateSHA256, "admin"); err != nil {
		t.Fatal(err)
	}
	replay, err := nodes.Mutate(ctx, f.token, in)
	if err != nil || replay.CertificateSHA256 != enrolled.CertificateSHA256 {
		t.Fatalf("replay: %v", err)
	}
	op, _ = f.s.NewOperation(ctx, f.token)
	_, err = nodes.Mutate(ctx, f.token, authorization.NodeMutation{OperationID: op, Namespace: "local", Node: "node", Kind: "DISABLE", ExpectedRevision: enrolled.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if err = nodes.Check(ctx, "local", "node", enrolled.CertificateSHA256, "admin"); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("disabled: %v", err)
	}
	if _, err = nodes.Mutate(ctx, f.token, in); err != nil {
		t.Fatal(err)
	}
	if err = nodes.Check(ctx, "local", "node", enrolled.CertificateSHA256, "admin"); err == nil {
		t.Fatal("replay restored disabled node")
	}
}

func (f *grantFixture) enroll(t *testing.T, nodes *authorization.NodeAuthority, node string, key *ecdsa.PrivateKey, kind string) authorization.NodeRecord {
	t.Helper()
	ctx := context.Background()
	snap, err := f.db.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.s.NewOperation(ctx, f.token)
	if err != nil {
		t.Fatal(err)
	}
	in := authorization.NodeMutation{OperationID: op, Namespace: "local", Node: node, Kind: kind, ExpectedRevision: snap.State.Revision, Subjects: []string{"admin"}, RequestExpires: f.clock.now.Add(time.Minute).Unix(), Expires: f.clock.now.Add(time.Hour).Unix()}
	in.CSR, err = nodetls.Request(key, in)
	if err != nil {
		t.Fatal(err)
	}
	r, err := nodes.Mutate(ctx, f.token, in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRealTLSReservationRotationAndDisable(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	ca, err := nodetls.GenerateCA(f.clock.now, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := nodetls.NewIssuer(ca)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := f.s.Nodes(authorization.NodeConfig{MaxTTL: time.Hour, RequestTTL: time.Minute, IOTimeout: time.Second, MaxRecords: 16, MaxOperations: 128}, issuer)
	if err != nil {
		t.Fatal(err)
	}
	clientKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	serverKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	clientRecord := f.enroll(t, nodes, "node", clientKey, "ENROLL")
	serverRecord := f.enroll(t, nodes, "receiver", serverKey, "ENROLL")
	// Separate node directories hold only that node's private key and certificate.
	for _, v := range []struct {
		name string
		key  *ecdsa.PrivateKey
		r    authorization.NodeRecord
	}{{"client", clientKey, clientRecord}, {"server", serverKey, serverRecord}} {
		dir := t.TempDir()
		raw, e := x509.MarshalPKCS8PrivateKey(v.key)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}), 0600); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, "certificate.der"), v.r.Certificate, 0600); e != nil {
			t.Fatal(e)
		}
		loaded, e := josegrant.LoadPrivateKey(filepath.Join(dir, "key.pem"))
		if e != nil {
			t.Fatal(e)
		}
		if v.name == "client" {
			clientKey = loaded
		} else {
			serverKey = loaded
		}
	}
	trust := []nodetls.Trust{{Certificate: ca.Certificate[0], Until: f.clock.now.Add(2 * time.Hour)}}
	endpoint := func(record authorization.NodeRecord, key *ecdsa.PrivateKey) *nodetls.Endpoint {
		t.Helper()
		e, err := nodetls.NewEndpoint(nodes, f.clock, "local", record.Node, tls.Certificate{Certificate: [][]byte{record.Certificate}, PrivateKey: key}, trust, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	serverEndpoint := endpoint(serverRecord, serverKey)
	content, contentBinding, contentRef := nodeContent(t, f)
	var calls atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.ContentLength > 0 {
			http.Error(w, "invalid request", 400)
			return
		}
		p, e := serverEndpoint.Presentation(r.Context(), *r.TLS, r.Header.Get("Subject"), "business-read", strings.Repeat("a", 64))
		if e != nil {
			http.Error(w, "denied", 403)
			return
		}
		action := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
		permit, e := f.g.ReserveUse(r.Context(), r.Header.Get("Grant"), p, action, 1)
		if e != nil {
			http.Error(w, "denied", 403)
			return
		}
		if permit.Units != 1 {
			http.Error(w, "invalid allocation", 500)
			return
		}
		if r.TLS.DidResume {
			w.Header().Set("Resumed", "true")
		}
		if e = nodes.RememberPresentation(r.Context(), p, r.TLS.PeerCertificates[0].Raw); e != nil {
			http.Error(w, "denied", 403)
			return
		}
		result, e := content.Call(r.Context(), contentBinding, &wire.ContentRequest{Method: "READ", Ref: contentRef, Purpose: "task", Limit: 5})
		if e != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		// Recheck immediately before disclosure; the local read grants no network identity.
		if _, e = f.g.Verify(r.Context(), r.Header.Get("Grant"), p, action); e != nil {
			http.Error(w, "denied", 403)
			return
		}
		w.Write(result.Data)
	}))
	server.Listener = netutil.LimitListener(server.Listener, 4)
	server.Config.ReadHeaderTimeout = time.Second
	server.Config.ReadTimeout = 2 * time.Second
	server.Config.WriteTimeout = 2 * time.Second
	server.Config.IdleTimeout = time.Second
	server.Config.MaxHeaderBytes = 4096
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = serverEndpoint.ServerConfig()
	server.StartTLS()
	defer server.Close()
	// A peer that never sends its handshake cannot hold a host connection forever.
	slow, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = slow.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var probe [1]byte
	_, err = slow.Read(probe[:])
	slow.Close()
	if err == nil {
		t.Fatal("incomplete handshake unexpectedly succeeded")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("server did not enforce handshake deadline")
	}

	makeClient := func(e *nodetls.Endpoint) (*http.Client, *http.Transport) {
		c, err := e.ClientConfig("receiver")
		if err != nil {
			t.Fatal(err)
		}
		tr := &http.Transport{TLSClientConfig: c, TLSHandshakeTimeout: time.Second, MaxConnsPerHost: 2}
		t.Cleanup(tr.CloseIdleConnections)
		return &http.Client{Transport: tr, Timeout: 2 * time.Second}, tr
	}
	client, tr := makeClient(endpoint(clientRecord, clientKey))
	f.spec.CertificateSha256 = clientRecord.CertificateSHA256
	snap, _ := f.db.Load(ctx)
	receipt, err := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", snap.State.Revision))
	if err != nil {
		t.Fatal(err)
	}
	request := func(c *http.Client, material, subject string) (int, bool) {
		t.Helper()
		req, _ := http.NewRequest("GET", server.URL, nil)
		req.Header.Set("Grant", material)
		req.Header.Set("Subject", subject)
		req.Header.Set("X-Forwarded-User", "admin")
		response, err := c.Do(req)
		if err != nil {
			return 0, false
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode == 200 && string(data) != "hello" {
			t.Fatalf("wrong business content: %q", data)
		}
		return response.StatusCode, response.Header.Get("Resumed") == "true"
	}
	if status, _ := request(client, receipt.Material, "admin"); status != 200 {
		t.Fatalf("first call: %d", status)
	}
	// Independent certificates exercise the server's actual TLS rejection path.
	leaf, _ := x509.ParseCertificate(clientRecord.Certificate)
	root, _ := x509.ParseCertificate(ca.Certificate[0])
	for _, bad := range []struct {
		name string
		edit func(*x509.Certificate)
	}{
		{"wrong-namespace", func(c *x509.Certificate) {
			u := *c.URIs[0]
			q := u.Query()
			q.Set("namespace", "foreign")
			u.RawQuery = q.Encode()
			c.URIs = []*url.URL{&u}
		}},
		{"wrong-node", func(c *x509.Certificate) {
			u := *c.URIs[0]
			q := u.Query()
			q.Set("node", "other")
			u.RawQuery = q.Encode()
			c.URIs = []*url.URL{&u}
		}},
		{"unregistered-certificate", func(c *x509.Certificate) {}},
		{"wrong-purpose", func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth} }},
		{"expired", func(c *x509.Certificate) {
			c.NotAfter = f.clock.now.Add(-time.Second)
			c.NotBefore = f.clock.now.Add(-time.Hour)
		}},
		{"not-yet-valid", func(c *x509.Certificate) { c.NotBefore = f.clock.now.Add(time.Minute) }},
	} {
		t.Run(bad.name, func(t *testing.T) {
			template := *leaf
			template.SerialNumber = big.NewInt(998)
			bad.edit(&template)
			der, e := x509.CreateCertificate(rand.Reader, &template, root, &clientKey.PublicKey, ca.PrivateKey)
			if e != nil {
				t.Fatal(e)
			}
			config, e := endpoint(clientRecord, clientKey).ClientConfig("receiver")
			if e != nil {
				t.Fatal(e)
			}
			config.Certificates = []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: clientKey}}
			badTransport := &http.Transport{TLSClientConfig: config}
			defer badTransport.CloseIdleConnections()
			if status, _ := request(&http.Client{Transport: badTransport, Timeout: 2 * time.Second}, receipt.Material, "admin"); status != 0 {
				t.Fatalf("bad certificate reached business: %d", status)
			}
		})
	}
	t.Run("untrusted-root-with-registered-leaf", func(t *testing.T) {
		rogueCA, e := nodetls.GenerateCA(f.clock.now, 2*time.Hour)
		if e != nil {
			t.Fatal(e)
		}
		rogueIssuer, e := nodetls.NewIssuer(rogueCA)
		if e != nil {
			t.Fatal(e)
		}
		rogueNodes, e := f.s.Nodes(authorization.NodeConfig{MaxTTL: time.Hour, RequestTTL: time.Minute, IOTimeout: time.Second, MaxRecords: 16, MaxOperations: 128}, rogueIssuer)
		if e != nil {
			t.Fatal(e)
		}
		rogue := f.enroll(t, rogueNodes, "rogue", clientKey, "ENROLL")
		c, e := endpoint(clientRecord, clientKey).ClientConfig("receiver")
		if e != nil {
			t.Fatal(e)
		}
		c.Certificates = []tls.Certificate{{Certificate: [][]byte{rogue.Certificate}, PrivateKey: clientKey}}
		transport := &http.Transport{TLSClientConfig: c}
		defer transport.CloseIdleConnections()
		if status, _ := request(&http.Client{Transport: transport, Timeout: 2 * time.Second}, receipt.Material, "admin"); status != 0 {
			t.Fatal("registered leaf bypassed root trust")
		}
	})
	t.Run("no-certificate-no-fallback", func(t *testing.T) {
		c, e := endpoint(clientRecord, clientKey).ClientConfig("receiver")
		if e != nil {
			t.Fatal(e)
		}
		c.Certificates = nil
		transport := &http.Transport{TLSClientConfig: c}
		defer transport.CloseIdleConnections()
		if status, _ := request(&http.Client{Transport: transport, Timeout: 2 * time.Second}, receipt.Material, "admin"); status != 0 {
			t.Fatal("uncertified identity header accepted")
		}
	})
	t.Run("wrong-target", func(t *testing.T) {
		c, e := endpoint(clientRecord, clientKey).ClientConfig("other")
		if e != nil {
			t.Fatal(e)
		}
		transport := &http.Transport{TLSClientConfig: c}
		defer transport.CloseIdleConnections()
		if status, _ := request(&http.Client{Transport: transport, Timeout: 2 * time.Second}, receipt.Material, "admin"); status != 0 {
			t.Fatal("wrong target accepted")
		}
	})
	if _, err := serverEndpoint.RestorePresentation(ctx, clientRecord.Certificate, "admin", "business-read", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if status, _ := request(client, receipt.Material, "impostor"); status != 403 {
		t.Fatalf("self-reported subject: %d", status)
	}
	tr.CloseIdleConnections()
	if status, resumed := request(client, receipt.Material, "admin"); status != 200 || !resumed {
		t.Fatalf("real session resumption: %d %v", status, resumed)
	}
	saved, err := nodes.LookupPresentation(ctx, f.token, "business-read")
	if err != nil {
		t.Fatal(err)
	}
	recoveryDB, err := sqliteauth.Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer recoveryDB.Close()
	recoveredAuth, err := authorization.New(recoveryDB, f.clock, config())
	if err != nil {
		t.Fatal(err)
	}
	recoveredNodes, err := recoveredAuth.Nodes(authorization.NodeConfig{MaxTTL: time.Hour, RequestTTL: time.Minute, IOTimeout: time.Second, MaxRecords: 16, MaxOperations: 128}, issuer)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := recoveredNodes.LookupPresentation(ctx, f.token, "business-read")
	if err != nil || !bytes.Equal(saved.Certificate, recovered.Certificate) || saved.Presentation != recovered.Presentation {
		t.Fatalf("original ingress not restored: %v", err)
	}
	recoveredEndpoint, err := nodetls.NewEndpoint(recoveredNodes, f.clock, "local", "receiver", tls.Certificate{Certificate: [][]byte{serverRecord.Certificate}, PrivateKey: serverKey}, trust, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	recoveredGrants, err := recoveredAuth.SignedGrants(f.cfg, f.crypto)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := recoveredEndpoint.RestorePresentation(ctx, recovered.Certificate, recovered.Presentation.Subject, recovered.Presentation.OperationID, recovered.Presentation.SemanticSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = recoveredGrants.Verify(ctx, receipt.Material, restored, &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err = recoveredGrants.LookupUse(ctx, restored); err != nil {
		t.Fatal(err)
	}
	read, err := content.Call(ctx, contentBinding, &wire.ContentRequest{Method: "READ", Ref: contentRef, Purpose: "task", Limit: 5})
	if err != nil || string(read.Data) != "hello" {
		t.Fatalf("restored business read: %v", err)
	}
	oldCalls := calls.Load()
	newKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rotated := f.enroll(t, nodes, "node", newKey, "ROTATE")
	if status, _ := request(client, receipt.Material, "admin"); status != 403 {
		t.Fatalf("old persistent connection: %d", status)
	}
	tr.CloseIdleConnections()
	if status, _ := request(client, receipt.Material, "admin"); status != 0 {
		t.Fatalf("old resumed session: %d", status)
	}
	if _, err := recoveredEndpoint.RestorePresentation(ctx, recovered.Certificate, "admin", "business-read", strings.Repeat("a", 64)); err == nil {
		t.Fatal("restored retired certificate")
	}
	if calls.Load() != oldCalls+1 {
		t.Fatal("retired session reached business handler")
	}
	next, nextTransport := makeClient(endpoint(rotated, newKey))
	if status, _ := request(next, receipt.Material, "admin"); status != 403 {
		t.Fatalf("old grant on new key: %d", status)
	}
	snap, _ = f.db.Load(ctx)
	op, _ := f.s.NewOperation(ctx, f.token)
	rebind := &wire.GrantMutation{OperationId: op, Kind: "REBIND", GrantId: receipt.GrantId, ExpectedRevision: snap.State.Revision, ExpectedGrantRevision: receipt.Revision}
	rebound, err := f.g.Mutate(ctx, f.token, rebind)
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := request(next, rebound.Material, "admin"); status != 200 {
		t.Fatalf("rebound original operation: %d", status)
	}
	grant, err := f.g.Get(ctx, f.token, receipt.GrantId)
	if err != nil || grant.Allocated != 1 || grant.Spec.CertificateSha256 != clientRecord.CertificateSHA256 {
		t.Fatalf("rotation changed original allocation/scope: %v %v", grant, err)
	}
	replay, err := f.g.Mutate(ctx, f.token, rebind)
	if err != nil || replay.Material != rebound.Material {
		t.Fatalf("rebind replay: %v", err)
	}
	snap, _ = f.db.Load(ctx)
	op, _ = f.s.NewOperation(ctx, f.token)
	_, err = nodes.Mutate(ctx, f.token, authorization.NodeMutation{OperationID: op, Namespace: "local", Node: "node", Kind: "DISABLE", ExpectedRevision: snap.State.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := request(next, rebound.Material, "admin"); status != 403 {
		t.Fatalf("disabled established connection: %d", status)
	}
	nextTransport.CloseIdleConnections()
	if status, _ := request(next, rebound.Material, "admin"); status != 0 {
		t.Fatalf("disabled resumed session: %d", status)
	}
}

func TestNodeRequestValidationAndReEnrollment(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	ca, _ := nodetls.GenerateCA(f.clock.now, 2*time.Hour)
	issuer, _ := nodetls.NewIssuer(ca)
	nodes, err := f.s.Nodes(authorization.NodeConfig{MaxTTL: time.Hour, RequestTTL: time.Minute, IOTimeout: time.Second, MaxRecords: 16, MaxOperations: 128}, issuer)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	op, _ := f.s.NewOperation(ctx, f.token)
	in := authorization.NodeMutation{OperationID: op, Namespace: "local", Node: "node", Kind: "ENROLL", ExpectedRevision: 1, Subjects: []string{"admin"}, RequestExpires: f.clock.now.Add(time.Minute).Unix(), Expires: f.clock.now.Add(time.Hour).Unix()}
	in.CSR, err = nodetls.Request(key, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name string
		edit func(*authorization.NodeMutation)
	}{
		{"namespace", func(r *authorization.NodeMutation) { r.Namespace = "other" }},
		{"node", func(r *authorization.NodeMutation) { r.Node = "other" }},
		{"subjects", func(r *authorization.NodeMutation) { r.Subjects = []string{"unknown"} }},
		{"expiry", func(r *authorization.NodeMutation) { r.Expires++ }},
		{"expired-request", func(r *authorization.NodeMutation) { r.RequestExpires = f.clock.now.Unix() }},
		{"oversized", func(r *authorization.NodeMutation) { r.CSR = make([]byte, 8193) }},
		{"forged-proof", func(r *authorization.NodeMutation) { r.CSR = append([]byte(nil), r.CSR...); r.CSR[len(r.CSR)-1] ^= 1 }},
	} {
		t.Run(change.name, func(t *testing.T) {
			bad := in
			change.edit(&bad)
			if _, err := nodes.Mutate(ctx, f.token, bad); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
	if _, err = nodes.Mutate(ctx, "invalid", in); err == nil {
		t.Fatal("untrusted manager accepted")
	}
	record, err := nodes.Mutate(ctx, f.token, in)
	if err != nil {
		t.Fatal(err)
	}
	// A management operation cannot acquire a different meaning in another partition.
	if _, err = f.g.Mutate(ctx, f.token, &wire.GrantMutation{OperationId: op, Kind: "ISSUE", Spec: f.spec}); !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatalf("operation reuse: %v", err)
	}
	op, _ = f.s.NewOperation(ctx, f.token)
	_, err = nodes.Mutate(ctx, f.token, authorization.NodeMutation{OperationID: op, Namespace: "local", Node: "node", Kind: "DISABLE", ExpectedRevision: record.Revision})
	if err != nil {
		t.Fatal(err)
	}
	newKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	replacement := f.enroll(t, nodes, "node", newKey, "REENROLL")
	if len(replacement.Previous) != 0 {
		t.Fatal("lost key retained old grant qualification")
	}
	if err = nodes.Check(ctx, "local", "node", record.CertificateSHA256, "admin"); err == nil {
		t.Fatal("old key revived")
	}
	if err = nodes.Check(ctx, "local", "node", replacement.CertificateSHA256, "admin"); err != nil {
		t.Fatal(err)
	}
	f.clock.now = f.clock.now.Add(time.Hour)
	if err = nodes.Check(ctx, "local", "node", replacement.CertificateSHA256, "admin"); err == nil {
		t.Fatal("expired registration accepted")
	}
}

func TestNodeSigningKeyOverlapAndCurrentGrantRevocation(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	oldKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	newKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cutoff := f.clock.now.Add(time.Minute)
	oldSigner, err := josegrant.NewRotating("old", oldKey, map[string]josegrant.VerificationKey{"old": {Public: &oldKey.PublicKey, Until: cutoff}}, f.clock)
	if err != nil {
		t.Fatal(err)
	}
	f.g, err = f.s.SignedGrants(f.cfg, oldSigner)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
	if err != nil {
		t.Fatal(err)
	}
	signer, err := josegrant.NewRotating("new", newKey, map[string]josegrant.VerificationKey{"old": {Public: &oldKey.PublicKey, Until: cutoff}, "new": {Public: &newKey.PublicKey, Until: f.clock.now.Add(time.Hour)}}, f.clock)
	if err != nil {
		t.Fatal(err)
	}
	f.g, err = f.s.SignedGrants(f.cfg, signer)
	if err != nil {
		t.Fatal(err)
	}
	p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Presenter: "node", Audience: "receiver", CertificateSHA256: f.spec.CertificateSha256}
	action := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
	if _, err = f.g.Verify(ctx, receipt.Material, p, action); err != nil {
		t.Fatal(err)
	}
	f.clock.now = cutoff
	if _, err = f.g.Verify(ctx, receipt.Material, p, action); err == nil {
		t.Fatal("old signing key accepted after overlap")
	}
	op, _ := f.s.NewOperation(ctx, f.token)
	rebound, err := f.g.Mutate(ctx, f.token, &wire.GrantMutation{OperationId: op, Kind: "REBIND", GrantId: receipt.GrantId, ExpectedRevision: 2, ExpectedGrantRevision: receipt.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.g.Verify(ctx, rebound.Material, p, action); err != nil {
		t.Fatal(err)
	}
	op, _ = f.s.NewOperation(ctx, f.token)
	_, err = f.g.Mutate(ctx, f.token, &wire.GrantMutation{OperationId: op, Kind: "REVOKE", GrantId: receipt.GrantId, ExpectedRevision: 3, ExpectedGrantRevision: receipt.Revision})
	if err != nil {
		t.Fatal(err)
	}
	op, _ = f.s.NewOperation(ctx, f.token)
	if _, err = f.g.Mutate(ctx, f.token, &wire.GrantMutation{OperationId: op, Kind: "REBIND", GrantId: receipt.GrantId, ExpectedRevision: 4, ExpectedGrantRevision: 4}); err == nil {
		t.Fatal("revoked grant re-signed")
	}
	if _, err = f.g.Verify(ctx, rebound.Material, p, action); err == nil {
		t.Fatal("re-sign bypassed revocation")
	}
}

type interruptedNodeIssuer struct {
	authorization.NodeIssuer
	after func()
}

func (i interruptedNodeIssuer) Issue(ctx context.Context, in authorization.NodeMutation, now time.Time) ([]byte, error) {
	der, err := i.NodeIssuer.Issue(ctx, in, now)
	if err == nil {
		i.after()
	}
	return der, err
}

type nodeLostReplyStore struct {
	authorization.Store
	lost bool
}

func (s *nodeLostReplyStore) Commit(ctx context.Context, v uint64, st authorization.State) error {
	err := s.Store.Commit(ctx, v, st)
	if err == nil && !s.lost && st.Nodes != nil && len(st.Nodes.Operations) > 0 {
		s.lost = true
		return &authorization.Error{Code: authorization.OutcomeUnknown}
	}
	return err
}

func TestNodeSQLiteRecoveryAndSigningRace(t *testing.T) {
	for _, mode := range []string{"reply-lost", "signing-race"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "authority.db")
			db, err := sqliteauth.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			clock := &grantClock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
			var store authorization.Store = db
			if mode == "reply-lost" {
				store = &nodeLostReplyStore{Store: db}
			}
			svc, err := authorization.New(store, clock, config())
			if err != nil {
				t.Fatal(err)
			}
			token, err := svc.Bootstrap(ctx, "local", "admin")
			if err != nil {
				t.Fatal(err)
			}
			ca, _ := nodetls.GenerateCA(clock.now, 2*time.Hour)
			issuer, _ := nodetls.NewIssuer(ca)
			cfg := authorization.NodeConfig{MaxTTL: time.Hour, RequestTTL: time.Minute, IOTimeout: time.Second, MaxRecords: 16, MaxOperations: 64}
			key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			op, _ := svc.NewOperation(ctx, token)
			in := authorization.NodeMutation{OperationID: op, Namespace: "local", Node: "node", Kind: "ENROLL", Subjects: []string{"admin"}, RequestExpires: clock.now.Add(time.Minute).Unix(), Expires: clock.now.Add(time.Hour).Unix()}
			in.CSR, err = nodetls.Request(key, in)
			if err != nil {
				t.Fatal(err)
			}
			var issuing authorization.NodeIssuer = issuer
			if mode == "signing-race" {
				issuing = interruptedNodeIssuer{issuer, func() {
					competing, err := sqliteauth.Open(path)
					if err != nil {
						t.Fatal(err)
					}
					defer competing.Close()
					other, err := authorization.New(competing, clock, config())
					if err != nil {
						t.Fatal(err)
					}
					id, _ := other.NewOperation(ctx, token)
					_, err = other.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: id, Command: &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{}}}})
					if err != nil {
						t.Fatal(err)
					}
				}}
			}
			nodes, err := svc.Nodes(cfg, issuing)
			if err != nil {
				t.Fatal(err)
			}
			result, err := nodes.Mutate(ctx, token, in)
			expected := authorization.OutcomeUnknown
			if mode == "signing-race" {
				expected = authorization.Conflict
			}
			if !authorization.Is(err, expected) || len(result.Certificate) != 0 {
				t.Fatalf("unconfirmed certificate delivered: %v %v", result, err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := sqliteauth.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			svc, err = authorization.New(reopened, clock, config())
			if err != nil {
				t.Fatal(err)
			}
			nodes, err = svc.Nodes(cfg, issuer)
			if err != nil {
				t.Fatal(err)
			}
			original, err := nodes.LookupOperation(ctx, token, op)
			if mode == "signing-race" {
				if !authorization.Is(err, authorization.NotFound) {
					t.Fatalf("uncommitted operation: %v", err)
				}
				return
			}
			if err != nil || len(original.Certificate) == 0 {
				t.Fatalf("lost reply recovery: %v", err)
			}
			replay, err := nodes.Mutate(ctx, token, in)
			if err != nil || replay.CertificateSHA256 != original.CertificateSHA256 {
				t.Fatalf("recovery reissued: %v", err)
			}
			id, _ := svc.NewOperation(ctx, token)
			disabled, err := nodes.Mutate(ctx, token, authorization.NodeMutation{OperationID: id, Namespace: "local", Node: "node", Kind: "DISABLE", ExpectedRevision: original.Revision})
			if err != nil {
				t.Fatal(err)
			}
			restored, err := sqliteauth.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			other, _ := authorization.New(restored, clock, config())
			current, _ := other.Nodes(cfg, issuer)
			record, err := current.Get(ctx, token, "node")
			if err != nil || !record.Disabled || record.Revision != disabled.Revision {
				t.Fatal("disabled revision not recovered", err)
			}
			if err = current.Check(ctx, "local", "node", original.CertificateSHA256, "admin"); err == nil {
				t.Fatal("reopen bypassed disable")
			}
		})
	}
}

func TestNodeCertificateIssuerOverlapAndUnavailableRegistry(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	ca, _ := nodetls.GenerateCA(f.clock.now, 2*time.Hour)
	issuer, _ := nodetls.NewIssuer(ca)
	cfg := authorization.NodeConfig{MaxTTL: time.Hour, RequestTTL: time.Minute, IOTimeout: time.Second, MaxRecords: 16, MaxOperations: 128}
	nodes, err := f.s.Nodes(cfg, issuer)
	if err != nil {
		t.Fatal(err)
	}
	clientKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	serverKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	client := f.enroll(t, nodes, "node", clientKey, "ENROLL")
	server := f.enroll(t, nodes, "receiver", serverKey, "ENROLL")
	nextCA, _ := nodetls.GenerateCA(f.clock.now, 2*time.Hour)
	nextIssuer, _ := nodetls.NewIssuer(nextCA)
	cutoff := f.clock.now.Add(time.Minute)
	trust := []nodetls.Trust{{Certificate: ca.Certificate[0], Until: cutoff}, {Certificate: nextCA.Certificate[0], Until: f.clock.now.Add(2 * time.Hour)}}
	endpoint, err := nodetls.NewEndpoint(nodes, f.clock, "local", "receiver", tls.Certificate{Certificate: [][]byte{server.Certificate}, PrivateKey: serverKey}, trust, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = endpoint.RestorePresentation(ctx, client.Certificate, "admin", "original-operation", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	f.clock.now = cutoff
	if _, err = endpoint.RestorePresentation(ctx, client.Certificate, "admin", "original-operation", strings.Repeat("a", 64)); err == nil {
		t.Fatal("expired issuer remained trusted during recovery")
	}
	nextNodes, err := f.s.Nodes(cfg, nextIssuer)
	if err != nil {
		t.Fatal(err)
	}
	// Principal expiry is fixed, so replacement certificates keep the original deadline.
	rotate := func(node string, key *ecdsa.PrivateKey) authorization.NodeRecord {
		t.Helper()
		snap, _ := f.db.Load(ctx)
		op, _ := f.s.NewOperation(ctx, f.token)
		in := authorization.NodeMutation{OperationID: op, Namespace: "local", Node: node, Kind: "ROTATE", ExpectedRevision: snap.State.Revision, Subjects: []string{"admin"}, RequestExpires: f.clock.now.Add(time.Minute).Unix(), Expires: client.Expires}
		in.CSR, err = nodetls.Request(key, in)
		if err != nil {
			t.Fatal(err)
		}
		r, e := nextNodes.Mutate(ctx, f.token, in)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	client = rotate("node", clientKey)
	server = rotate("receiver", serverKey)
	endpoint, err = nodetls.NewEndpoint(nextNodes, f.clock, "local", "receiver", tls.Certificate{Certificate: [][]byte{server.Certificate}, PrivateKey: serverKey}, trust[1:], time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = endpoint.RestorePresentation(ctx, client.Certificate, "admin", "original-operation", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err = f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = endpoint.RestorePresentation(ctx, client.Certificate, "admin", "original-operation", strings.Repeat("a", 64)); err == nil {
		t.Fatal("unavailable registry accepted")
	}
}

func TestNodeRebindPreservesSingleUseAndAncestorAllocation(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	ca, _ := nodetls.GenerateCA(f.clock.now, 2*time.Hour)
	issuer, _ := nodetls.NewIssuer(ca)
	nodes, err := f.s.Nodes(authorization.NodeConfig{MaxTTL: time.Hour, RequestTTL: time.Minute, IOTimeout: time.Second, MaxRecords: 16, MaxOperations: 128}, issuer)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	serverKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	original := f.enroll(t, nodes, "node", key, "ENROLL")
	f.enroll(t, nodes, "receiver", serverKey, "ENROLL")
	f.spec.CertificateSha256 = original.CertificateSHA256
	snap, _ := f.db.Load(ctx)
	root, err := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", snap.State.Revision))
	if err != nil {
		t.Fatal(err)
	}
	childSpec := proto.Clone(f.spec).(*wire.SignedGrantSpec)
	childSpec.Mode = "single"
	childSpec.Units = 1
	childSpec.DelegationDepth = 0
	childSpec.OperationBinding = "original-action"
	childSpec.SemanticSha256 = strings.Repeat("b", 64)
	snap, _ = f.db.Load(ctx)
	op, _ := f.s.NewOperation(ctx, f.token)
	child, err := f.g.Mutate(ctx, f.token, &wire.GrantMutation{OperationId: op, Kind: "DERIVE", GrantId: root.GrantId, ExpectedRevision: snap.State.Revision, ExpectedGrantRevision: root.Revision, Spec: childSpec})
	if err != nil {
		t.Fatal(err)
	}
	action := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
	p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Presenter: "node", Audience: "receiver", CertificateSHA256: original.CertificateSHA256, OperationID: childSpec.OperationBinding, SemanticSHA256: childSpec.SemanticSha256}
	permit, err := f.g.ReserveUse(ctx, child.Material, p, action, 1)
	if err != nil {
		t.Fatal(err)
	}
	nextKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rotated := f.enroll(t, nodes, "node", nextKey, "ROTATE")
	p.CertificateSHA256 = rotated.CertificateSHA256
	snap, _ = f.db.Load(ctx)
	op, _ = f.s.NewOperation(ctx, f.token)
	rebound, err := f.g.Mutate(ctx, f.token, &wire.GrantMutation{OperationId: op, Kind: "REBIND", GrantId: child.GrantId, ExpectedRevision: snap.State.Revision, ExpectedGrantRevision: child.Revision})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.g.ReserveUse(ctx, rebound.Material, p, action, 1)
	if err != nil || replay != permit {
		t.Fatalf("rebind changed allocation: %v", err)
	}
	if err = f.g.UpdateExecution(ctx, func(tx authorization.ExecutionTransaction) error { return tx.ValidateUse(permit, p, action) }); err != nil {
		t.Fatal(err)
	}
	p.OperationID = "different-action"
	if _, err = f.g.ReserveUse(ctx, rebound.Material, p, action, 1); err == nil {
		t.Fatal("single-use identity changed")
	}
	for _, id := range []string{root.GrantId, child.GrantId} {
		record, err := f.g.Get(ctx, f.token, id)
		if err != nil || record.Allocated != 1 {
			t.Fatalf("ancestor quota changed: %v %v", record, err)
		}
	}
	record, err := f.g.Get(ctx, f.token, child.GrantId)
	if err != nil || !proto.Equal(record.Spec, childSpec) || record.Parent != root.GrantId {
		t.Fatalf("scope/ancestor changed: %v", err)
	}
}
