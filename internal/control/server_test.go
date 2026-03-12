package control

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ggos3/sluice/internal/acl"
)

func startTestServer(t *testing.T) (string, *acl.Whitelist, func()) {
	t.Helper()

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	whitelist := acl.New(true, []string{"github.com", "*.golang.org"})
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	srv := NewServer(socketPath, whitelist, log)
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}

	return socketPath, whitelist, func() {
		if err := srv.Stop(); err != nil {
			t.Errorf("stop server: %v", err)
		}
	}
}

func TestServerStartStop(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	whitelist := acl.New(false, nil)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	srv := NewServer(socketPath, whitelist, log)
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("socket file should exist: %v", err)
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatal("socket file should be removed after stop")
	}
}

func TestServerDenyCommand(t *testing.T) {
	socketPath, whitelist, cleanup := startTestServer(t)
	defer cleanup()

	resp, err := SendCommand(socketPath, Request{Action: "deny", Domain: "evil.com"})
	if err != nil {
		t.Fatalf("send command: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}

	if whitelist.IsAllowed("evil.com") {
		t.Fatal("evil.com should be denied after deny command")
	}
}

func TestServerDenyBlocksWhitelistedDomain(t *testing.T) {
	socketPath, whitelist, cleanup := startTestServer(t)
	defer cleanup()

	if !whitelist.IsAllowed("github.com") {
		t.Fatal("github.com should be allowed by static whitelist")
	}

	resp, err := SendCommand(socketPath, Request{Action: "deny", Domain: "github.com"})
	if err != nil {
		t.Fatalf("send command: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}

	if whitelist.IsAllowed("github.com") {
		t.Fatal("github.com should be denied after deny command")
	}
}

func TestServerAllowCommand(t *testing.T) {
	socketPath, whitelist, cleanup := startTestServer(t)
	defer cleanup()

	if whitelist.IsAllowed("newdomain.com") {
		t.Fatal("newdomain.com should not be allowed before allow command")
	}

	resp, err := SendCommand(socketPath, Request{Action: "allow", Domain: "newdomain.com"})
	if err != nil {
		t.Fatalf("send command: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}

	if !whitelist.IsAllowed("newdomain.com") {
		t.Fatal("newdomain.com should be allowed after allow command")
	}
}

func TestServerRemoveCommand(t *testing.T) {
	socketPath, whitelist, cleanup := startTestServer(t)
	defer cleanup()

	// Add a deny rule first
	_, err := SendCommand(socketPath, Request{Action: "deny", Domain: "temp.com"})
	if err != nil {
		t.Fatalf("deny: %v", err)
	}
	if whitelist.IsAllowed("temp.com") {
		t.Fatal("temp.com should be denied")
	}

	// Remove it
	resp, err := SendCommand(socketPath, Request{Action: "remove", Domain: "temp.com"})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}
	if resp.Message != "removed: temp.com" {
		t.Fatalf("message = %q, want 'removed: temp.com'", resp.Message)
	}
}

func TestServerRemoveNotFound(t *testing.T) {
	socketPath, _, cleanup := startTestServer(t)
	defer cleanup()

	resp, err := SendCommand(socketPath, Request{Action: "remove", Domain: "nonexistent.com"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}
	if resp.Message != "not found: nonexistent.com" {
		t.Fatalf("message = %q, want 'not found: nonexistent.com'", resp.Message)
	}
}

func TestServerRulesCommand(t *testing.T) {
	socketPath, _, cleanup := startTestServer(t)
	defer cleanup()

	// Add some dynamic rules
	_, err := SendCommand(socketPath, Request{Action: "deny", Domain: "evil.com"})
	if err != nil {
		t.Fatalf("deny: %v", err)
	}
	_, err = SendCommand(socketPath, Request{Action: "allow", Domain: "good.com"})
	if err != nil {
		t.Fatalf("allow: %v", err)
	}

	resp, err := SendCommand(socketPath, Request{Action: "rules"})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}

	// Expect: 2 runtime rules + 2 config rules (github.com, *.golang.org)
	if len(resp.Rules) != 4 {
		t.Fatalf("rules count = %d, want 4", len(resp.Rules))
	}

	// Verify runtime rules come first
	if resp.Rules[0].Source != "runtime" || resp.Rules[0].Action != "deny" || resp.Rules[0].Domain != "evil.com" {
		t.Fatalf("rules[0] = %+v, want {evil.com deny runtime}", resp.Rules[0])
	}
	if resp.Rules[1].Source != "runtime" || resp.Rules[1].Action != "allow" || resp.Rules[1].Domain != "good.com" {
		t.Fatalf("rules[1] = %+v, want {good.com allow runtime}", resp.Rules[1])
	}

	// Verify config rules
	if resp.Rules[2].Source != "config" || resp.Rules[2].Domain != "github.com" {
		t.Fatalf("rules[2] = %+v, want {github.com allow config}", resp.Rules[2])
	}
}

func TestServerUnknownAction(t *testing.T) {
	socketPath, _, cleanup := startTestServer(t)
	defer cleanup()

	resp, err := SendCommand(socketPath, Request{Action: "invalid"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if resp.OK {
		t.Fatal("expected error for unknown action")
	}
	if resp.Error == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestServerEmptyDomain(t *testing.T) {
	socketPath, _, cleanup := startTestServer(t)
	defer cleanup()

	for _, action := range []string{"deny", "allow", "remove"} {
		resp, err := SendCommand(socketPath, Request{Action: action, Domain: ""})
		if err != nil {
			t.Fatalf("%s: send: %v", action, err)
		}
		if resp.OK {
			t.Fatalf("%s: expected error for empty domain", action)
		}
	}
}

func TestServerConcurrentCommands(t *testing.T) {
	socketPath, _, cleanup := startTestServer(t)
	defer cleanup()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			domain := "concurrent" + string(rune('a'+n)) + ".com"
			_, _ = SendCommand(socketPath, Request{Action: "deny", Domain: domain})
			_, _ = SendCommand(socketPath, Request{Action: "rules"})
			_, _ = SendCommand(socketPath, Request{Action: "remove", Domain: domain})
		}(i % 26)
	}
	wg.Wait()
}

func TestSendCommand_NoServer(t *testing.T) {
	_, err := SendCommand("/tmp/nonexistent-sluice-test.sock", Request{Action: "rules"})
	if err == nil {
		t.Fatal("expected error when connecting to non-existent socket")
	}
}
