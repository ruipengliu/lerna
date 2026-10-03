package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ruipengliu/lerna/adapters/executor"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
)

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return api.E("invalid_request", "executor_command_required")
	}
	verb := args[0]
	flags := flag.NewFlagSet("executor "+verb, flag.ContinueOnError)
	configPath := flags.String("config", "", "paired executor config file")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *configPath == "" || flags.NArg() != 0 {
		return api.E("invalid_request", "executor_config_file_required")
	}
	c, err := executor.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	switch verb {
	case "setup":
		h, err := executor.Open(ctx, c, true)
		if err != nil {
			return err
		}
		err = executor.SaveConfig(*configPath, h.Config)
		return errors.Join(err, h.Close())
	case "serve":
		h, err := executor.Open(ctx, c, false)
		if err != nil {
			return err
		}
		return errors.Join(h.Run(ctx), h.Close())
	case "migrate":
		if c.DatabaseID == "" {
			return api.E("forbidden", "original_device_database_required")
		}
		store, err := sqlite.Open(c.DatabasePath, sqlite.WithExpectedDatabaseID(c.DatabaseID))
		if err != nil {
			return err
		}
		return errors.Join(store.Migrate(ctx), store.Close())
	default:
		return api.E("unsupported", "executor_command_not_supported")
	}
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
