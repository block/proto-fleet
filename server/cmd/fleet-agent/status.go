package main

import (
	"fmt"
	"io"
	"os"
	"time"
)

type StatusCmd struct{}

func (s *StatusCmd) Run(c *Context) error {
	return s.run(c, os.Stdout)
}

func (s *StatusCmd) run(c *Context, w io.Writer) error {
	path := statePath(c.StateDir)
	st, exists, err := loadState(path)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("no state at %s; run `fleet-agent enroll` first", path)
	}
	_, _ = fmt.Fprintf(w, "state_path:            %s\n", path)
	_, _ = fmt.Fprintf(w, "server_url:            %s\n", st.ServerURL)
	_, _ = fmt.Fprintf(w, "agent_id:              %d\n", st.AgentID)
	_, _ = fmt.Fprintf(w, "identity_fingerprint:  %s\n", st.IdentityFingerprint)
	_, _ = fmt.Fprintf(w, "api_key_present:       %t\n", st.APIKey != "")
	_, _ = fmt.Fprintf(w, "session_token_present: %t\n", st.SessionToken != "")
	if !st.SessionExpiresAt.IsZero() {
		remaining := time.Until(st.SessionExpiresAt).Round(time.Second)
		_, _ = fmt.Fprintf(w, "session_expires_at:    %s (in %s)\n", st.SessionExpiresAt.Format(time.RFC3339), remaining)
	}
	return nil
}
