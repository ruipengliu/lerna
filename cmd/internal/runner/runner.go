// Package runner 负责叶进程参数、明确管理动作及实际生命周期；业务装配由 bootstrap 提供。
package runner

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/cmd/internal/bootstrap"
)

func Main(role string) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := Run(ctx, role, os.Args[1:], os.Stdout); err != nil {
		var problem *api.Error
		if errors.As(err, &problem) {
			fmt.Fprintf(os.Stderr, "%s: %s\n", problem.Code, problem.Reason)
		} else {
			fmt.Fprintln(os.Stderr, "dependency_unavailable: process_dependency_failed")
		}
		os.Exit(1)
	}
}

func Run(ctx context.Context, role string, args []string, out io.Writer) (err error) {
	if role != "migrate" && role != "gateway" && role != "application" && role != "worker" {
		return api.E("unsupported", "process_role_not_supported")
	}
	f := flag.NewFlagSet("harness-"+role, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	config := f.String("config", "", "absolute private process configuration file")
	var developmentInit *bool
	var data, driver *string
	if role == "migrate" {
		developmentInit = f.Bool("development-init", false, "explicit management-only development identity/policy initialization")
		data = f.String("data", "", "absolute private development data directory")
		driver = f.String("driver", "postgres", "postgres or explicit development-only sqlite")
	}
	if f.Parse(args) != nil || len(f.Args()) != 0 || !filepath.IsAbs(*config) {
		return api.E("invalid_request", "invalid_process_arguments")
	}
	if role == "migrate" {
		return migrate(ctx, *config, *developmentInit, *data, *driver, out)
	}
	c, err := bootstrap.LoadConfig(*config)
	if err != nil {
		return err
	}
	a, err := bootstrap.OpenAppForRole(ctx, c, false, role)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.Close()) }()
	address := ""
	if role == "gateway" {
		address = c.HTTPAddr
	}
	if role == "application" {
		address = c.GRPCAddr
	}
	if _, err = out.Write(append(api.Raw(struct {
		Role    string `json:"role"`
		Address string `json:"address,omitempty"`
		Status  string `json:"status"`
	}{role, address, "starting"}), '\n')); err != nil {
		return err
	}
	switch role {
	case "gateway":
		return a.Run(ctx, true, false)
	case "application":
		return a.RunRPC(ctx)
	case "worker":
		return a.Run(ctx, false, true)
	}
	return api.E("unsupported", "process_role_not_supported")
}

func migrate(ctx context.Context, configPath string, initialize bool, root, driver string, out io.Writer) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var c bootstrap.Config
	if initialize {
		if !filepath.IsAbs(root) || (driver != "postgres" && driver != "sqlite") {
			return api.E("invalid_request", "invalid_development_initialization")
		}
		c, err = bootstrap.InitializeConfig(ctx, configPath, root, driver)
		if err != nil {
			return err
		}
		var app *bootstrap.App
		app, err = bootstrap.OpenAppForRole(ctx, c, true, "management")
		if err != nil {
			return err
		}
		if err = app.Close(); err != nil {
			return err
		}
	} else {
		if root != "" || driver != "postgres" {
			return api.E("invalid_request", "initialization_flags_require_explicit_management_action")
		}
		c, err = bootstrap.LoadConfig(configPath)
		if err != nil {
			return err
		}
		store, err := bootstrap.OpenStore(ctx, c, false) // 在任何迁移前核原数据库身份。
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, store.Close()) }()
		migrator, ok := store.(interface{ Migrate(context.Context) error })
		if !ok {
			return api.E("unsupported", "storage_migration_not_supported")
		}
		if err = migrator.Migrate(ctx); err != nil {
			return err
		}
	}
	_, err = out.Write(append(api.Raw(struct {
		DatabaseID  string `json:"database_id"`
		Driver      string `json:"driver"`
		Initialized bool   `json:"initialized"`
		Status      string `json:"status"`
	}{c.DatabaseID, c.Driver, initialize, "migrated"}), '\n'))
	return err
}
