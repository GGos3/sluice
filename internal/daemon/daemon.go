//go:build linux

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const EnvDaemonChild = "SLUICE_DAEMON_CHILD"

type Config struct {
	PIDFile string
	LogFile string
	Args    []string
}

func (c Config) Validate() error {
	if c.PIDFile == "" {
		return fmt.Errorf("pid file is required")
	}
	if c.LogFile == "" {
		return fmt.Errorf("log file is required")
	}
	if len(c.Args) == 0 {
		return fmt.Errorf("args are required")
	}

	return nil
}

func Daemonize(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("daemonize: validate config: %w", err)
	}

	exe, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return fmt.Errorf("daemonize: read exe path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755); err != nil {
		return fmt.Errorf("daemonize: create log directory: %w", err)
	}

	logFd, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("daemonize: open log file: %w", err)
	}
	defer func() {
		_ = logFd.Close()
	}()

	devNull, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("daemonize: open /dev/null: %w", err)
	}
	defer func() {
		_ = devNull.Close()
	}()

	env := append(os.Environ(), EnvDaemonChild+"=1")
	child, err := os.StartProcess(exe, cfg.Args, &os.ProcAttr{
		Dir:   "/",
		Env:   env,
		Files: []*os.File{devNull, logFd, logFd},
	})
	if err != nil {
		return fmt.Errorf("daemonize: start child process: %w", err)
	}

	time.Sleep(100 * time.Millisecond)
	if !IsRunning(child.Pid) {
		return fmt.Errorf("daemonize: child process not running: %d", child.Pid)
	}

	if err := WritePID(cfg.PIDFile, child.Pid); err != nil {
		return fmt.Errorf("daemonize: write pid file: %w", err)
	}

	if err := child.Release(); err != nil {
		return fmt.Errorf("daemonize: release child process: %w", err)
	}

	return nil
}

func IsChild() bool {
	return os.Getenv(EnvDaemonChild) == "1"
}

func IsSystemd() bool {
	return os.Getenv("INVOCATION_ID") != ""
}

func StopProcess(pidFile string) error {
	pid, err := FindProcess(pidFile)
	if err != nil {
		return fmt.Errorf("stop process: find process: %w", err)
	}

	if err := unix.Kill(pid, unix.SIGTERM); err != nil {
		return fmt.Errorf("stop process: send sigterm: %w", err)
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()

	for {
		if !IsRunning(pid) {
			break
		}

		select {
		case <-ticker.C:
		case <-timeout.C:
			return fmt.Errorf("stop process: timeout waiting for process to stop: %d", pid)
		}
	}

	if err := RemovePID(pidFile); err != nil {
		return fmt.Errorf("stop process: remove pid file: %w", err)
	}

	return nil
}

func StripFlag(args []string, flag string) []string {
	if len(args) == 0 {
		return []string{}
	}

	result := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == flag {
			continue
		}
		result = append(result, arg)
	}

	return result
}
