//go:build !linux

package main

import (
	"context"
	"fmt"
)

func agentCmd(ctx context.Context, args []string) error {
	return fmt.Errorf("agent mode is only supported on Linux")
}
