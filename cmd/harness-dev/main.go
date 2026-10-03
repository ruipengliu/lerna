package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/cmd/internal/bootstrap"
)

func main() {
	configPath := flag.String("config", "/workspace/lerna-dev/config.json", "private process configuration")
	dataRoot := flag.String("data", "/workspace/lerna-dev", "private development data directory")
	driver := flag.String("driver", "postgres", "postgres or sqlite (explicit development only)")
	initialize := flag.Bool("init", false, "run management migrations and initialize explicit development identities and rules")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var c bootstrap.Config
	var err error
	if *initialize {
		c, err = bootstrap.InitializeConfig(ctx, *configPath, *dataRoot, *driver)
	} else {
		c, err = bootstrap.LoadConfig(*configPath)
	}
	if err == nil {
		var app *bootstrap.App
		app, err = bootstrap.OpenApp(ctx, c, *initialize)
		if err == nil {
			fmt.Printf("Harness development: http://%s\nPrivate configuration: %s\n", c.HTTPAddr, *configPath)
			err = errors.Join(app.Run(ctx, true, true), app.Close())
		}
	}
	if err != nil {
		var business *api.Error
		if errors.As(err, &business) {
			fmt.Fprintf(os.Stderr, "%s: %s\n", business.Code, business.Reason)
		} else {
			fmt.Fprintln(os.Stderr, "development process failed; verify configured dependencies")
		}
		os.Exit(1)
	}
}
