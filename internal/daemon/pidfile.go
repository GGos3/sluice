//go:build linux

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func WritePID(path string, pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid: %d", pid)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create pid directory: %w", err)
	}

	tmpPath := path + ".tmp"
	content := []byte(strconv.Itoa(pid) + "\n")
	if err := os.WriteFile(tmpPath, content, 0o644); err != nil {
		return fmt.Errorf("write temp pid file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename pid file: %w", err)
	}

	return nil
}

func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read pid file: %w", err)
	}

	pidText := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		return 0, fmt.Errorf("parse pid: %w", err)
	}

	if pid <= 0 {
		return 0, fmt.Errorf("invalid pid: %d", pid)
	}

	return pid, nil
}

func RemovePID(path string) error {
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}

	return fmt.Errorf("remove pid file: %w", err)
}

func IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	_, err := os.Stat(fmt.Sprintf("/proc/%d/stat", pid))
	return err == nil
}

func FindProcess(pidFile string) (int, error) {
	pid, err := ReadPID(pidFile)
	if err != nil {
		return 0, err
	}

	if !IsRunning(pid) {
		return 0, fmt.Errorf("stale pid file: process not running: %d", pid)
	}

	return pid, nil
}
