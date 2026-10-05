// Command lernad 是长期运行的单进程宿主：持续推进各事务域的待办工作。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/infra/clock"
)

func main() {
	data := flag.String("data", os.Getenv("LERNA_HOME"), "数据目录")
	user := flag.String("user", "u-local", "用户标识（M1 单用户）")
	flag.Parse()
	if *data == "" {
		fmt.Fprintln(os.Stderr, "lernad: --data is required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*data, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "lernad:", err)
		os.Exit(1)
	}
	h, err := host.Open(host.Config{Dir: *data, UserID: *user, Clock: clock.System{}, Instance: "lernad-" + ids.New()})
	if err != nil {
		fmt.Fprintln(os.Stderr, "lernad:", err)
		os.Exit(1)
	}
	defer func() { _ = h.Close() }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for ctx.Err() == nil {
		did, err := h.RunOnce(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "lernad:", err)
		}
		if !did {
			select {
			case <-ctx.Done():
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
}
