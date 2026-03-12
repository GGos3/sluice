//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ggos3/sluice/internal/daemon"
	"github.com/ggos3/sluice/internal/gateway"
	"github.com/ggos3/sluice/internal/logger"
)

func agentCmd(ctx context.Context, args []string) error {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "start":
			return agentStartCmd(ctx, args[1:])
		case "stop":
			return stopServiceCmd("agent", args[1:])
		}
	}

	return agentStartCmd(ctx, args)
}

func agentStartCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	cfg := gateway.NewConfigFromFlags(fs)
	port := fs.Int("port", 18080, "local tunnel endpoint port (localhost:{port})")
	daemonize := fs.Bool("daemon", false, "run as background daemon")
	pidFile := fs.String("pid-file", "/var/run/sluice/agent.pid", "PID file path (daemon mode)")
	logDir := fs.String("log-dir", "/var/log/sluice", "log directory (daemon mode)")

	// Handle -d alias for --daemon
	for i, arg := range args {
		if arg == "-d" {
			args[i] = "--daemon"
		}
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	if *daemonize {
		if daemon.IsChild() || daemon.IsSystemd() {
			// We are the child process — continue with normal startup
		} else {
			childArgs := daemon.StripFlag(os.Args, "-d")
			childArgs = daemon.StripFlag(childArgs, "--daemon")
			logFile := filepath.Join(*logDir, "agent.log")
			return daemon.Daemonize(daemon.Config{
				PIDFile: *pidFile,
				LogFile: logFile,
				Args:    childArgs,
			})
		}
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

	if daemon.IsChild() {
		if err := daemon.WritePID(*pidFile, os.Getpid()); err != nil {
			log.Warn("failed to write pid file", "error", err)
		}
		defer func() {
			_ = daemon.RemovePID(*pidFile)
		}()
	}

	return gateway.Run(ctx, cfg, log)
}
