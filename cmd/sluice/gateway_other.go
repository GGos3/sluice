//go:build !linux

package main

import (
	"context"
	"fmt"
)

func runGateway(ctx context.Context, args []string) error {
	return fmt.Errorf("gateway mode is only supported on Linux")
}
