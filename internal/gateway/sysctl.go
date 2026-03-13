//go:build linux

package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const rpFilterProcBase = "/proc/sys/net/ipv4/conf"

// SysctlManager saves and restores kernel sysctl values modified by the agent.
type SysctlManager struct {
	saved []savedSysctl
}

type savedSysctl struct {
	path     string
	original string
}

// DisableRPFilter sets rp_filter=0 on the named TUN interface and on "all".
// Strict mode (default on RHEL/OL) drops transparent proxy responses whose
// source IP is not routable via the TUN. Original values are saved for Restore.
func (s *SysctlManager) DisableRPFilter(tunName string) error {
	targets := []string{tunName, "all"}
	for _, iface := range targets {
		path := filepath.Join(rpFilterProcBase, iface, "rp_filter")
		if err := s.set(path, "0"); err != nil {
			return fmt.Errorf("disable rp_filter for %s: %w", iface, err)
		}
	}
	return nil
}

// Restore resets every modified sysctl back to its original value, in
// reverse order. Errors are collected but do not stop remaining restores.
func (s *SysctlManager) Restore() error {
	var errs []string
	for i := len(s.saved) - 1; i >= 0; i-- {
		entry := s.saved[i]
		if err := os.WriteFile(entry.path, []byte(entry.original), 0644); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", entry.path, err))
		}
	}
	s.saved = nil
	if len(errs) > 0 {
		return fmt.Errorf("restore sysctl: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *SysctlManager) set(path, value string) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	s.saved = append(s.saved, savedSysctl{
		path:     path,
		original: string(original),
	})

	if err := os.WriteFile(path, []byte(value+"\n"), 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
