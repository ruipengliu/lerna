package asynccheck

import (
	"bufio"
	"context"
	"fmt"
	"google.golang.org/protobuf/proto"
	"io"
	wsbinding "lerna/adapters/transport/ws"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/executionwire"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWSReliableProcess(t *testing.T) {
	dir := os.Getenv("HARNESS_RELIABLE_DIR")
	if dir == "" {
		t.Skip("subprocess only")
	}
	h, host, d := openWS(t, dir)
	defer h.close()
	host = reliableHost(t, h, host)
	raw, e := os.ReadFile(filepath.Join(dir, "reliable-request.pb"))
	mustGRPC(t, e)
	request := new(wire.CapabilityRequest)
	mustGRPC(t, proto.Unmarshal(raw, request))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if os.Getenv("HARNESS_RELIABLE_ADDRESS") != "" {
		j, e := wsbinding.NewJournal(h.auth, "local/operator/"+d.Peer, reliableConfig())
		mustGRPC(t, e)
		if os.Getenv("HARNESS_RELIABLE_RESUME") == "" {
			_, e = j.Prepare(ctx, request)
			mustGRPC(t, e)
		}
		p, e := host.Dial(ctx, "wss://"+os.Getenv("HARNESS_RELIABLE_ADDRESS")+"/harness", d.Peer)
		mustGRPC(t, e)
		defer p.Close()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			result, e := p.Result(ctx, request.MessageId)
			if e == nil {
				if result.GetReceipt().GetOperationId() != request.GetInvoke().GetInvocation().GetOperationId() {
					t.Fatal(result)
				}
				fmt.Println("RESULT")
				return
			}
			select {
			case <-tick.C:
			case <-p.Done():
				fmt.Println("DISCONNECTED")
				return
			case <-ctx.Done():
				t.Fatal("no recovered reply", e)
			}
		}
	}
	mode := os.Getenv("HARNESS_RELIABLE_CRASH")
	if mode != "" {
		resolve := host.Resolve
		host.Resolve = func(ctx context.Context, p authorization.GrantPresentation) (*wsbinding.Binding, error) {
			b, e := resolve(ctx, p)
			if e != nil {
				return nil, e
			}
			retain := b.Retain
			b.Retain = func(ctx context.Context, p authorization.GrantPresentation, r *wire.CapabilityRequest) error {
				if e := retain(ctx, p, r); e != nil {
					return e
				}
				pending, e := b.Journal.Unconsumed(ctx)
				if e != nil {
					return e
				}
				if len(pending) > 0 {
					if mode != "inbox" {
						_, e = h.exec.Invoke(ctx, executionwire.Decode(request.GetInvoke().GetInvocation()), request.GetInvoke().GetGrantMaterial())
						mustGRPC(t, e)
					}
					if mode == "effect" {
						_, e = h.exec.Run(ctx, request.GetInvoke().GetInvocation().GetOperationId())
						mustGRPC(t, e)
						mustGRPC(t, h.target.Complete(ctx, request.GetInvoke().GetInvocation().GetOperationId()))
					}
					fmt.Println("CRASH " + mode)
					os.Exit(77)
				}
				return nil
			}
			return b, nil
		}
	}
	s, e := wsbinding.NewServer(host)
	mustGRPC(t, e)
	defer s.Close()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go s.Serve(l)
	fmt.Println("ADDRESS " + l.Addr().String())
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		switch scan.Text() {
		case "finish":
			op := request.GetInvoke().GetInvocation().GetOperationId()
			record, e := h.exec.GetInvocation(ctx, op)
			mustGRPC(t, e)
			if !record.Started {
				_, e = h.exec.Run(ctx, op)
				mustGRPC(t, e)
				mustGRPC(t, h.target.Complete(ctx, op))
			}
			h.clock.advance(time.Second)
			_, e = h.exec.Reconcile(ctx, op)
			mustGRPC(t, e)
			truth, e := h.target.Snapshot(ctx)
			mustGRPC(t, e)
			if truth.Changes != 1 {
				t.Fatal(truth)
			}
			fmt.Println("EFFECT 1")
		case "quit":
			return
		default:
			t.Fatal("unknown control")
		}
	}
}

type reliableChild struct {
	cmd   *exec.Cmd
	input io.WriteCloser
	scan  *bufio.Scanner
}

func startReliableChild(t *testing.T, dir, address, crash string, resume bool) *reliableChild {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWSReliableProcess$", "-test.v")
	cmd.Env = append(os.Environ(), "HARNESS_RELIABLE_DIR="+dir, "HARNESS_RELIABLE_ADDRESS="+address, "HARNESS_RELIABLE_CRASH="+crash)
	if resume {
		cmd.Env = append(cmd.Env, "HARNESS_RELIABLE_RESUME=1")
	}
	in, e := cmd.StdinPipe()
	mustGRPC(t, e)
	out, e := cmd.StdoutPipe()
	mustGRPC(t, e)
	cmd.Stderr = os.Stderr
	mustGRPC(t, cmd.Start())
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	t.Logf("role=%s PID=%d", filepath.Base(dir), cmd.Process.Pid)
	return &reliableChild{cmd, in, bufio.NewScanner(out)}
}
func (c *reliableChild) line(t *testing.T, prefix string) string {
	for c.scan.Scan() {
		line := c.scan.Text()
		t.Log(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("process ended before %s", prefix)
	return ""
}
func TestWSReliableProcessRecovery(t *testing.T) {
	for _, mode := range []string{"inbox", "handoff", "effect", "reverse-inbox", "reverse-handoff", "reverse-effect"} {
		t.Run(mode, func(t *testing.T) {
			edge, cloud := prepareWS(t)
			if strings.HasPrefix(mode, "reverse-") {
				edge, cloud = cloud, edge
			}
			crashMode := strings.TrimPrefix(mode, "reverse-")
			h, _, _ := openWS(t, cloud)
			req, material, e := h.request(context.Background())
			mustGRPC(t, e)
			h.close()
			request := &wire.CapabilityRequest{MessageId: "reconnect-intent", Namespace: "local", Body: &wire.CapabilityRequest_Invoke{Invoke: &wire.InvokeCapability{Invocation: executionwire.Encode(req), GrantMaterial: material}}}
			raw, e := proto.Marshal(request)
			mustGRPC(t, e)
			for _, dir := range []string{edge, cloud} {
				mustGRPC(t, os.WriteFile(filepath.Join(dir, "reliable-request.pb"), raw, 0600))
			}
			server := startReliableChild(t, cloud, "", crashMode, false)
			address := server.line(t, "ADDRESS ")
			client := startReliableChild(t, edge, address, "", false)
			server.line(t, "CRASH ")
			if e = server.cmd.Wait(); e == nil {
				t.Fatal("crash did not exit")
			}
			client.line(t, "DISCONNECTED")
			mustGRPC(t, client.cmd.Wait())
			server = startReliableChild(t, cloud, "", "", true)
			address = server.line(t, "ADDRESS ")
			client = startReliableChild(t, edge, address, "", true)
			client.line(t, "RESULT")
			mustGRPC(t, client.cmd.Wait())
			fmt.Fprintln(server.input, "finish")
			server.line(t, "EFFECT 1")
			fmt.Fprintln(server.input, "quit")
			mustGRPC(t, server.cmd.Wait())
		})
	}
}
