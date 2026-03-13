//go:build linux

package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSysctlManager_SetAndRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rp_filter")
	if err := os.WriteFile(path, []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var s SysctlManager
	if err := s.set(path, "0"); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "0\n" {
		t.Fatalf("after set: got %q, want %q", got, "0\n")
	}

	if err := s.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}

	got, _ = os.ReadFile(path)
	if string(got) != "1\n" {
		t.Fatalf("after restore: got %q, want %q", got, "1\n")
	}
}

func TestSysctlManager_RestoreReverseOrder(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a")
	pathB := filepath.Join(dir, "b")
	os.WriteFile(pathA, []byte("origA\n"), 0644)
	os.WriteFile(pathB, []byte("origB\n"), 0644)

	var s SysctlManager
	s.set(pathA, "newA")
	s.set(pathB, "newB")

	if err := s.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}

	gotA, _ := os.ReadFile(pathA)
	gotB, _ := os.ReadFile(pathB)
	if string(gotA) != "origA\n" {
		t.Fatalf("a: got %q, want %q", gotA, "origA\n")
	}
	if string(gotB) != "origB\n" {
		t.Fatalf("b: got %q, want %q", gotB, "origB\n")
	}
}

func TestSysctlManager_RestoreNoop(t *testing.T) {
	var s SysctlManager
	if err := s.Restore(); err != nil {
		t.Fatalf("restore on empty manager: %v", err)
	}
}

func TestSysctlManager_SetMissingFile(t *testing.T) {
	var s SysctlManager
	err := s.set("/nonexistent/path/rp_filter", "0")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
