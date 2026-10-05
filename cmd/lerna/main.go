// lerna 是本地单用户宿主，只负责配置与装配。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("db", "lerna.db", "local SQLite file")
	user := flag.String("user", "local-user", "configured local user")
	issuer := flag.String("issuer", "local-cli", "authenticated local installation namespace")
	domain := flag.String("domain", "local-adjudication", "fixed adjudication domain")
	flag.Parse()
	h, err := assembly.Open(*path, *user, *domain)
	if err != nil {
		return err
	}
	defer h.Close()
	cli := interaction.CLI{Grants: h.Grants, Confirmations: h.Sessions, ConfirmationTasks: h.Tasks, Ledger: h.Ledger, Egress: h.Egress, Content: h.Content, Observations: h.Content, Sessions: h.Sessions, Tasks: h.Tasks, Durable: h.Durable, Caller: &v1.Caller{UserId: *user, IssuerId: *issuer}, Domain: *domain}

	return cli.Run(context.Background(), flag.Args(), os.Stdout)
}
