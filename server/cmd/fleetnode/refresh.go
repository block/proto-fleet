package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/block/proto-fleet/server/internal/fleetnodebootstrap"
)

type RefreshCmd struct{}

func (r *RefreshCmd) Run(c *Context) error {
	return r.run(c, os.Stdin, os.Stdout, os.Stderr)
}

func (r *RefreshCmd) run(c *Context, stdin io.Reader, stdout, stderr io.Writer) error {
	return fleetnodebootstrap.WithStateLock(c.StateDir, func() error {
		return r.runLocked(c, stdin, stdout, stderr)
	})
}

func (r *RefreshCmd) runLocked(c *Context, stdin io.Reader, stdout, stderr io.Writer) error {
	path := fleetnodebootstrap.StatePath(c.StateDir)
	st, exists, err := fleetnodebootstrap.LoadState(path)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("no state at %s; run `fleetnode enroll` first", path)
	}
	if st.ServerURL == "" {
		return errors.New("state has no server_url; re-enroll the fleet node")
	}

	if st.APIKey == "" {
		secrets := newSecretReader(stdin, stderr)
		apiKey, err := secrets.read("Paste the api_key issued for this fleet node:\n> ")
		if err != nil {
			return fmt.Errorf("read api_key: %w", err)
		}
		if apiKey == "" {
			return errors.New("empty api_key; re-enroll the fleet node")
		}
		st.APIKey = apiKey
		// Persist before the handshake so a transient handshake error does
		// not force the operator to re-paste the key on retry.
		if err := fleetnodebootstrap.SaveState(path, st); err != nil {
			return err
		}
	}

	if err := fleetnodebootstrap.Refresh(context.Background(), st); err != nil {
		if errors.Is(err, fleetnodebootstrap.ErrBeginAuthRejected) {
			return fmt.Errorf("server rejected BeginAuthHandshake: %w\n  the server returns Unauthenticated for any of: revoked api_key, identity_pubkey mismatch, expired challenge, or server clock drift. Local credentials are preserved; retry once the operator-side cause is resolved", err)
		}
		return fmt.Errorf("refresh: %w", err)
	}
	if err := fleetnodebootstrap.SaveState(path, st); err != nil {
		return err
	}
	if !st.SessionExpiresAt.IsZero() {
		_, _ = fmt.Fprintf(stdout, "refreshed session_expires_at=%s\n", st.SessionExpiresAt.Format(time.RFC3339))
	} else {
		_, _ = fmt.Fprintln(stdout, "refreshed (server returned no expiry)")
	}
	return nil
}
