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

func agentCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	cfg := gateway.NewConfigFromFlags(fs)
	port := fs.Int("port", 18080, "local tunnel endpoint port (localhost:{port})")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	if err := gateway.PostProcessConfig(cfg, fs); err != nil {
		return fmt.Errorf("postprocess config: %w", err)
	}

	cfg.ProxyHost = "127.0.0.1"
	cfg.ProxyPort = *port

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	log, err := logger.Setup(cfg.LogLevel, cfg.LogFormat, "stderr")
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}

	return gateway.Run(ctx, cfg, log)
}
