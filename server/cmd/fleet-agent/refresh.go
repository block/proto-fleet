package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"connectrpc.com/connect"
)

type RefreshCmd struct{}

func (r *RefreshCmd) Run(c *Context) error {
	path := statePath(c.StateDir)
	st, exists, err := loadState(path)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("no state at %s; run `fleet-agent enroll` first", path)
	}
	if st.APIKey == "" {
		return errors.New("state has no api_key; re-enroll the agent")
	}
	if st.ServerURL == "" {
		return errors.New("state has no server_url; re-enroll the agent")
	}
	client := newGatewayClient(st.ServerURL)
	if err := runHandshake(context.Background(), client, st); err != nil {
		if connect.CodeOf(err) == connect.CodeUnauthenticated {
			st.APIKey = ""
			st.SessionToken = ""
			st.SessionExpiresAt = time.Time{}
			if saveErr := saveState(path, st); saveErr != nil {
				return fmt.Errorf("handshake unauthenticated; failed to clear local state: %w", saveErr)
			}
			return fmt.Errorf("api_key rejected; cleared local credentials, re-enroll the agent: %w", err)
		}
		return err
	}
	if err := saveState(path, st); err != nil {
		return err
	}
	if !st.SessionExpiresAt.IsZero() {
		_, _ = fmt.Fprintf(os.Stdout, "refreshed session_expires_at=%s\n", st.SessionExpiresAt.Format(time.RFC3339))
	} else {
		_, _ = fmt.Fprintln(os.Stdout, "refreshed (server returned no expiry)")
	}
	return nil
}
