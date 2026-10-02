package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ruipengliu/lerna/runtime"
)

func main() {
	configPath := flag.String("config", "", "explicit local host JSON configuration")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if *configPath == "" || flag.NArg() != 0 {
		logger.Error("usage: harnessd --config dev/config/<role>.json")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runtime.Run(ctx, *configPath, logger); err != nil {
		logger.Error("host failed", "error", err)
		os.Exit(1)
	}
}
