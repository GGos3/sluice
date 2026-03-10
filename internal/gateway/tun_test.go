//go:build linux

package gateway

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func tunAvailable() bool {
	if os.Getuid() != 0 {
		return false
	}
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func skipIfTunUnavailable(t *testing.T) {
	if !tunAvailable() {
		t.Skip("requires root privileges and /dev/net/tun")
	}
}

func TestTUNDeviceNilReceivers(t *testing.T) {
	var d *TUNDevice

	if got := d.Name(); got != "" {
		t.Errorf("Name() = %q, want empty", got)
	}
	if got := d.File(); got != nil {
		t.Errorf("File() = %v, want nil", got)
	}
	if got := d.FD(); got != -1 {
		t.Errorf("FD() = %d, want -1", got)
	}
	if got := d.MTU(); got != 0 {
		t.Errorf("MTU() = %d, want 0", got)
	}
	if err := d.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}

	closed := d.Closed()
	select {
	case <-closed:
	default:
		t.Error("Closed() channel should be closed for nil receiver")
	}
}

func TestTUNDeviceRequiresRoot(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, skip non-root test")
	}

	_, err := NewTUN("testtun0", 1420)
	if err == nil {
		t.Fatal("NewTUN() succeeded without root, want error")
	}
}

func TestTUNDeviceLifecycle(t *testing.T) {
	skipIfTunUnavailable(t)

	dev, err := NewTUN("sluicetest0", 1420)
	if err != nil {
		t.Fatalf("NewTUN() = %v", err)
	}
	defer dev.Close()

	if name := dev.Name(); name == "" {
		t.Error("Name() returned empty string")
	}

	if fd := dev.FD(); fd < 0 {
		t.Errorf("FD() = %d, want >= 0", fd)
	}

	if file := dev.File(); file == nil {
		t.Error("File() returned nil")
	}

	if mtu := dev.MTU(); mtu <= 0 {
		t.Errorf("MTU() = %d, want > 0", mtu)
	}

	closed := dev.Closed()
	select {
	case <-closed:
		t.Error("Closed() channel should not be closed yet")
	default:
	}

	if err := dev.Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}

	select {
	case <-closed:
	default:
		t.Error("Closed() channel should be closed after Close()")
	}

	if err := dev.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil (idempotent)", err)
	}
}

func TestTUNDeviceKernelAssignedName(t *testing.T) {
	skipIfTunUnavailable(t)

	dev, err := NewTUN("", 1420)
	if err != nil {
		t.Fatalf("NewTUN(\"\") = %v", err)
	}
	defer dev.Close()

	name := dev.Name()
	if name == "" {
		t.Error("kernel should have assigned a name")
	}
	t.Logf("kernel assigned name: %s", name)
}

func TestTUNDeviceStaleRecovery(t *testing.T) {
	skipIfTunUnavailable(t)

	const tunName = "sluicestale0"

	dev1, err := NewTUN(tunName, 1420)
	if err != nil {
		t.Fatalf("first NewTUN() = %v", err)
	}

	actualName := dev1.Name()
	t.Logf("first device name: %s", actualName)

	_ = dev1.Close()

	dev2, err := NewTUN(tunName, 1420)
	if err != nil {
		t.Fatalf("second NewTUN() after close = %v", err)
	}
	defer dev2.Close()

	t.Logf("second device name: %s", dev2.Name())
}

func TestIsDeviceInUse(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "unrelated error",
			err:      errors.New("some error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDeviceInUse(tt.err); got != tt.expected {
				t.Errorf("isDeviceInUse(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestTUNDeviceMTUDefault(t *testing.T) {
	skipIfTunUnavailable(t)

	dev, err := NewTUN("", 0)
	if err != nil {
		t.Fatalf("NewTUN(\"\", 0) = %v", err)
	}
	defer dev.Close()

	mtu := dev.MTU()
	if mtu <= 0 {
		t.Errorf("MTU() = %d, want > 0", mtu)
	}
	t.Logf("MTU: %d", mtu)
}

func TestMain(m *testing.M) {
	if os.Getuid() != 0 {
		if _, err := exec.LookPath("unshare"); err == nil {
			cmd := exec.Command("unshare", "--user", "--map-root-user", os.Args[0], "-test.run=^Test")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Env = append(os.Environ(), "GATEWAY_TEST_UNPRIVILEGED=1")
			if err := cmd.Run(); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func init() {
	if os.Getenv("GATEWAY_TEST_UNPRIVILEGED") != "" {
		runtime.LockOSThread()
	}
}
