//go:build !linux

package daemon

import "fmt"

const EnvDaemonChild = "SLUICE_DAEMON_CHILD"

type Config struct {
	PIDFile string
	LogFile string
	Args    []string
}

func (c Config) Validate() error {
	return fmt.Errorf("daemon: not supported on this platform")
}

func Daemonize(_ Config) error {
	return fmt.Errorf("daemon: not supported on this platform")
}

func IsChild() bool {
	return false
}

func IsSystemd() bool {
	return false
}

func StopProcess(_ string) error {
	return fmt.Errorf("daemon: not supported on this platform")
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

func WritePID(_ string, _ int) error {
	return fmt.Errorf("daemon: not supported on this platform")
}

func ReadPID(_ string) (int, error) {
	return 0, fmt.Errorf("daemon: not supported on this platform")
}

func RemovePID(_ string) error {
	return nil
}

func IsRunning(_ int) bool {
	return false
}

func FindProcess(_ string) (int, error) {
	return 0, fmt.Errorf("daemon: not supported on this platform")
}
