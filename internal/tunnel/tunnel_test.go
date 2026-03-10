package tunnel

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "valid",
			cfg: Config{
				RemoteUser:     "user",
				RemoteHost:     "host",
				SSHPort:        22,
				LocalPort:      18080,
				RemoteBindPort: 18080,
			},
		},
		{
			name: "missing remote host",
			cfg: Config{
				RemoteUser:     "user",
				RemoteHost:     "",
				SSHPort:        22,
				LocalPort:      18080,
				RemoteBindPort: 18080,
			},
			wantErr: "remote host is required",
		},
		{
			name: "missing remote user",
			cfg: Config{
				RemoteUser:     "",
				RemoteHost:     "host",
				SSHPort:        22,
				LocalPort:      18080,
				RemoteBindPort: 18080,
			},
			wantErr: "remote user is required",
		},
		{
			name: "invalid ssh port",
			cfg: Config{
				RemoteUser:     "user",
				RemoteHost:     "host",
				SSHPort:        70000,
				LocalPort:      18080,
				RemoteBindPort: 18080,
			},
			wantErr: "ssh port must be between 1 and 65535",
		},
		{
			name: "invalid local port",
			cfg: Config{
				RemoteUser:     "user",
				RemoteHost:     "host",
				SSHPort:        22,
				LocalPort:      0,
				RemoteBindPort: 18080,
			},
			wantErr: "local port must be between 1 and 65535",
		},
		{
			name: "invalid remote bind port",
			cfg: Config{
				RemoteUser:     "user",
				RemoteHost:     "host",
				SSHPort:        22,
				LocalPort:      18080,
				RemoteBindPort: 70000,
			},
			wantErr: "remote bind port must be between 1 and 65535",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("Validate() error = nil, want %q", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("Validate() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestStartBuildsExpectedSSHCommand(t *testing.T) {
	t.Parallel()

	cfg := Config{
		RemoteUser:     "testuser",
		RemoteHost:     "192.168.1.100",
		SSHPort:        22,
		LocalPort:      18080,
		RemoteBindPort: 18080,
	}

	m := NewManager(cfg, slog.Default())

	var mu sync.Mutex
	var gotName string
	var gotArgs []string
	proc := newFakeProcess()
	m.startProcess = func(_ context.Context, name string, args ...string) (process, error) {
		mu.Lock()
		defer mu.Unlock()
		gotName = name
		gotArgs = append([]string(nil), args...)
		return proc, nil
	}
	m.sleep = func(_ context.Context, _ time.Duration) bool { return false }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	mu.Lock()
	if gotName != "ssh" {
		t.Fatalf("command = %q, want %q", gotName, "ssh")
	}
	wantArgs := []string{
		"-R", "18080:localhost:18080",
		"-o", "ServerAliveInterval=60",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-p", "22",
		"-N",
		"testuser@192.168.1.100",
	}
	if len(gotArgs) != len(wantArgs) {
		mu.Unlock()
		t.Fatalf("args len = %d, want %d (%v)", len(gotArgs), len(wantArgs), gotArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			mu.Unlock()
			t.Fatalf("arg[%d] = %q, want %q", i, gotArgs[i], wantArgs[i])
		}
	}
	mu.Unlock()

	if err := m.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestStartRetriesOnExitUntilContextCancelled(t *testing.T) {
	t.Parallel()

	m := NewManager(Config{
		RemoteUser:     "user",
		RemoteHost:     "host",
		SSHPort:        22,
		LocalPort:      18080,
		RemoteBindPort: 18080,
	}, slog.Default())

	firstProc := newFakeProcess()
	secondProc := newFakeProcess()

	startCountCh := make(chan int, 4)
	var mu sync.Mutex
	startCount := 0
	m.startProcess = func(_ context.Context, _ string, _ ...string) (process, error) {
		mu.Lock()
		defer mu.Unlock()
		startCount++
		startCountCh <- startCount
		switch startCount {
		case 1:
			return firstProc, nil
		case 2:
			return secondProc, nil
		default:
			return nil, errors.New("unexpected extra start")
		}
	}
	m.sleep = func(ctx context.Context, _ time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := m.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start() error = %v", err)
	}

	firstProc.exit(nil)

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()

	gotSecond := false
	for !gotSecond {
		select {
		case n := <-startCountCh:
			if n >= 2 {
				gotSecond = true
			}
		case <-deadline.C:
			cancel()
			_ = m.Stop()
			t.Fatal("manager did not attempt reconnect")
		}
	}

	cancel()
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

type fakeProcess struct {
	mu      sync.Mutex
	waitCh  chan struct{}
	waitErr error
	killed  bool
	closed  bool
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{waitCh: make(chan struct{})}
}

func (p *fakeProcess) Wait() error {
	<-p.waitCh
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

func (p *fakeProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.killed = true
	if !p.closed {
		p.closed = true
		close(p.waitCh)
	}
	return nil
}

func (p *fakeProcess) exit(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.waitErr = err
	if !p.closed {
		p.closed = true
		close(p.waitCh)
	}
}
