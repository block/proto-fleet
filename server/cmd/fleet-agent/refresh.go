package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

type RefreshCmd struct{}

func (r *RefreshCmd) Run(c *Context) error {
	return r.run(c, os.Stdin, os.Stdout, os.Stderr)
}

func (r *RefreshCmd) run(c *Context, stdin io.Reader, stdout, stderr io.Writer) error {
	return withStateLock(c.StateDir, func() error {
		return r.runLocked(c, stdin, stdout, stderr)
	})
}

func (r *RefreshCmd) runLocked(c *Context, stdin io.Reader, stdout, stderr io.Writer) error {
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

	if st.APIKey == "" {
		secrets := newSecretReader(stdin, stderr)
		apiKey, err := secrets.read("Paste the api_key issued for this agent:\n> ")
		if err != nil {
			return fmt.Errorf("read api_key: %w", err)
		}
		if apiKey == "" {
			return errors.New("empty api_key; re-enroll the agent")
		}
		st.APIKey = apiKey
		// Persist before the handshake so a transient handshake error
		// does not force the operator to re-paste the key on retry.
		if err := saveState(path, st); err != nil {
			return err
		}
	}

	if err := runHandshake(context.Background(), newGatewayClient(st.ServerURL), st); err != nil {
		return err
	}
	if err := saveState(path, st); err != nil {
		return err
	}
	if !st.SessionExpiresAt.IsZero() {
		_, _ = fmt.Fprintf(stdout, "refreshed session_expires_at=%s\n", st.SessionExpiresAt.Format(time.RFC3339))
	} else {
		_, _ = fmt.Fprintln(stdout, "refreshed (server returned no expiry)")
	}
	return nil
}
