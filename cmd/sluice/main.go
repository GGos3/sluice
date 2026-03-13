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
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ggos3/sluice/internal/acl"
	"github.com/ggos3/sluice/internal/config"
	"github.com/ggos3/sluice/internal/control"
	"github.com/ggos3/sluice/internal/daemon"
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
  server [start] [user@host[:port]]   Start the proxy server
  server stop                          Stop the proxy server daemon
  server deny <domain>                 Deny a domain at runtime
  server allow <domain>                Allow a domain at runtime
  server remove <domain>               Remove a runtime domain rule
  server rules                         List active domain rules
  agent [start]                        Run as transparent proxy agent (Linux only)
  agent stop                           Stop the agent daemon
  run                                  Run a command with proxy environment variables
  gateway                              Run as transparent proxy gateway (Linux only)
  version                              Show version information

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

// serverCmd routes server subcommands.
func serverCmd(ctx context.Context, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return serverStartCmd(ctx, args)
	}

	switch args[0] {
	case "start":
		return serverStartCmd(ctx, args[1:])
	case "stop":
		return stopServiceCmd("server", args[1:])
	case "deny", "allow", "remove", "rules":
		return controlCmd(args[0], args[1:])
	default:
		// Could be a positional tunnel target (user@host), route to serverStartCmd
		return serverStartCmd(ctx, args)
	}
}

func serverStartCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("server start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "configs/config.yaml", "path to configuration file")
	port := fs.Int("port", 18080, "proxy server listen port")
	daemonize := fs.Bool("daemon", false, "run as background daemon")
	pidFile := fs.String("pid-file", "/var/run/sluice/server.pid", "PID file path (daemon mode)")
	logDir := fs.String("log-dir", "/var/log/sluice", "log directory (daemon mode)")
	socketPath := fs.String("socket", control.DefaultSocketPath, "control socket path")

	// Extract positional tunnel target (user@host[:port]) before flag parsing.
	// Go's flag package stops parsing at the first non-flag argument, so flags
	// after the positional arg (e.g., "root@host --port 18080") would be lost.
	var tunnelTarget string
	flagArgs := make([]string, 0, len(args))
	for _, arg := range args {
		if tunnelTarget == "" && !strings.HasPrefix(arg, "-") && strings.Contains(arg, "@") {
			tunnelTarget = arg
		} else if arg == "-d" {
			// Handle -d alias for --daemon
			flagArgs = append(flagArgs, "--daemon")
		} else {
			flagArgs = append(flagArgs, arg)
		}
	}

	if err := fs.Parse(flagArgs); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	if tunnelTarget == "" {
		if remaining := fs.Args(); len(remaining) > 0 {
			tunnelTarget = remaining[0]
		}
	}
	useTunnel := tunnelTarget != ""

	// Daemon mode: parent spawns child and exits
	if *daemonize {
		if !daemon.IsChild() && !daemon.IsSystemd() {
			childArgs := daemon.StripFlag(os.Args, "-d")
			childArgs = daemon.StripFlag(childArgs, "--daemon")
			logFile := filepath.Join(*logDir, "server.log")
			return daemon.Daemonize(daemon.Config{
				PIDFile: *pidFile,
				LogFile: logFile,
				Args:    childArgs,
			})
		}
	}

	portOverridden := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			portOverridden = true
		}
	})

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

	// Write PID file if we are the daemon child
	if daemon.IsChild() {
		if writeErr := daemon.WritePID(*pidFile, os.Getpid()); writeErr != nil {
			log.Warn("failed to write pid file", "error", writeErr)
		}
		defer func() {
			_ = daemon.RemovePID(*pidFile)
		}()
	}

	whitelist := acl.New(cfg.Whitelist.Enabled, cfg.Whitelist.Domains)

	// Start control server (non-fatal if it fails)
	ctrlServer := control.NewServer(*socketPath, whitelist, log)
	if ctrlErr := ctrlServer.Start(); ctrlErr != nil {
		log.Warn("control server failed to start", "error", ctrlErr)
	}
	defer func() {
		if stopErr := ctrlServer.Stop(); stopErr != nil {
			log.Warn("control server stop error", "error", stopErr)
		}
	}()

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
		user, host, sshPort, parseErr := parseTunnelTarget(tunnelTarget)
		if parseErr != nil {
			return parseErr
		}

		tunnelCfg := tunnel.Config{
			RemoteUser:     user,
			RemoteHost:     host,
			SSHPort:        sshPort,
			LocalPort:      *port,
			RemoteBindPort: *port,
		}

		tunnelMgr = tunnel.NewManager(tunnelCfg, log)
		if startErr := tunnelMgr.Start(runCtx); startErr != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
			return fmt.Errorf("start tunnel: %w", startErr)
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

// stopServiceCmd stops a running daemon by PID file.
func stopServiceCmd(service string, args []string) error {
	fs := flag.NewFlagSet(service+" stop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	pidFile := fs.String("pid-file", fmt.Sprintf("/var/run/sluice/%s.pid", service), "PID file path")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	if err := daemon.StopProcess(*pidFile); err != nil {
		return fmt.Errorf("stop %s: %w", service, err)
	}

	fmt.Fprintf(os.Stderr, "%s stopped\n", service)
	return nil
}

// controlCmd sends a domain management command to the running server via Unix socket.
func controlCmd(action string, args []string) error {
	fs := flag.NewFlagSet("server "+action, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socketPath := fs.String("socket", control.DefaultSocketPath, "control socket path")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	var domain string
	if action != "rules" {
		remaining := fs.Args()
		if len(remaining) == 0 {
			return fmt.Errorf("%s: domain argument required", action)
		}
		domain = remaining[0]
	}

	resp, err := control.SendCommand(*socketPath, control.Request{
		Action: action,
		Domain: domain,
	})
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}

	if !resp.OK {
		return fmt.Errorf("%s: %s", action, resp.Error)
	}

	if action == "rules" {
		if len(resp.Rules) == 0 {
			fmt.Println("no rules")
			return nil
		}
		for _, r := range resp.Rules {
			fmt.Printf("%-6s  %-40s  (%s)\n", r.Action, r.Domain, r.Source)
		}
	} else {
		fmt.Println(resp.Message)
	}

	return nil
}

func parseTunnelTarget(value string) (user, host string, sshPort int, err error) {
	target := strings.TrimSpace(value)
	u, hostPort, ok := strings.Cut(target, "@")
	if !ok || strings.TrimSpace(u) == "" || strings.TrimSpace(hostPort) == "" {
		return "", "", 0, fmt.Errorf("invalid tunnel target %q: expected user@host[:port]", value)
	}

	user = strings.TrimSpace(u)
	hostPort = strings.TrimSpace(hostPort)

	h, p, splitErr := net.SplitHostPort(hostPort)
	if splitErr != nil {
		// No port specified, use default
		host = hostPort
		sshPort = 22
	} else {
		if strings.TrimSpace(h) == "" {
			return "", "", 0, fmt.Errorf("invalid tunnel target %q: empty host", value)
		}
		host = h
		portNum, parseErr := strconv.Atoi(p)
		if parseErr != nil || portNum <= 0 || portNum > 65535 {
			return "", "", 0, fmt.Errorf("invalid tunnel target %q: invalid port %q", value, p)
		}
		sshPort = portNum
	}

	return user, host, sshPort, nil
}
