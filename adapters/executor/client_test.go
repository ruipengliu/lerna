package executor

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

func deviceTLSFiles(t *testing.T, root string) (caPath, certPath, keyPath string) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Independent device test CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	caPath, certPath, keyPath = filepath.Join(root, "ca.pem"), filepath.Join(root, "server.pem"), filepath.Join(root, "server-key.pem")
	for path, body := range map[string][]byte{caPath: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), certPath: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyPath: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})} {
		if err = os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func frozenInvoke(f *deviceFixture, window api.ControlSnapshot) api.Command {
	in := execution.InvokeInput{OperationID: f.b.Intent.OperationID, TaskRef: f.b.Intent.TaskRef, GoalRevision: f.b.Intent.GoalRevision, ControlRevision: f.b.Intent.ControlRevision, CapabilityRef: f.b.Intent.CapabilityRef, BindingRef: f.b.Intent.BindingRef, IntentRef: f.b.IntentRef, IntentHash: f.b.ExecutionHash, UseRefs: f.b.UseRefs, Deadline: f.b.Intent.Deadline, ControlSnapshot: window, ReservationRef: f.b.ReservationRef}
	return commandFor(f.b.EndpointID, f.b.OriginalCommandID, "execution.invoke", f.b.Intent.OperationID, f.b.Intent.Deadline, in)
}
func waitApplied(t *testing.T, ctx context.Context, client *Client, operationID string) execution.OperationView {
	t.Helper()
	var view execution.OperationView
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		var err error
		view, err = client.Get(ctx, operationID)
		if err != nil {
			t.Fatal(err)
		}
		if view.NewAttemptsClosed && view.Operation.Effect == "applied" && view.ActuallyStopped {
			return view
		}
		if view.NewAttemptsClosed && view.Operation.Effect == "not_started" {
			t.Fatalf("original operation failed to enter: %+v", view)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("original operation did not converge: %+v", view)
	return view
}
func reserveAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
func startTLSHost(t *testing.T, f *deviceFixture) (*Client, context.Context) {
	return startTLSHostFor(t, f, 30*time.Second)
}
func startTLSHostFor(t *testing.T, f *deviceFixture, timeout time.Duration) (*Client, context.Context) {
	t.Helper()
	ca, cert, key := deviceTLSFiles(t, f.root)
	f.h.Config.TLSCertificateFile = cert
	f.h.Config.TLSKeyFile = key
	f.h.Config.GRPCAddr = reserveAddress(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	done := make(chan error, 1)
	go func() { done <- f.h.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(8 * time.Second):
			t.Error("device RPC/worker did not exit")
		}
	})
	client, err := Dial(ctx, RemoteConfig{DatabaseID: f.b.DeviceDatabaseID, Endpoint: "grpcs://" + f.h.Config.GRPCAddr, OwnerID: f.b.EndpointID, InstanceID: f.b.InstanceID, AuthorityID: f.b.AuthorityID, TenantID: f.b.Principal.TenantID, TLSCAFile: ca, PeerTokenFile: f.h.Config.PeerTokenFile, JournalRoot: filepath.Join(f.root, "outbound")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		var status ContentOutput
		err = client.query(ctx, "executor.content.status", f.b.IntentRef.ContentID, ContentID{f.b.IntentRef}, &status)
		if api.IsCode(err, "not_found") {
			return client, ctx
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("TLS device unavailable: %v", err)
	return nil, nil
}

type loseOriginalReply struct {
	inner     harness.Transport
	once      atomic.Bool
	journal   harness.Journal
	commandID string
}

func (t *loseOriginalReply) Call(ctx context.Context, kind string, raw json.RawMessage) (json.RawMessage, error) {
	if kind == "command" {
		var c api.Command
		if err := api.Decode(raw, &c); err != nil {
			return nil, err
		}
		if c.CommandID == t.commandID {
			entry, err := t.journal.Read(ctx, c.CommandID)
			if err != nil || !api.Equal(entry.Command, c) {
				return nil, api.E("forbidden", "command_not_durable_before_io")
			}
			reply, err := t.inner.Call(ctx, kind, raw)
			if err == nil && !t.once.Swap(true) {
				return nil, api.E("dependency_unavailable", "test_original_reply_lost_after_delivery")
			}
			return reply, err
		}
	}
	return t.inner.Call(ctx, kind, raw)
}
func TestRealTLSClientJournalsBeforeIOAndRecoversSamePhysicalAttempt(t *testing.T) {
	f := newDeviceFixture(t)
	client, ctx := startTLSHost(t, f)
	if err := client.Prepare(ctx, f.b, func(_ context.Context, p ContentPermission) ([]byte, error) {
		return f.raw[contentKey(p.ContentRef)], nil
	}); err != nil {
		t.Fatal(err)
	}
	window := f.control(t, "active", "running", 1)
	original := frozenInvoke(f, window)
	var saved ControlDelivery
	if _, err := f.h.Store.Read(ctx, f.h.Scope, Namespace+".controls", window.WindowID, 0, &saved); err != nil {
		t.Fatal(err)
	}
	fault := &loseOriginalReply{inner: client.SDK.Transport, journal: client.SDK.Journal, commandID: original.CommandID}
	client.SDK.Transport = fault
	if _, err := client.Dispatch(ctx, original, saved); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("lost original reply: %v", err)
	}
	if results, partial, err := client.SDK.Recover(ctx); err != nil || partial || len(results) != 1 || results[0].CommandID != original.CommandID {
		t.Fatalf("original journal recovery: %+v %t %v", results, partial, err)
	}
	view := waitApplied(t, ctx, client, f.b.Intent.OperationID)
	if len(view.Attempts.Items) != 1 {
		t.Fatalf("original physical attempt: %+v", view)
	}
	actual, err := os.ReadFile(filepath.Join(f.root, "files", "reports", "result.txt"))
	if err != nil || string(actual) != "independent cloud-admitted file bytes\n" {
		t.Fatalf("independent bytes: %q %v", actual, err)
	}
	usage, err := client.Usage(ctx, f.b.Intent.OperationID)
	if err != nil || !usage.UsageFinal || len(usage.ProofRefs) != 1 {
		t.Fatalf("device usage: %+v %v", usage, err)
	}
	bytes, permission, err := client.ReadBytes(ctx, usage.ProofRefs[0])
	if err != nil || api.Hash(bytes) != usage.ProofRefs[0].Hash || permission.ContentRef.OwnerID != f.b.EndpointID {
		t.Fatalf("exact original proof copy: %+v %v", permission, err)
	}
	report, err := client.LeaseUsage(ctx, f.b.Lease.LeaseID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Report.Usage.SpendingClosed || !report.Report.Usage.UsageFinal || report.Report.Usage.SourceRef.ObjectID != f.b.Lease.LeaseID || report.Report.Usage.SourceRef.OwnerID != f.b.EndpointID {
		t.Fatalf("original lease accounting: %+v", report)
	}
	deviceKey := f.h.Keys.Keys["device-es256"]
	deviceKey.Private = nil
	ring := &platform.Keyring{Keys: map[string]platform.RegisteredKey{"device-es256": deviceKey}}
	if err = VerifyLeaseReport(ring, report); err != nil {
		t.Fatal(err)
	}
	second, err := client.LeaseUsage(ctx, f.b.Lease.LeaseID)
	if err != nil || !api.Equal(report, second) {
		t.Fatalf("original lease fact changed: %v", err)
	}
	modified := report
	modified.Report.Usage = report.Report.Usage
	modified.Report.Usage.UsageFinal = false
	if err = VerifyLeaseReport(ring, modified); err == nil {
		t.Fatal("unsigned altered lease fact accepted")
	}
	physical, err := os.ReadDir(filepath.Join(f.root, "files", ".harness", "journals"))
	if err != nil || len(physical) != 1 {
		t.Fatalf("physical journals: %d %v", len(physical), err)
	}
	t.Logf("EXECUTOR_EVIDENCE %s", api.Raw(struct {
		TaskRef          api.ObjectRef     `json:"task_ref"`
		CommandID        string            `json:"command_id"`
		OperationID      string            `json:"operation_id"`
		AttemptID        string            `json:"attempt_id"`
		Usage            api.UsageSnapshot `json:"usage"`
		SourceGrantRefs  []api.ObjectRef   `json:"source_grant_refs"`
		OriginalTTL      string            `json:"original_ttl"`
		ControlBefore    string            `json:"control_before"`
		PhysicalJournals uint64            `json:"physical_journals"`
	}{f.b.Intent.TaskRef, original.CommandID, f.b.Intent.OperationID, view.Attempts.Items[0].AttemptID, usage, f.b.Lease.GrantRefs, original.ExpiresAt, window.StartBefore, uint64(len(physical))}))
}
