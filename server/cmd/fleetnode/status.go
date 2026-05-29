package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
)

type StatusCmd struct{}

func (s *StatusCmd) Run(c *Context) error {
	return s.run(c, os.Stdout)
}

func (s *StatusCmd) run(c *Context, w io.Writer) error {
	path := bootstrap.StatePath(c.StateDir)
	st, exists, err := bootstrap.LoadState(path)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("no state at %s; run `fleetnode enroll` first", path)
	}
	_, _ = fmt.Fprintf(w, "state_path:            %s\n", path)
	_, _ = fmt.Fprintf(w, "server_url:            %s\n", st.ServerURL)
	_, _ = fmt.Fprintf(w, "fleet_node_id:         %d\n", st.FleetNodeID)
	_, _ = fmt.Fprintf(w, "identity_fingerprint:  %s\n", st.IdentityFingerprint)
	_, _ = fmt.Fprintf(w, "api_key_present:       %t\n", st.APIKey != "")
	_, _ = fmt.Fprintf(w, "session_token_present: %t\n", st.SessionToken != "")
	if !st.SessionExpiresAt.IsZero() {
		remaining := time.Until(st.SessionExpiresAt).Round(time.Second)
		_, _ = fmt.Fprintf(w, "session_expires_at:    %s (in %s)\n", st.SessionExpiresAt.Format(time.RFC3339), remaining)
	}
	return nil
}
