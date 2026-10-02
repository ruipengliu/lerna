// Package runtime provides the public host assembly entry point.
// This initial host runs dependency diagnostics and keeps business admission closed.
package runtime

import (
	"context"
	"log/slog"

	"github.com/ruipengliu/lerna/internal/host"
)

const Version = "0.0.0-skeleton"

// Run opens the explicitly configured local host and drains it when ctx ends.
func Run(ctx context.Context, configPath string, logger *slog.Logger) error {
	config, err := host.LoadConfig(configPath)
	if err != nil {
		return err
	}
	return host.Run(ctx, config, logger)
}
