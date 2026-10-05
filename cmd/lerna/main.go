// Command lerna 是命令行：在本进程内装配宿主（本地绑定，不实现网关），执行一条命令后推进待办工作。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/infra/clock"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "lerna:", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("lerna", flag.ContinueOnError)
	data := fs.String("data", defaultDataDir(), "数据目录")
	user := fs.String("user", "u-local", "用户标识（M1 单用户，只用合成数据）")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if err := os.MkdirAll(*data, 0o700); err != nil {
		return err
	}
	h, err := host.Open(host.Config{Dir: *data, UserID: *user, Clock: clock.System{}, Instance: "cli-" + ids.New()})
	if err != nil {
		return err
	}
	defer func() { _ = h.Close() }()
	cli := &interaction.CLI{
		Core:   h.Client("cli"),
		User:   *user,
		Issuer: "cli",
		Domain: host.DomainAdjudication,
		Out:    os.Stdout,
		Settle: func(ctx context.Context) error { return h.RunUntilIdle(ctx, 10000) },
	}
	return cli.Run(context.Background(), fs.Args())
}

func defaultDataDir() string {
	if d := os.Getenv("LERNA_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".lerna"
	}
	return filepath.Join(home, ".lerna")
}
