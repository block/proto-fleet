package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
)

type EnrollCmd struct {
	ServerURL              string `required:"" help:"base URL of the fleet server, e.g. https://fleet.example.com"`
	Name                   string `help:"fleet node name; defaults to os.Hostname() when empty"`
	Force                  bool   `help:"overwrite an existing populated state file"`
	AllowInsecureTransport bool   `name:"allow-insecure-transport" help:"permit non-https server URLs for non-loopback hosts; testing only"`
}

func (e *EnrollCmd) Run(c *Context) error {
	return e.run(c, os.Stdin, os.Stdout, os.Stderr)
}

func (e *EnrollCmd) run(c *Context, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := bootstrap.ValidateServerURL(e.ServerURL, e.AllowInsecureTransport); err != nil {
		return err
	}
	return bootstrap.WithStateLock(c.StateDir, func() error {
		return e.runLocked(c, stdin, stdout, stderr)
	})
}

func (e *EnrollCmd) runLocked(c *Context, stdin io.Reader, stdout, stderr io.Writer) error {
	path := bootstrap.StatePath(c.StateDir)
	st, exists, err := bootstrap.LoadState(path)
	if err != nil {
		return err
	}
	if exists && st.FleetNodeID != 0 && !e.Force {
		return fmt.Errorf("state already populated at %s; pass --force to overwrite", path)
	}

	name := e.Name
	if name == "" {
		host, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("resolve hostname: %w", err)
		}
		name = host
	}

	if exists && st.FleetNodeID != 0 && e.Force {
		_, _ = fmt.Fprintf(stderr, "warning: --force discarded local state for fleet_node_id=%d. If %q is still registered server-side, Register will fail; revoke the prior fleet node in the operator UI first or pass --name=<unique-value>.\n", st.FleetNodeID, name)
	}

	secrets := newSecretReader(stdin, stderr)
	code, err := secrets.read("Paste the one-time enrollment code from the operator UI:\n> ")
	if err != nil {
		return fmt.Errorf("read enrollment code: %w", err)
	}
	if code == "" {
		return errors.New("empty enrollment code")
	}

	result, err := bootstrap.Register(context.Background(), bootstrap.RegisterParams{
		ServerURL:              e.ServerURL,
		Name:                   name,
		Code:                   code,
		AllowInsecureTransport: e.AllowInsecureTransport,
	})
	if err != nil {
		if errors.Is(err, bootstrap.ErrRegisterRejected) {
			return fmt.Errorf("register rejected by server: %w\n  recovery: revoke the prior fleet node in the operator UI (then re-run with --force), or pass --name=<unique-value> to register as a new fleet node. If the enrollment code was already used, typo'd, or expired, request a fresh one from the operator UI", err)
		}
		return fmt.Errorf("register: %w", err)
	}
	state := result.State

	// Persist before the api_key prompt so a Ctrl-C cannot orphan the
	// server-side fleet_node row; the operator can complete the enrollment
	// by running `fleetnode refresh` and entering the api_key when prompted.
	if err := bootstrap.SaveState(path, state); err != nil {
		return fmt.Errorf(
			"save state after Register: %w\n"+
				"  recovery: a fleet_node row was created server-side "+
				"(fleet_node_id=%d, name=%q) but no local credentials were persisted. "+
				"Revoke that fleet node in the operator UI before retrying "+
				"`fleetnode enroll`, or pass `--name=<unique-value>` to register as a new fleet node",
			err, state.FleetNodeID, name,
		)
	}

	_, _ = fmt.Fprintf(stderr, "Fleet node registered (fleet_node_id=%d, name=%q).\n", state.FleetNodeID, name)
	_, _ = fmt.Fprintf(stderr, "Identity fingerprint: %s\n", state.IdentityFingerprint)
	_, _ = fmt.Fprintf(stderr, "Compare this fingerprint against the value shown in the operator UI before pasting the api_key.\n")

	apiKey, err := secrets.read("Once you confirm enrollment, the UI will display an api_key. Paste it here:\n> ")
	if err != nil {
		return fmt.Errorf("read api_key: %w", err)
	}
	if apiKey == "" {
		return errors.New("empty api_key")
	}

	if err := bootstrap.CompleteEnrollment(context.Background(), state, apiKey); err != nil {
		if errors.Is(err, bootstrap.ErrBeginAuthRejected) {
			return fmt.Errorf("%w. The server returns Unauthenticated for any of: revoked api_key, identity_pubkey mismatch, expired challenge, or server clock drift. Verify the api_key matches the one minted in the UI, then retry with `fleetnode refresh`; local credentials are preserved", bootstrap.ErrBeginAuthRejected)
		}
		return fmt.Errorf("complete enrollment: %w", err)
	}
	if err := bootstrap.SaveState(path, state); err != nil {
		return fmt.Errorf(
			"save state after CompleteEnrollment: %w\n"+
				"  recovery: enrollment succeeded server-side "+
				"(fleet_node_id=%d, name=%q) and an api_key was issued, "+
				"but no local credentials were persisted. Revoke that fleet node "+
				"in the operator UI and re-enroll; the issued api_key cannot be recovered",
			err, state.FleetNodeID, name,
		)
	}
	_, _ = fmt.Fprintf(stdout, "enrolled fleet_node_id=%d fingerprint=%s state=%s\n", state.FleetNodeID, state.IdentityFingerprint, path)
	return nil
}
