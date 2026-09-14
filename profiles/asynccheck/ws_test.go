package asynccheck

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"lerna/adapters/catalogauth"
	"lerna/adapters/contentpolicy"
	"lerna/adapters/nodetls"
	"lerna/adapters/sqlitecatalog"
	"lerna/adapters/wsbinding"
	"lerna/authorization"
	"lerna/catalog"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/schema"
	"lerna/sdk"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func wsConfig() wsbinding.Config {
	return wsbinding.Config{MaxConnections: 4, Window: 4, MaxMessage: 65536, Chunk: 1024, Timeout: 2 * time.Second, Lifetime: time.Minute, Poll: 50 * time.Millisecond}
}

type wsDisk struct {
	Binding                     execution.Binding
	Node, Peer, RemoteOperation string
	Request                     execution.Request
	Material                    string
}

func wsScope(h *harness, remote bool) *wire.AuthorizationScope {
	locations := []string{"local"}
	if remote {
		locations = append(locations, "edge", "cloud")
	}
	return &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read", "task.execute", "task.reconcile", "capability.invoke", "capability.read", "capability.reconcile", "resource.change", "content.store", "content.process", "content.retain", "content.discover", "content.disclose", "content.delete", "catalog.list", "catalog.search", "catalog.describe"}, Purposes: []string{"task"}, Locations: locations, ExpiresUnix: h.now().Add(time.Hour).Unix()}
}
func wsPolicy(t *testing.T, h *harness, remote bool, grant bool) {
	ctx := context.Background()
	op, e := h.operation(ctx)
	mustGRPC(t, e)
	snap, e := h.db.Load(ctx)
	mustGRPC(t, e)
	cmd := &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "ws", Scope: wsScope(h, remote)}}}}, ExpectedRevision: snap.State.Revision}
	if grant {
		cmd.Change = &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "ws", Subject: "operator", Scope: wsScope(h, remote), Mode: "continuous"}}
	}
	_, e = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: cmd})
	mustGRPC(t, e)
}
func prepareWS(t *testing.T) (string, string) {
	ctx := context.Background()
	root := t.TempDir()
	dirs := []string{filepath.Join(root, "edge"), filepath.Join(root, "cloud")}
	hs := make([]*harness, 2)
	for i, dir := range dirs {
		mustGRPC(t, os.Mkdir(dir, 0700))
		h, e := open(ctx, dir, "")
		mustGRPC(t, e)
		hs[i] = h
		defer h.close()
		wsPolicy(t, h, true, false)
		wsPolicy(t, h, true, true)
	}
	ca, e := nodetls.GenerateCA(hs[0].now(), 2*time.Hour)
	mustGRPC(t, e)
	issuer, e := nodetls.NewIssuer(ca)
	mustGRPC(t, e)
	nodes, e := hs[0].auth.Nodes(nodeConfig(), issuer)
	mustGRPC(t, e)
	certs := make([]tls.Certificate, 2)
	for i, name := range []string{"edge", "cloud"} {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		mustGRPC(t, e)
		op, e := hs[0].operation(ctx)
		mustGRPC(t, e)
		snap, e := hs[0].db.Load(ctx)
		mustGRPC(t, e)
		in := authorization.NodeMutation{OperationID: op, Namespace: "local", Node: name, Kind: "ENROLL", ExpectedRevision: snap.State.Revision, Subjects: []string{"operator"}, RequestExpires: hs[0].now().Add(time.Minute).Unix(), Expires: hs[0].now().Add(time.Hour).Unix()}
		in.CSR, e = nodetls.Request(key, in)
		mustGRPC(t, e)
		r, e := nodes.Mutate(ctx, hs[0].token, in)
		mustGRPC(t, e)
		certs[i] = tls.Certificate{Certificate: [][]byte{r.Certificate}, PrivateKey: key}
		saveCertificate(t, dirs[i], certs[i])
		mustGRPC(t, os.WriteFile(filepath.Join(dirs[i], "ca.der"), ca.Certificate[0], 0600))
	}
	public, e := hs[0].db.Load(ctx)
	mustGRPC(t, e)
	dest, e := hs[1].db.Load(ctx)
	mustGRPC(t, e)
	dest.State.Nodes = &authorization.NodeJournal{Config: nodeConfig(), Records: public.State.Nodes.Records, Operations: map[string]authorization.NodeOperation{}}
	mustGRPC(t, hs[1].db.Commit(ctx, dest.Version, dest.State))
	disks := make([]wsDisk, 2)
	for i, h := range hs {
		peer := 1 - i
		names := []string{"edge", "cloud"}
		h.binding.Audience = names[i]
		h.binding.Presenter = names[peer]
		h.binding.CertificateSHA256 = authorization.CertificateDigest(certs[peer].Certificate[0])
		h.exec, e = execution.New(h.grants, h.work, h.access, h.target, h.binding, h.cap, config(), h.operation)
		mustGRPC(t, e)
		r, m, e := h.request(ctx)
		mustGRPC(t, e)
		_, e = h.exec.Invoke(ctx, r, m)
		mustGRPC(t, e)
		disks[i] = wsDisk{Binding: h.binding, Node: names[i], Peer: names[peer], Request: r, Material: m}
	}
	for i, d := range disks {
		d.RemoteOperation = disks[1-i].Request.OperationID
		privateJSON(t, filepath.Join(dirs[i], "ws.json"), d)
	}
	return dirs[0], dirs[1]
}
func openWS(t *testing.T, dir string) (*harness, wsbinding.Host, wsDisk) {
	raw, e := os.ReadFile(filepath.Join(dir, "ws.json"))
	mustGRPC(t, e)
	var d wsDisk
	mustGRPC(t, json.Unmarshal(raw, &d))
	h, e := open(context.Background(), dir, d.Binding.Token)
	mustGRPC(t, e)
	h.binding = d.Binding
	h.exec, e = execution.New(h.grants, h.work, h.access, h.target, h.binding, h.cap, config(), h.operation)
	mustGRPC(t, e)
	st, e := sqlitecatalog.Open(filepath.Join(dir, "catalog.db"), func(entry catalog.Entry) error {
		if entry.Capability.Digest() != h.cap.Digest() {
			return fmt.Errorf("unsupported implementation")
		}
		return nil
	})
	mustGRPC(t, e)
	t.Cleanup(func() { st.Close() })
	entry := catalog.Entry{Ref: catalog.Ref{Namespace: "local", Name: h.cap.Name, Version: h.cap.Version, Implementation: h.cap.Implementation, ImplementationVersion: h.cap.ImplementationVersion, Digest: h.cap.Digest()}, Source: catalog.Source{Kind: "catalog", Key: "public", Revision: 1}, Title: "Counter addition", Category: "reference", Purpose: "task", Location: "local", Resource: "root", ResourceType: "counter", Preconditions: "Registered counter", Effects: "Adds delta once", Unsupported: "Arbitrary counters", Guarantees: "Operation identity retained", Available: true, Capability: h.cap}
	hidden := entry
	hidden.Ref.Namespace = "hidden"
	hidden.Source.Key = "hidden"
	snap, e := st.Snapshot(context.Background(), "", 4)
	mustGRPC(t, e)
	if snap.Revision == 0 {
		_, e = st.Replace(context.Background(), 0, []catalog.Entry{entry, hidden})
		mustGRPC(t, e)
	}
	pol, e := contentpolicy.New([]contentpolicy.Rule{{Kind: "query", Key: "remote-query", Revision: 1, Actions: []string{"process"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(time.Hour).Unix()}, {Kind: "catalog", Key: "public", Revision: 1, Actions: []string{"process", "discover", "disclose"}, Purposes: []string{"task"}, Locations: []string{"local", d.Peer}, RetainUntil: h.now().Add(time.Hour).Unix()}})
	mustGRPC(t, e)
	cat, e := catalog.New(st, catalogauth.Adapter{Authority: h.auth, Policy: pol, Clock: h.clock, QuerySource: catalog.Source{Kind: "query", Key: "remote-query", Revision: 1}}, catalog.Config{MaxScan: 64, MaxPage: 8, MaxCandidates: 8, Timeout: time.Second}, catalog.QueryContext{Namespace: "local", Subject: "operator", Token: h.token, Purpose: "task", Location: "local"})
	mustGRPC(t, e)
	p := authorization.GrantPresentation{Namespace: "local", Subject: "operator", Audience: d.Node, Presenter: d.Peer, CertificateSHA256: h.binding.CertificateSHA256}
	b := &wsbinding.Binding{Peer: p, Catalog: cat, Execution: h.exec, Disclose: func(ctx context.Context, p authorization.GrantPresentation, r *wire.CapabilityRequest) error {
		// This reference host exposes only synthetic public catalog/counter state.
		// local denotes execution location; the destination is separately checked.
		v, e := h.auth.ViewActions(ctx, h.token, []*wire.AuthorizationAction{{Resource: "root", Action: "content.disclose", Purpose: "task", Location: d.Peer}})
		if e != nil {
			return e
		}
		if v.Identity.Subject != p.Subject || !v.Allowed[0] {
			return &authorization.Error{Code: authorization.Denied}
		}
		return pol.Check(ctx, &wire.ContentSource{Kind: "catalog", Key: "public", Revision: 1}, "disclose", "task", d.Peer, h.now().Unix())
	}}
	registry, e := schema.New([]schema.Resource{h.cap.Input})
	mustGRPC(t, e)
	host := wsbinding.Host{Endpoint: endpoint(t, dir, d.Node, h.auth, h.clock), Config: wsConfig(), Subject: "operator", Required: []string{"catalog.read.v1", "invocation.read.v1", "progress.v1", "chunks.v1"}, Schema: registry, Resolve: func(ctx context.Context, peer authorization.GrantPresentation) (*wsbinding.Binding, error) {
		if peer != p {
			return nil, &authorization.Error{Code: authorization.Denied}
		}
		return b, nil
	}}
	return h, host, d
}
func wsQueries(ctx context.Context, p *wsbinding.Peer, op string) error {
	api := sdk.NewCapabilityClient(p, "local")
	q := catalog.Query{Purpose: "task", Location: "local", Limit: 4, Budget: 8}
	page, e := api.List(ctx, q)
	if e != nil {
		return e
	}
	if len(page.Items) != 1 || page.Items[0].Ref.Namespace != "local" {
		return fmt.Errorf("catalog disclosure %+v", page)
	}
	q.Text = "counter"
	found, e := api.Search(ctx, q)
	if e != nil {
		return e
	}
	if len(found.Items) != 1 || found.Items[0].Ref != page.Items[0].Ref {
		return fmt.Errorf("search mismatch")
	}
	entry, e := api.Describe(ctx, page.Items[0].Ref)
	if e != nil {
		return e
	}
	if entry.Ref != page.Items[0].Ref {
		return fmt.Errorf("descriptor mismatch")
	}
	v, e := api.GetInvocation(ctx, op)
	if e != nil {
		return e
	}
	if v.Request.OperationID != op {
		return fmt.Errorf("operation identity changed")
	}
	return nil
}
func TestWSProcess(t *testing.T) {
	dir := os.Getenv("HARNESS_WS_PROCESS")
	if dir == "" {
		t.Skip("subprocess only")
	}
	h, host, d := openWS(t, dir)
	defer h.close()
	s, e := wsbinding.NewServer(host)
	mustGRPC(t, e)
	defer s.Close()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go s.Serve(l)
	fmt.Println(l.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, e := s.Accept(ctx)
	mustGRPC(t, e)
	// Both sides issue real queries concurrently without parent-triggered reads.
	mustGRPC(t, wsQueries(ctx, p, d.RemoteOperation))
	fmt.Println("reverse queries passed")
	sub, e := p.Subscribe(ctx, d.RemoteOperation)
	mustGRPC(t, e)
	defer sub.Close()
	v, e := sub.Receive(ctx)
	mustGRPC(t, e)
	if v.GetSnapshot().Phase != "NOT_STARTED" {
		t.Fatal(v)
	}
	fmt.Println("reverse subscription passed")
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		switch scan.Text() {
		case "complete":
			_, e = h.exec.Run(ctx, d.Request.OperationID)
			mustGRPC(t, e)
			mustGRPC(t, h.target.Complete(ctx, d.Request.OperationID))
			h.clock.advance(time.Second)
			_, e = h.exec.Reconcile(ctx, d.Request.OperationID)
			mustGRPC(t, e)
		case "revoke":
			wsPolicy(t, h, false, false)
		case "quit":
			return
		}
		fmt.Println("controlled")
	}
}
func TestWSDuplexProcesses(t *testing.T) {
	edge, cloud := prepareWS(t)
	h, host, d := openWS(t, edge)
	defer h.close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWSProcess$", "-test.v")
	cmd.Env = append(os.Environ(), "HARNESS_WS_PROCESS="+cloud)
	in, e := cmd.StdinPipe()
	mustGRPC(t, e)
	out, e := cmd.StdoutPipe()
	mustGRPC(t, e)
	cmd.Stderr = os.Stderr
	mustGRPC(t, cmd.Start())
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	scan := bufio.NewScanner(out)
	if !scan.Scan() {
		t.Fatal("no process output")
	}
	if scan.Text() == "=== RUN   TestWSProcess" {
		scan.Scan()
	}
	address := scan.Text()
	t.Logf("edge PID=%d cloud PID=%d endpoint=%s", os.Getpid(), cmd.Process.Pid, address)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	p, e := host.Dial(ctx, "wss://"+address+"/harness", "cloud")
	mustGRPC(t, e)
	defer p.Close()
	mustGRPC(t, wsQueries(ctx, p, d.RemoteOperation))
	for _, want := range []string{"reverse queries passed", "reverse subscription passed"} {
		if !scan.Scan() || scan.Text() != want {
			t.Fatalf("remote: %s", scan.Text())
		}
	}
	sub, e := p.Subscribe(ctx, d.RemoteOperation)
	mustGRPC(t, e)
	defer sub.Close()
	v, e := sub.Receive(ctx)
	mustGRPC(t, e)
	if v.GetSnapshot().Phase != "NOT_STARTED" {
		t.Fatal(v)
	}
	fmt.Fprintln(in, "complete")
	if !scan.Scan() || scan.Text() != "controlled" {
		t.Fatal(scan.Text())
	}
	for i := 0; i < 16; i++ {
		v, e = sub.Receive(ctx)
		mustGRPC(t, e)
		if v.GetSnapshot().Phase == "FINISHED" {
			break
		}
	}
	if v.GetSnapshot().Effect != "CONFIRMED" {
		t.Fatal(v)
	}
	_, e = sdk.NewCapabilityClient(p, "local").Invoke(ctx, d.Request, d.Material)
	if !authorization.Is(e, authorization.Unsupported) {
		t.Fatalf("mutation: %v", e)
	}
	fmt.Fprintln(in, "revoke")
	if !scan.Scan() || scan.Text() != "controlled" {
		t.Fatal(scan.Text())
	}
	_, e = sdk.NewCapabilityClient(p, "local").GetInvocation(ctx, d.RemoteOperation)
	if !authorization.Is(e, authorization.Denied) {
		t.Fatalf("revoked: %v", e)
	}
	_, e = sub.Receive(ctx)
	if !authorization.Is(e, authorization.Denied) {
		t.Fatalf("revoked subscription: %v", e)
	}
	fmt.Fprintln(in, "quit")
	for scan.Scan() {
		t.Log(scan.Text())
	}
	mustGRPC(t, cmd.Wait())
	truth, e := h.target.Snapshot(ctx)
	mustGRPC(t, e)
	if truth.Changes != 0 {
		t.Fatal("rejected mutation changed local target")
	}
}
