package deployment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// This token coordinates a one-shot startup; it is not a credential.
const controlledStartToken = "/run/proto-fleet-ha/controlled-start/passive" // #nosec G101 -- one-shot startup coordination path, not a credential.

func clearControlledStartToken(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear controlled passive start: %w", err)
	}
	return nil
}

// Only a stopped local application in passive mode may skip the cooldown, and
// only after observing a serving peer. Recovery and already-running applications
// keep the ordinary restart behavior without probing the peer.
func startApplicationWithCooldown(ctx context.Context, tokenPath string, requirePassive bool, localStatusErr error, observePeer func() fleetHostStatus, start func() error) error {
	skipCooldown := false
	if requirePassive && errors.Is(localStatusErr, syscall.ECONNREFUSED) {
		peer := observePeer()
		skipCooldown = peer.reachable && peer.active && peer.version != ""
	}
	return controlledPassiveStart(ctx, tokenPath, skipCooldown, start)
}

// The container consumes this token before exec. Its on-failure restart uses
// the same command with no token, so every later crash keeps the lease cooldown.
// Failed Compose starts and the normal systemd entrypoint also clear the token.
func controlledPassiveStart(ctx context.Context, tokenPath string, skipCooldown bool, start func() error) (result error) {
	if err := clearControlledStartToken(tokenPath); err != nil {
		return err
	}
	if !skipCooldown {
		return start()
	}
	directory := filepath.Dir(tokenPath)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return fmt.Errorf("create controlled start directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect controlled start directory: %w", err)
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&0022 != 0 || !ok || owner.Uid != uint32(os.Geteuid()) { //nolint:gosec // Effective Unix UIDs are non-negative.
		return errors.New("controlled start directory must be trusted and not writable by group or others")
	}
	token, err := os.OpenFile(tokenPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create controlled passive start token: %w", err)
	}
	if err := token.Close(); err != nil {
		_ = clearControlledStartToken(tokenPath)
		return fmt.Errorf("close controlled passive start token: %w", err)
	}
	defer func() { result = errors.Join(result, clearControlledStartToken(tokenPath)) }()
	if err := start(); err != nil {
		return err
	}
	return waitForControlledStart(ctx, tokenPath)
}

func waitForControlledStart(ctx context.Context, tokenPath string) error {
	ackCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, err := os.Lstat(tokenPath)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect controlled start acknowledgement: %w", err)
		}
		select {
		case <-ackCtx.Done():
			return fmt.Errorf("container did not consume controlled passive start token: %w", ackCtx.Err())
		case <-ticker.C:
		}
	}
}
