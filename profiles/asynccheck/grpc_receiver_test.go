package asynccheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"lerna/adapters/grpcbinding"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestGRPCReceiverProcess(t *testing.T) {
	root := os.Getenv("HARNESS_GRPC_RECEIVER")
	if root == "" {
		t.Skip("receiver subprocess only")
	}
	db, e := sqliteauth.Open(filepath.Join(root, "authority.db"))
	mustGRPC(t, e)
	defer db.Close()
	clock := &clock{now: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	a, e := authorization.New(db, clock, authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second})
	mustGRPC(t, e)
	ep := endpoint(t, root, "local-host", a, clock)
	c, e := grpcbinding.Dial(os.Getenv("HARNESS_GRPC_ADDRESS"), "execution-local", ep, grpcConfig())
	mustGRPC(t, e)
	defer c.Close()
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_, e = c.Negotiate(ctx, "operator")
	mustGRPC(t, e)
	inbox, e := grpcbinding.OpenJournal(filepath.Join(root, "receiver.db"), 64)
	mustGRPC(t, e)
	defer inbox.Close()
	sub, e := c.Subscribe(ctx, os.Getenv("HARNESS_GRPC_OPERATION"), inbox)
	mustGRPC(t, e)
	defer sub.Close()
	u, fresh, e := receivePhase(sub, "FINISHED")
	mustGRPC(t, e)
	raw, e := json.Marshal(struct {
		Fresh bool
		Seq   uint64
		PID   int
	}{fresh, u.Seq, os.Getpid()})
	mustGRPC(t, e)
	fmt.Println(string(raw))
}
func TestGRPCReceiverRestartKeepsReceiptIdentity(t *testing.T) {
	f := prepareNetwork(t)
	f.h.close()
	p := launchServer(t, f.server)
	address := p.address(t)
	c, api := connect(t, f, address)
	_, e := api.Invoke(context.Background(), f.request, f.material)
	mustGRPC(t, e)
	p.control(t, "start", f.request.OperationID)
	p.control(t, "complete", f.request.OperationID)
	mustGRPC(t, c.Close())
	mustGRPC(t, f.clientDB.Close())
	var last uint64
	var firstPID int
	for i := 0; i < 2; i++ {
		ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGRPCReceiverProcess$")
		cmd.Env = append(os.Environ(), "HARNESS_GRPC_RECEIVER="+f.client, "HARNESS_GRPC_ADDRESS="+address, "HARNESS_GRPC_OPERATION="+f.request.OperationID)
		raw, e := cmd.CombinedOutput()
		stop()
		if e != nil {
			t.Fatalf("receiver: %v %s", e, raw)
		}
		var report struct {
			Fresh bool
			Seq   uint64
			PID   int
		}
		mustGRPC(t, json.NewDecoder(bytes.NewReader(raw)).Decode(&report))
		if report.Fresh != (i == 0) || report.PID == os.Getpid() || (i == 1 && (report.Seq != last || report.PID == firstPID)) {
			t.Fatal(report)
		}
		last = report.Seq
		firstPID = report.PID
		t.Logf("receiver PID=%d fresh=%v revision=%d", report.PID, report.Fresh, report.Seq)
	}
}
