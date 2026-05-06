package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
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
		return err
	}
	if err := saveState(path, st); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "refreshed session_expires_at=%s\n", st.SessionExpiresAt.Format(time.RFC3339))
	return nil
}
