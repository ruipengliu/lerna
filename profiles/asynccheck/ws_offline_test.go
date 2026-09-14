package asynccheck

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lerna/adapters/josegrant"
	"lerna/adapters/wsbinding"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
)

func offlineConfig() authorization.OfflineConfig {
	return authorization.OfflineConfig{Window: time.Minute, Skew: time.Second, IOTimeout: 5 * time.Second, Retention: time.Minute, Records: 16, PageSize: 1, Bytes: 65536}
}
func TestWSOfflineExecution(t *testing.T) {
	for _, allow := range []bool{true, false} {
		t.Run(map[bool]string{true: "finite-offline", false: "online-required"}[allow], func(t *testing.T) {
			edge, cloud := prepareWS(t)
			a, ah, _ := openWS(t, edge)
			defer a.close()
			b, bh, _ := openWS(t, cloud)
			defer b.close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			req, localMaterial, e := b.request(ctx)
			mustGRPC(t, e)
			issuer := *a
			issuer.binding = b.binding
			remoteMaterial, e := issuer.issue(ctx, req)
			mustGRPC(t, e)
			presentation := b.binding.Presentation(req)
			view, e := a.grants.OfflineView("counter-view", presentation, offlineConfig())
			mustGRPC(t, e)
			action := &wire.AuthorizationAction{Resource: "root", Action: "resource.change", Purpose: "task", Location: "local"}
			mustGRPC(t, view.Allocate(ctx, remoteMaterial, presentation, action, allow))
			resolve := ah.Resolve
			ah.Required = append(ah.Required, "authorization.sync.v1")
			bh.Required = append(bh.Required, "authorization.sync.v1")
			ah.Resolve = func(ctx context.Context, p authorization.GrantPresentation) (*wsbinding.Binding, error) {
				binding, e := resolve(ctx, p)
				if e != nil {
					return nil, e
				}
				copy := *binding
				copy.Authorization = view
				return &copy, nil
			}
			raw, e := os.ReadFile(filepath.Join(edge, "signer.pem"))
			mustGRPC(t, e)
			block, _ := pem.Decode(raw)
			key, e := x509.ParsePKCS8PrivateKey(block.Bytes)
			mustGRPC(t, e)
			verifier, e := josegrant.NewVerifier(map[string]josegrant.VerificationKey{"key1": {Public: &key.(*ecdsa.PrivateKey).PublicKey, Until: b.now().Add(time.Hour)}}, b.clock)
			mustGRPC(t, e)
			replica, e := authorization.NewOfflineReplica(b.auth, "counter-view", presentation, offlineConfig(), verifier, authorization.SystemOfflineClock{}, offlineRetention(b))
			mustGRPC(t, e)
			b.exec, e = b.exec.BindOffline(replica, nil)
			mustGRPC(t, e)
			_, e = b.exec.Invoke(ctx, req, localMaterial)
			mustGRPC(t, e)
			if _, e = b.exec.Run(ctx, req.OperationID); e == nil {
				t.Fatal("remote authorization absent but effect started")
			}
			server, e := wsbinding.NewServer(ah)
			mustGRPC(t, e)
			defer server.Close()
			listener, e := net.Listen("tcp", "127.0.0.1:0")
			mustGRPC(t, e)
			go server.Serve(listener)
			peer, e := bh.Dial(ctx, "wss://"+listener.Addr().String()+"/harness", "edge")
			mustGRPC(t, e)

			// A fresh process can read the same durable replica, but cannot reuse its anchor.
			public, e := x509.MarshalPKIXPublicKey(&key.(*ecdsa.PrivateKey).PublicKey)
			mustGRPC(t, e)
			mustGRPC(t, os.WriteFile(filepath.Join(cloud, "offline-public.der"), public, 0600))
			saved, e := json.Marshal(req)
			mustGRPC(t, e)
			mustGRPC(t, os.WriteFile(filepath.Join(cloud, "offline-request.json"), saved, 0600))
			crash := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWSOfflineRestartProcess$", "-test.v")
			crash.Env = append(os.Environ(), "HARNESS_OFFLINE_PROCESS="+cloud, "HARNESS_OFFLINE_MODE=receive", "HARNESS_OFFLINE_ADDRESS=wss://"+listener.Addr().String()+"/harness")
			crashOutput, crashError := crash.CombinedOutput()
			t.Log(string(crashOutput))
			exit, ok := crashError.(*exec.ExitError)
			if !ok || exit.ExitCode() != 77 {
				t.Fatalf("expected committed inbox crash 77: %v", crashError)
			}
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWSOfflineRestartProcess$", "-test.v")
			child.Env = append(os.Environ(), "HARNESS_OFFLINE_PROCESS="+cloud)
			output, e := child.CombinedOutput()
			t.Log(string(output))
			mustGRPC(t, e)
			replica, e = authorization.NewOfflineReplica(b.auth, "counter-view", presentation, offlineConfig(), verifier, authorization.SystemOfflineClock{}, offlineRetention(b))
			mustGRPC(t, e)
			b.exec, e = b.exec.BindOffline(replica, nil)
			mustGRPC(t, e)
			mustGRPC(t, replica.Synchronize(ctx, peer.ReadAuthorization))
			peer.Close()
			_, e = b.exec.Run(ctx, req.OperationID)
			if !allow {
				if e == nil {
					t.Fatal("online-required operation started disconnected")
				}
				truth, e := b.target.Snapshot(ctx)
				mustGRPC(t, e)
				if truth.Changes != 0 {
					t.Fatal(truth)
				}
				online, e := bh.Dial(ctx, "wss://"+listener.Addr().String()+"/harness", "edge")
				mustGRPC(t, e)
				defer online.Close()
				b.exec, e = b.exec.BindOffline(replica, online.ReadAuthorization)
				mustGRPC(t, e)
				_, e = b.exec.Run(ctx, req.OperationID)
				mustGRPC(t, e)

			}
			if allow {
				mustGRPC(t, e)
			}
			mustGRPC(t, b.target.Complete(ctx, req.OperationID))
			b.clock.advance(time.Second)
			_, e = b.exec.Reconcile(ctx, req.OperationID)
			mustGRPC(t, e)
			truth, e := b.target.Snapshot(ctx)
			mustGRPC(t, e)
			if truth.Changes != 1 {
				t.Fatal(truth)
			}
			t.Logf("offline original operation %s: independent effect changes=%d", req.OperationID, truth.Changes)
		})
	}
}

func TestWSOfflineRestartProcess(t *testing.T) {
	dir := os.Getenv("HARNESS_OFFLINE_PROCESS")
	if dir == "" {
		t.Skip("subprocess only")
	}
	h, host, _ := openWS(t, dir)
	defer h.close()
	ctx := context.Background()
	raw, e := os.ReadFile(filepath.Join(dir, "offline-request.json"))
	mustGRPC(t, e)
	var req execution.Request
	mustGRPC(t, json.Unmarshal(raw, &req))
	raw, e = os.ReadFile(filepath.Join(dir, "offline-public.der"))
	mustGRPC(t, e)
	public, e := x509.ParsePKIXPublicKey(raw)
	mustGRPC(t, e)
	verifier, e := josegrant.NewVerifier(map[string]josegrant.VerificationKey{"key1": {Public: public.(*ecdsa.PublicKey), Until: h.now().Add(time.Hour)}}, h.clock)
	mustGRPC(t, e)
	replica, e := authorization.NewOfflineReplica(h.auth, "counter-view", h.binding.Presentation(req), offlineConfig(), verifier, authorization.SystemOfflineClock{}, offlineRetention(h))
	mustGRPC(t, e)
	if os.Getenv("HARNESS_OFFLINE_MODE") == "receive" {
		host.Required = append(host.Required, "authorization.sync.v1")
		peer, e := host.Dial(ctx, os.Getenv("HARNESS_OFFLINE_ADDRESS"), "edge")
		mustGRPC(t, e)
		q, e := replica.Begin(ctx)
		mustGRPC(t, e)
		page, e := peer.ReadAuthorization(ctx, q)
		mustGRPC(t, e)
		mustGRPC(t, replica.Receive(ctx, page))
		progress, e := replica.Progress(ctx)
		mustGRPC(t, e)
		if progress.Received == 0 || progress.Applied != 0 {
			t.Fatal(progress)
		}
		t.Logf("PID=%d RECEIVED=%d APPLIED=0 exit=77", os.Getpid(), progress.Received)
		os.Exit(77)
	}
	mustGRPC(t, replica.Apply(ctx))
	progress, e := replica.Progress(ctx)
	mustGRPC(t, e)
	if progress.Applied == 0 || progress.Pending {
		t.Fatal(progress)
	}
	h.exec, e = h.exec.BindOffline(replica, nil)
	mustGRPC(t, e)
	_, e = h.exec.Run(ctx, req.OperationID)
	if e == nil {
		t.Fatal("restarted process executed remote-authorized work offline")
	}
	truth, e := h.target.Snapshot(ctx)
	mustGRPC(t, e)
	if truth.Changes != 0 {
		t.Fatal(truth)
	}
	t.Logf("PID=%d offline restart: denied new start, independent changes=0", os.Getpid())
}

func offlineRetention(h *harness) func(context.Context) error {
	return func(ctx context.Context) error {
		v, e := h.auth.ViewActions(ctx, h.token, []*wire.AuthorizationAction{{Resource: "root", Action: "content.store", Purpose: "task", Location: "local"}, {Resource: "root", Action: "content.retain", Purpose: "task", Location: "local"}})
		if e != nil {
			return e
		}
		if !v.Allowed[0] || !v.Allowed[1] {
			return &authorization.Error{Code: authorization.Denied}
		}
		return nil
	}
}
