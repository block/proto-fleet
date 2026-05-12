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
	// LoadState is pure; check existence before taking the state lock so
	// `fleetnode refresh` on a never-enrolled host does not create
	// ~/.local/state/fleetnode/state.lock as a side effect.
	path := fleetnodebootstrap.StatePath(c.StateDir)
	if _, exists, err := fleetnodebootstrap.LoadState(path); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("no state at %s; run `fleetnode enroll` first", path)
	}
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
		// Hold the pasted key in memory only until Refresh succeeds. If the
		// server rejects it, persisting would leave a bad key on disk and
		// suppress the next prompt — the operator would be stuck without a
		// CLI path to replace it.
		st.APIKey = apiKey
	}

	if err := fleetnodebootstrap.Refresh(context.Background(), st); err != nil {
		if errors.Is(err, fleetnodebootstrap.ErrBeginAuthRejected) {
			return fmt.Errorf("%w. The server returns Unauthenticated for any of: revoked api_key, identity_pubkey mismatch, expired challenge, or server clock drift. Local credentials are preserved; verify the api_key matches the one minted in the UI and retry", fleetnodebootstrap.ErrBeginAuthRejected)
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
