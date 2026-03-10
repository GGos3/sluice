package tunnel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultSSHPort        = 22
	defaultLocalPort      = 18080
	defaultRemoteBindPort = 18080
	defaultReconnectDelay = 2 * time.Second
)

type Config struct {
	RemoteUser     string
	RemoteHost     string
	SSHPort        int
	LocalPort      int
	RemoteBindPort int
}

func (c *Config) normalize() {
	c.RemoteUser = strings.TrimSpace(c.RemoteUser)
	c.RemoteHost = strings.TrimSpace(c.RemoteHost)
	if c.SSHPort == 0 {
		c.SSHPort = defaultSSHPort
	}
	if c.LocalPort == 0 {
		c.LocalPort = defaultLocalPort
	}
	if c.RemoteBindPort == 0 {
		c.RemoteBindPort = defaultRemoteBindPort
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.RemoteUser) == "" {
		return errors.New("remote user is required")
	}
	if strings.TrimSpace(c.RemoteHost) == "" {
		return errors.New("remote host is required")
	}
	if c.SSHPort < 1 || c.SSHPort > 65535 {
		return errors.New("ssh port must be between 1 and 65535")
	}
	if c.LocalPort < 1 || c.LocalPort > 65535 {
		return errors.New("local port must be between 1 and 65535")
	}
	if c.RemoteBindPort < 1 || c.RemoteBindPort > 65535 {
		return errors.New("remote bind port must be between 1 and 65535")
	}
	return nil
}

type process interface {
	Wait() error
	Kill() error
}

type managerProcess struct {
	cmd *exec.Cmd
}

func (p *managerProcess) Wait() error {
	return p.cmd.Wait()
}

func (p *managerProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

type processStarter func(ctx context.Context, name string, args ...string) (process, error)

type Manager struct {
	cfg Config
	log *slog.Logger

	startProcess processStarter
	sleep        func(ctx context.Context, d time.Duration) bool

	reconnectDelay time.Duration

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	doneCh  chan struct{}
	proc    process
}

func NewManager(cfg Config, logger *slog.Logger) *Manager {
	cfg.normalize()
	if logger == nil {
		logger = slog.Default()
	}

	return &Manager{
		cfg:            cfg,
		log:            logger,
		startProcess:   startOSProcess,
		sleep:          sleepWithContext,
		reconnectDelay: defaultReconnectDelay,
	}
}

func (m *Manager) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if err := m.cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return errors.New("tunnel manager already started")
	}
	childCtx, cancel := context.WithCancel(ctx)
	doneCh := make(chan struct{})
	firstStart := make(chan error, 1)
	m.started = true
	m.cancel = cancel
	m.doneCh = doneCh
	m.mu.Unlock()

	go m.run(childCtx, doneCh, firstStart)

	select {
	case err := <-firstStart:
		if err != nil {
			_ = m.Stop()
			return err
		}
		return nil
	case <-ctx.Done():
		_ = m.Stop()
		return context.Cause(ctx)
	}
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	cancel := m.cancel
	proc := m.proc
	doneCh := m.doneCh
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	var stopErr error
	if proc != nil {
		if err := proc.Kill(); err != nil {
			stopErr = errors.Join(stopErr, fmt.Errorf("kill ssh process: %w", err))
		}
	}

	if doneCh != nil {
		<-doneCh
	}

	m.mu.Lock()
	m.started = false
	m.cancel = nil
	m.doneCh = nil
	m.proc = nil
	m.mu.Unlock()

	if stopErr != nil {
		return stopErr
	}
	return nil
}

func (m *Manager) run(ctx context.Context, doneCh chan struct{}, firstStart chan<- error) {
	defer close(doneCh)
	first := true

	for {
		proc, err := m.startProcess(ctx, "ssh", m.commandArgs()...)
		if first {
			if err != nil {
				firstStart <- fmt.Errorf("start ssh reverse tunnel: %w", err)
				return
			}
			firstStart <- nil
			first = false
		}
		if err != nil {
			m.log.Warn("ssh tunnel start failed", "error", err)
			if !m.sleep(ctx, m.reconnectDelay) {
				return
			}
			continue
		}

		m.mu.Lock()
		m.proc = proc
		m.mu.Unlock()

		m.log.Info("ssh reverse tunnel connected",
			"remote", fmt.Sprintf("%s@%s", m.cfg.RemoteUser, m.cfg.RemoteHost),
			"ssh_port", m.cfg.SSHPort,
			"remote_bind_port", m.cfg.RemoteBindPort,
			"local_port", m.cfg.LocalPort,
		)

		waitErr := proc.Wait()

		m.mu.Lock()
		if m.proc == proc {
			m.proc = nil
		}
		m.mu.Unlock()

		if ctx.Err() != nil {
			m.log.Info("ssh reverse tunnel stopped")
			return
		}

		if waitErr != nil {
			m.log.Warn("ssh reverse tunnel disconnected", "error", waitErr)
		} else {
			m.log.Warn("ssh reverse tunnel exited")
		}

		if !m.sleep(ctx, m.reconnectDelay) {
			return
		}
		m.log.Info("ssh reverse tunnel reconnecting")
	}
}

func (m *Manager) commandArgs() []string {
	return []string{
		"-R", strconv.Itoa(m.cfg.RemoteBindPort) + ":localhost:" + strconv.Itoa(m.cfg.LocalPort),
		"-o", "ServerAliveInterval=60",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-p", strconv.Itoa(m.cfg.SSHPort),
		"-N",
		m.cfg.RemoteUser + "@" + m.cfg.RemoteHost,
	}
}

func startOSProcess(ctx context.Context, name string, args ...string) (process, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &managerProcess{cmd: cmd}, nil
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
