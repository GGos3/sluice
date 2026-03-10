//go:build linux

package gateway

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/rawfile"
)

const (
	tunCloneDevice = "/dev/net/tun"
	tunIfReqSize   = unix.IFNAMSIZ + 64
)

// TUNDevice represents a Linux TUN device with managed lifecycle.
// It provides access to the underlying file descriptor for integration
// with gVisor's fdbased endpoint.
type TUNDevice struct {
	file   *os.File
	fd     int
	name   string
	mtu    uint32
	once   sync.Once
	closed chan struct{}
}

// NewTUN creates a new Linux TUN device with the given name and MTU.
// If name is empty, the kernel will assign a name (e.g., "tun0").
// If the requested name already exists (stale from crash), it attempts
// to recover by trying alternative names.
//
// The device is created with IFF_TUN | IFF_NO_PI flags, suitable for
// gVisor fdbased endpoint integration.
func NewTUN(name string, mtu int) (*TUNDevice, error) {
	if mtu <= 0 {
		mtu = defaultStackMTU
	}

	file, actualName, err := createTUNDevice(name)
	if err != nil {
		// If the name is already in use, try with an empty name to get
		// a kernel-assigned name as a fallback.
		if name != "" && isDeviceInUse(err) {
			file, actualName, err = createTUNDevice("")
			if err != nil {
				return nil, fmt.Errorf("create tun device (fallback): %w", err)
			}
		} else {
			return nil, err
		}
	}

	fd := int(file.Fd())

	actualMTU, err := rawfile.GetMTU(actualName)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("get mtu for %q: %w", actualName, err)
	}

	return &TUNDevice{
		file:   file,
		fd:     fd,
		name:   actualName,
		mtu:    actualMTU,
		closed: make(chan struct{}),
	}, nil
}

// Name returns the interface name of the TUN device.
func (d *TUNDevice) Name() string {
	if d == nil {
		return ""
	}
	return d.name
}

// File returns the os.File for the TUN device.
// The caller must not close this file directly; use Close() instead.
func (d *TUNDevice) File() *os.File {
	if d == nil {
		return nil
	}
	return d.file
}

// FD returns the raw file descriptor for the TUN device.
// This is useful for gVisor fdbased endpoint integration.
func (d *TUNDevice) FD() int {
	if d == nil {
		return -1
	}
	return d.fd
}

// MTU returns the maximum transmission unit of the TUN device.
func (d *TUNDevice) MTU() uint32 {
	if d == nil {
		return 0
	}
	return d.mtu
}

// Close closes the TUN device. It is safe to call multiple times.
// Subsequent calls return nil.
func (d *TUNDevice) Close() error {
	if d == nil {
		return nil
	}
	var closeErr error
	d.once.Do(func() {
		if d.file != nil {
			closeErr = d.file.Close()
		}
		close(d.closed)
	})
	return closeErr
}

// Closed returns a channel that is closed when the device is closed.
func (d *TUNDevice) Closed() <-chan struct{} {
	if d == nil {
		c := make(chan struct{})
		close(c)
		return c
	}
	return d.closed
}

// createTUNDevice opens the TUN clone device and configures it.
func createTUNDevice(name string) (*os.File, string, error) {
	fd, err := unix.Open(tunCloneDevice, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open %s: %w", tunCloneDevice, err)
	}

	var ifr [tunIfReqSize]byte
	if len(name) > 0 {
		copy(ifr[:], name)
	}
	*(*uint16)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])) = unix.IFF_TUN | unix.IFF_NO_PI

	if _, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.TUNSETIFF),
		uintptr(unsafe.Pointer(&ifr[0])),
	); errno != 0 {
		unix.Close(fd)
		return nil, "", fmt.Errorf("TUNSETIFF %q: %w", name, errno)
	}

	file := os.NewFile(uintptr(fd), tunCloneDevice)

	ifName := unix.ByteSliceToString(ifr[:unix.IFNAMSIZ])
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = file.Close()
		return nil, "", fmt.Errorf("set nonblock: %w", err)
	}

	return file, ifName, nil
}

// isDeviceInUse checks if the error indicates the device name is already in use.
func isDeviceInUse(err error) bool {
	if err == nil {
		return false
	}
	var errno unix.Errno
	if errors.As(err, &errno) {
		return errno == unix.EBUSY || errno == unix.EEXIST
	}
	return false
}
