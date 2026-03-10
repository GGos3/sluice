package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ggos3/sluice/internal/acl"
	"github.com/ggos3/sluice/internal/config"
	"github.com/ggos3/sluice/internal/logger"
	"github.com/ggos3/sluice/internal/proxy"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	args := os.Args[1:]

	if len(args) == 0 {
		printUsage()
		return fmt.Errorf("no command specified")
	}

	switch args[0] {
	case "server":
		return serverCmd(ctx, args[1:])
	case "gateway":
		return gatewayCmd(ctx, args[1:])
	case "version":
		return versionCmd()
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: sluice <command> [flags]

Commands:
  server    Start the proxy server
  gateway   Run as transparent proxy gateway (not yet implemented)
  version   Show version information

Run 'sluice <command> -h' for more information on a command.
`)
}

func versionCmd() error {
	fmt.Printf("sluice %s (built %s)\n", version, buildTime)
	return nil
}

func serverCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "configs/config.yaml", "path to configuration file")

	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, generated, err := config.Ensure(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log, err := logger.Setup(cfg.Logging.Level, cfg.Logging.Format, cfg.Logging.AccessLog)
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}

	if generated {
		log.Info("generated default config", "path", *configPath)
	}

	whitelist := acl.New(cfg.Whitelist.Enabled, cfg.Whitelist.Domains)

	var opts []proxy.Option
	if cfg.Auth.Enabled {
		credentials := make(map[string]string, len(cfg.Auth.Credentials))
		for _, cred := range cfg.Auth.Credentials {
			credentials[cred.Username] = cred.Password
		}
		opts = append(opts, proxy.WithAuth(credentials))
	}

	handler := proxy.NewHandler(whitelist, log, opts...)

	srv := &http.Server{
		Addr:         cfg.Address(),
		Handler:      handler,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeout) * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("starting proxy server",
			"address", cfg.Address(),
			"whitelist_enabled", cfg.Whitelist.Enabled,
			"auth_enabled", cfg.Auth.Enabled,
			"version", version,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("received signal, shutting down", "signal", ctx.Err().Error())
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	serverCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(serverCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	log.Info("proxy server stopped")
	return nil
}

func gatewayCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	proxyHost := fs.String("proxy-host", "", "proxy server host (required)")
	proxyPort := fs.String("proxy-port", "8080", "proxy server port")
	proxyUser := fs.String("proxy-user", "", "proxy authentication username")
	proxyPass := fs.String("proxy-pass", "", "proxy authentication password")
	domains := fs.String("domains", "", "comma-separated list of domains to proxy (empty for all)")
	logLevel := fs.String("log-level", "info", "logging level (debug, info, warn, error)")
	logFormat := fs.String("log-format", "json", "log format (json, text)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *proxyHost == "" {
		fmt.Println("Gateway mode is not yet implemented.")
		fmt.Println("Use 'sluice server' to start the proxy server.")
		return nil
	}

	_ = proxyPort
	_ = proxyUser
	_ = proxyPass
	_ = domains
	_ = logLevel
	_ = logFormat
	_ = ctx

	fmt.Println("Gateway mode is not yet implemented.")
	fmt.Println("Use 'sluice server' to start the proxy server.")
	return nil
}
