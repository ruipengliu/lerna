package grpc_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

// 原普通请求实际占用服务端 32 槽时，准确控制合同仍可送入原业务接纳边界。
func TestGRPCActualControlsUseReservedCapacityAndKeepAuthority(t *testing.T) {
	f := newUnaryFixture(t)
	dispatcher := f.processor.(wss.LocalProcessor).Dispatcher
	methods := []string{"task.cancel", "task.pause", "task.resume", "grant.revoke", "schedule.pause", "schedule.resume", "schedule.delete"}
	for _, method := range methods {
		dispatcher.Registry.MustRegister(runtime.Method{Contract: api.Contract[echoInput, echoOutput](method, "grpctest", "command", false, false), Participants: []string{"grpctest"}, Apply: func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command) (runtime.Outcome, error) {
			var in echoInput
			if err := api.Decode(c.Payload, &in); err != nil {
				return runtime.Outcome{}, err
			}
			if in.Message == "unauthorized" {
				return runtime.Outcome{}, api.E("forbidden", "control_not_authorized")
			}
			out := echoOutput{Message: in.Message, SubjectID: a.SubjectID}
			if err := tx.Create(ctx, "grpctest.messages", c.TargetID, "", out); err != nil {
				return runtime.Outcome{}, err
			}
			return runtime.Applied(out), nil
		}})
	}
	f.discovery.Methods = dispatcher.Registry.Contracts()
	f.discovery.MethodsDigest, _ = api.Digest(f.discovery.Methods)
	block := blockingQueries{next: f.processor, entered: make(chan struct{}, 32), release: make(chan struct{})}
	f.processor = block
	address := f.serve(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	g, err := harness.DialGRPC(ctx, address, f.token, f.discovery, f.clientTLS, false)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	original := commandFor(f)
	if _, err = g.Call(ctx, "command", api.Raw(original)); err != nil {
		t.Fatal(err)
	}
	query := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, QueryID: api.NewID("query"), Method: "testing.get", TargetID: original.TargetID, Payload: api.Raw(struct{}{})}
	var workers sync.WaitGroup
	results := make(chan error, 32)
	released := false
	defer func() {
		if !released {
			close(block.release)
		}
		workers.Wait()
	}()
	for range 32 {
		workers.Add(1)
		go func() { defer workers.Done(); _, e := g.Call(ctx, "query", api.Raw(query)); results <- e }()
	}
	for range 32 {
		select {
		case <-block.entered:
		case <-ctx.Done():
			t.Fatal("ordinary RPCs did not actually enter")
		}
	}
	if _, err = g.Call(ctx, "query", api.Raw(query)); !api.IsCode(err, "overloaded") {
		t.Fatalf("ordinary capacity not saturated: %v", err)
	}
	for _, method := range methods {
		for _, input := range []string{"authorized", "unauthorized"} {
			command := commandFor(f)
			command.Method = method
			command.Payload = api.Raw(echoInput{Message: input})
			body, err := g.Call(ctx, "command", api.Raw(command))
			var receipt api.Receipt
			if err != nil || api.Decode(body, &receipt) != nil {
				t.Fatalf("reserved %s rejected at transport: %v", method, err)
			}
			if input == "authorized" && receipt.Stage != "applied" {
				t.Fatalf("actual control did not apply %+v", receipt)
			}
			if input == "unauthorized" && (receipt.Stage != "rejected" || !api.IsCode(receipt.Error, "forbidden")) {
				t.Fatalf("reserved capacity bypassed control authority %+v", receipt)
			}
		}
	}
	close(block.release)
	released = true
	workers.Wait()
	for range 32 {
		if err := <-results; err != nil {
			t.Fatalf("held ordinary RPC did not actually exit: %v", err)
		}
	}
}
