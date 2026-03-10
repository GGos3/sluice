package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ggos3/sluice/internal/acl"
	"github.com/ggos3/sluice/internal/config"
	"github.com/ggos3/sluice/internal/dns"
	"github.com/ggos3/sluice/internal/logger"
	"github.com/ggos3/sluice/internal/proxy"
	"github.com/ggos3/sluice/internal/tunnel"
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
	case "run":
		return runCmd(ctx, args[1:])
	case "gateway":
		return runGateway(ctx, args[1:])
	case "agent":
		return agentCmd(ctx, args[1:])
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
  run       Run a command with proxy environment variables
  gateway   Run as transparent proxy gateway (Linux only)
  agent     Run as transparent proxy agent (Linux only)
  version   Show version information

Run 'sluice <command> -h' for more information on a command.
`)
}

func versionCmd() error {
	fmt.Printf("sluice %s (built %s)\n", version, buildTime)
	return nil
}

func runCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	proxyHost := fs.String("proxy-host", "localhost", "proxy server host (default: localhost)")
	proxyPort := fs.String("port", "18080", "proxy server port")
	proxyUser := fs.String("proxy-user", "", "proxy authentication username")
	proxyPass := fs.String("proxy-pass", "", "proxy authentication password")
	noProxy := fs.String("no-proxy", "localhost,127.0.0.1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16", "comma-separated list of hosts to bypass proxy")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	authPart := ""
	if *proxyUser != "" {
		authPart = *proxyUser
		if *proxyPass != "" {
			authPart = authPart + ":" + *proxyPass
		}
		authPart = authPart + "@"
	}

	proxyURL := fmt.Sprintf("http://%s%s:%s", authPart, *proxyHost, *proxyPort)

	os.Setenv("HTTP_PROXY", proxyURL)
	os.Setenv("HTTPS_PROXY", proxyURL)
	os.Setenv("NO_PROXY", *noProxy)
	os.Setenv("http_proxy", proxyURL)
	os.Setenv("https_proxy", proxyURL)
	os.Setenv("no_proxy", *noProxy)

	remaining := fs.Args()

	var shellPath string
	var argv []string
	if len(remaining) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "sh"
		}
		var err error
		shellPath, err = exec.LookPath(shell)
		if err != nil {
			return fmt.Errorf("shell not found: %s", shell)
		}
		argv = []string{shellPath}
	} else {
		var err error
		shellPath, err = exec.LookPath(remaining[0])
		if err != nil {
			return fmt.Errorf("command not found: %s", remaining[0])
		}
		argv = append([]string{shellPath}, remaining[1:]...)
	}

	err := syscall.Exec(shellPath, argv, os.Environ())
	return fmt.Errorf("exec failed: %w", err)
}

func serverCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "configs/config.yaml", "path to configuration file")
	tunnelTarget := fs.String("tunnel", "", "SSH reverse tunnel in user@host format")
	sshPort := fs.Int("ssh-port", 22, "SSH port for tunnel connection")
	port := fs.Int("port", 18080, "proxy server listen port")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	portOverridden := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			portOverridden = true
		}
	})

	useTunnel := strings.TrimSpace(*tunnelTarget) != ""

	var (
		cfg       *config.Config
		generated bool
		err       error
	)

	if useTunnel {
		cfg = config.Default()
		cfg.Server.Host = "127.0.0.1"
		cfg.Server.Port = *port
	} else {
		cfg, generated, err = config.Ensure(*configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if portOverridden {
			cfg.Server.Port = *port
		}
	}

	log, err := logger.Setup(cfg.Logging.Level, cfg.Logging.Format, cfg.Logging.AccessLog)
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}

	if generated {
		log.Info("generated default config", "path", *configPath)
	}

	whitelist := acl.New(cfg.Whitelist.Enabled, cfg.Whitelist.Domains)
	dohHandler := dns.NewHandler(log)

	var opts []proxy.Option
	if cfg.Auth.Enabled {
		credentials := make(map[string]string, len(cfg.Auth.Credentials))
		for _, cred := range cfg.Auth.Credentials {
			credentials[cred.Username] = cred.Password
		}
		opts = append(opts, proxy.WithAuth(credentials))
	}

	handlerArgs := make([]any, 0, len(opts)+1)
	handlerArgs = append(handlerArgs, dohHandler)
	for _, opt := range opts {
		handlerArgs = append(handlerArgs, opt)
	}
	handler := proxy.NewHandler(whitelist, log, handlerArgs...)

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
			"tunnel_mode", useTunnel,
			"whitelist_enabled", cfg.Whitelist.Enabled,
			"auth_enabled", cfg.Auth.Enabled,
			"version", version,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	var tunnelMgr *tunnel.Manager
	if useTunnel {
		user, host, err := parseTunnelTarget(*tunnelTarget)
		if err != nil {
			return err
		}

		tunnelCfg := tunnel.Config{
			RemoteUser:     user,
			RemoteHost:     host,
			SSHPort:        *sshPort,
			LocalPort:      *port,
			RemoteBindPort: *port,
		}

		tunnelMgr = tunnel.NewManager(tunnelCfg, log)
		if err := tunnelMgr.Start(runCtx); err != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
			return fmt.Errorf("start tunnel: %w", err)
		}
	}

	select {
	case <-ctx.Done():
		log.Info("received signal, shutting down", "signal", ctx.Err().Error())
	case err := <-errCh:
		cancelRun()
		if tunnelMgr != nil {
			if stopErr := tunnelMgr.Stop(); stopErr != nil {
				return errors.Join(fmt.Errorf("server error: %w", err), fmt.Errorf("stop tunnel: %w", stopErr))
			}
		}

		serverCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if shutdownErr := srv.Shutdown(serverCtx); shutdownErr != nil {
			return errors.Join(fmt.Errorf("server error: %w", err), fmt.Errorf("server shutdown: %w", shutdownErr))
		}
		return fmt.Errorf("server error: %w", err)
	}

	cancelRun()

	if tunnelMgr != nil {
		if err := tunnelMgr.Stop(); err != nil {
			return fmt.Errorf("stop tunnel: %w", err)
		}
	}

	serverCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(serverCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	log.Info("proxy server stopped")
	return nil
}

func parseTunnelTarget(value string) (string, string, error) {
	target := strings.TrimSpace(value)
	user, host, ok := strings.Cut(target, "@")
	if !ok || strings.TrimSpace(user) == "" || strings.TrimSpace(host) == "" {
		return "", "", fmt.Errorf("invalid --tunnel value %q: expected user@host", value)
	}

	host = strings.TrimSpace(host)
	if h, p, err := net.SplitHostPort(host); err == nil {
		if strings.TrimSpace(h) == "" || strings.TrimSpace(p) == "" {
			return "", "", fmt.Errorf("invalid --tunnel value %q: expected user@host", value)
		}
		host = h
	}

	return strings.TrimSpace(user), host, nil
}
