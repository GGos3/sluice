package daemon

import (
	"strings"
	"testing"
)

func TestIsChild_Unset(t *testing.T) {
	t.Setenv(EnvDaemonChild, "")

	if IsChild() {
		t.Fatal("expected false when child env is unset")
	}
}

func TestIsChild_Set(t *testing.T) {
	t.Setenv(EnvDaemonChild, "1")

	if !IsChild() {
		t.Fatal("expected true when child env is set")
	}
}

func TestIsSystemd_Unset(t *testing.T) {
	t.Setenv("INVOCATION_ID", "")

	if IsSystemd() {
		t.Fatal("expected false when systemd env is unset")
	}
}

func TestIsSystemd_Set(t *testing.T) {
	t.Setenv("INVOCATION_ID", "abc")

	if !IsSystemd() {
		t.Fatal("expected true when systemd env is set")
	}
}

func TestConfigValidate_EmptyPIDFile(t *testing.T) {
	cfg := Config{LogFile: "/tmp/sluice.log", Args: []string{"sluice", "server"}}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestConfigValidate_EmptyLogFile(t *testing.T) {
	cfg := Config{PIDFile: "/tmp/sluice.pid", Args: []string{"sluice", "server"}}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestConfigValidate_EmptyArgs(t *testing.T) {
	cfg := Config{PIDFile: "/tmp/sluice.pid", LogFile: "/tmp/sluice.log"}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestConfigValidate_Valid(t *testing.T) {
	cfg := Config{
		PIDFile: "/tmp/sluice.pid",
		LogFile: "/tmp/sluice.log",
		Args:    []string{"sluice", "server"},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestStopProcess_MissingPIDFile(t *testing.T) {
	err := StopProcess("/definitely/missing/sluice.pid")
	if err == nil {
		t.Fatal("expected error for missing pid file")
	}
	if !strings.Contains(err.Error(), "stop process: find process") {
		t.Fatalf("expected descriptive error, got %v", err)
	}
}

func TestStripFlag_RemovesDaemon(t *testing.T) {
	in := []string{"sluice", "server", "--daemon", "--port", "80"}
	out := StripFlag(in, "--daemon")

	expected := []string{"sluice", "server", "--port", "80"}
	if len(out) != len(expected) {
		t.Fatalf("expected %d args, got %d", len(expected), len(out))
	}
	for i := range expected {
		if out[i] != expected[i] {
			t.Fatalf("expected %q at %d, got %q", expected[i], i, out[i])
		}
	}
}

func TestStripFlag_NoMatch(t *testing.T) {
	in := []string{"sluice", "server", "--port", "80"}
	out := StripFlag(in, "--daemon")

	if len(out) != len(in) {
		t.Fatalf("expected %d args, got %d", len(in), len(out))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Fatalf("expected %q at %d, got %q", in[i], i, out[i])
		}
	}
}

func TestStripFlag_MultipleOccurrences(t *testing.T) {
	in := []string{"sluice", "--daemon", "server", "--daemon", "--port", "80", "--daemon"}
	out := StripFlag(in, "--daemon")

	expected := []string{"sluice", "server", "--port", "80"}
	if len(out) != len(expected) {
		t.Fatalf("expected %d args, got %d", len(expected), len(out))
	}
	for i := range expected {
		if out[i] != expected[i] {
			t.Fatalf("expected %q at %d, got %q", expected[i], i, out[i])
		}
	}
}

func TestStripFlag_EmptyArgs(t *testing.T) {
	out := StripFlag(nil, "--daemon")
	if len(out) != 0 {
		t.Fatalf("expected empty args, got %v", out)
	}
}
