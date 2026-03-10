//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ggos3/sluice/internal/gateway"
	"github.com/ggos3/sluice/internal/logger"
)

func runGateway(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	cfg := gateway.NewConfigFromFlags(fs)

	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	if len(cfg.Domains) > 0 {
		return fmt.Errorf("--domains is not supported in gateway mode yet; selective domain routing is not implemented")
	}

	log, err := logger.Setup(cfg.LogLevel, cfg.LogFormat, "stderr")
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}

	return gateway.Run(ctx, cfg, log)
}
