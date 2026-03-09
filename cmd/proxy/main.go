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
	configPath := flag.String("config", "configs/config.yaml", "path to configuration file")
	showVersion := flag.Bool("version", false, "show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("sluice %s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	if err := run(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log, err := logger.Setup(cfg.Logging.Level, cfg.Logging.Format, cfg.Logging.AccessLog)
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
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

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		log.Info("received signal, shutting down", "signal", sig.String())
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	log.Info("proxy server stopped")
	return nil
}
