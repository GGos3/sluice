package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndReadPID(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "sluice.pid")

	if err := WritePID(pidPath, 12345); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	pid, err := ReadPID(pidPath)
	if err != nil {
		t.Fatalf("read pid: %v", err)
	}

	if pid != 12345 {
		t.Fatalf("expected pid 12345, got %d", pid)
	}
}

func TestReadPID_MissingFile(t *testing.T) {
	_, err := ReadPID(filepath.Join(t.TempDir(), "missing.pid"))
	if err == nil {
		t.Fatal("expected error for missing pid file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestReadPID_CorruptContent(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "sluice.pid")
	if err := os.WriteFile(pidPath, []byte("notanumber\n"), 0o644); err != nil {
		t.Fatalf("write test pid file: %v", err)
	}

	_, err := ReadPID(pidPath)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse pid") {
		t.Fatalf("expected parse pid error, got %v", err)
	}
}

func TestReadPID_NegativePID(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "sluice.pid")
	if err := os.WriteFile(pidPath, []byte("-1\n"), 0o644); err != nil {
		t.Fatalf("write test pid file: %v", err)
	}

	_, err := ReadPID(pidPath)
	if err == nil {
		t.Fatal("expected invalid pid error")
	}
	if !strings.Contains(err.Error(), "invalid pid") {
		t.Fatalf("expected invalid pid error, got %v", err)
	}
}

func TestRemovePID_MissingFile(t *testing.T) {
	err := RemovePID(filepath.Join(t.TempDir(), "missing.pid"))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestIsRunning_CurrentProcess(t *testing.T) {
	if !IsRunning(os.Getpid()) {
		t.Fatal("expected current process to be running")
	}
}

func TestIsRunning_ZeroPID(t *testing.T) {
	if IsRunning(0) {
		t.Fatal("expected pid 0 to be not running")
	}
}

func TestIsRunning_BogusHighPID(t *testing.T) {
	if IsRunning(999999999) {
		t.Fatal("expected bogus pid to be not running")
	}
}

func TestFindProcess_StaleFile(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "sluice.pid")
	if err := WritePID(pidPath, 999999999); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	_, err := FindProcess(pidPath)
	if err == nil {
		t.Fatal("expected stale pid file error")
	}
	if !strings.Contains(err.Error(), "stale pid file") {
		t.Fatalf("expected stale pid file error, got %v", err)
	}
}

func TestWritePID_AtomicRename(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "sluice.pid")

	if err := WritePID(pidPath, os.Getpid()); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	if _, err := os.Stat(pidPath + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected temp pid file to be removed, got err=%v", err)
	}
}
