package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type RefreshCmd struct {
	APIKey string `name:"api-key" env:"FLEET_AGENT_API_KEY" help:"api_key to use for the handshake; required when state has no api_key (e.g. recovering from an interrupted enroll), otherwise overrides the stored value"`
}

func (r *RefreshCmd) Run(c *Context) error {
	return r.run(c, os.Stdout)
}

func (r *RefreshCmd) run(c *Context, w io.Writer) error {
	return withStateLock(c.StateDir, func() error {
		return r.runLocked(c, w)
	})
}

func (r *RefreshCmd) runLocked(c *Context, w io.Writer) error {
	path := statePath(c.StateDir)
	st, exists, err := loadState(path)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("no state at %s; run `fleet-agent enroll` first", path)
	}
	if st.ServerURL == "" {
		return errors.New("state has no server_url; re-enroll the agent")
	}
	if err := validateServerURL(st.ServerURL, st.AllowInsecureTransport); err != nil {
		return err
	}

	storedKey := st.APIKey
	overrideKey := strings.TrimSpace(r.APIKey)
	overrideUsed := overrideKey != ""

	attemptedKey := storedKey
	if overrideUsed {
		attemptedKey = overrideKey
	}
	if attemptedKey == "" {
		return errors.New("state has no api_key; pass --api-key=<value> or re-enroll the agent")
	}
	st.APIKey = attemptedKey

	if err := runHandshake(context.Background(), newGatewayClient(st.ServerURL), st); err != nil {
		// Unauthenticated is ambiguous (revocation, identity mismatch,
		// expired challenge, bad signature). Preserve local state on
		// every failure; restore in-memory api_key so a future defensive
		// saveState cannot persist a rejected override.
		st.APIKey = storedKey
		if overrideUsed && overrideKey != storedKey && errors.Is(err, errAPIKeyRejected) {
			return fmt.Errorf("api_key override rejected; stored credentials preserved, retry without --api-key: %w", err)
		}
		return err
	}
	if err := saveState(path, st); err != nil {
		return err
	}
	if !st.SessionExpiresAt.IsZero() {
		_, _ = fmt.Fprintf(w, "refreshed session_expires_at=%s\n", st.SessionExpiresAt.Format(time.RFC3339))
	} else {
		_, _ = fmt.Fprintln(w, "refreshed (server returned no expiry)")
	}
	return nil
}
