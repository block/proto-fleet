package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	"connectrpc.com/connect"
	"golang.org/x/term"

	pb "github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1"
)

type EnrollCmd struct {
	ServerURL              string `required:"" help:"base URL of the fleet server, e.g. https://fleet.example.com"`
	Name                   string `help:"agent name; defaults to os.Hostname() when empty"`
	Force                  bool   `help:"overwrite an existing populated state file"`
	AllowInsecureTransport bool   `name:"allow-insecure-transport" help:"permit non-https server URLs for non-loopback hosts; testing only"`
}

func (e *EnrollCmd) Run(c *Context) error {
	return e.run(c, os.Stdin, os.Stdout, os.Stderr)
}

func (e *EnrollCmd) run(c *Context, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := validateServerURL(e.ServerURL, e.AllowInsecureTransport); err != nil {
		return err
	}
	return withStateLock(c.StateDir, func() error {
		return e.runLocked(c, stdin, stdout, stderr)
	})
}

func (e *EnrollCmd) runLocked(c *Context, stdin io.Reader, stdout, stderr io.Writer) error {
	path := statePath(c.StateDir)
	st, exists, err := loadState(path)
	if err != nil {
		return err
	}
	if exists && st.AgentID != 0 && !e.Force {
		return fmt.Errorf("state already populated at %s; pass --force to overwrite", path)
	}

	idPub, idPriv, err := generateKeypair()
	if err != nil {
		return err
	}
	mPub, mPriv, err := generateKeypair()
	if err != nil {
		return err
	}

	name := e.Name
	if name == "" {
		host, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("resolve hostname: %w", err)
		}
		name = host
	}

	if exists && st.AgentID != 0 && e.Force {
		_, _ = fmt.Fprintf(stderr, "warning: --force discarded local state for agent_id=%d. If %q is still registered server-side, Register will fail; revoke the prior agent in the operator UI first or pass --name=<unique-value>.\n", st.AgentID, name)
	}

	secrets := newSecretReader(stdin, stderr)
	code, err := secrets.read("Paste the one-time enrollment code from the operator UI:\n> ")
	if err != nil {
		return fmt.Errorf("read enrollment code: %w", err)
	}
	if code == "" {
		return errors.New("empty enrollment code")
	}

	client := newGatewayClient(e.ServerURL)
	resp, err := client.Register(context.Background(), connect.NewRequest(&pb.RegisterRequest{
		EnrollmentToken:    code,
		Name:               name,
		IdentityPubkey:     idPub,
		MinerSigningPubkey: mPub,
	}))
	if err != nil {
		code := connect.CodeOf(err)
		if code == connect.CodeAlreadyExists || code == connect.CodeFailedPrecondition {
			return fmt.Errorf("register rejected by server: %w\n  recovery: revoke the prior agent in the operator UI (then re-run with --force), or pass --name=<unique-value> to register as a new agent. If the enrollment code was already used or expired, request a fresh one from the operator UI", err)
		}
		return fmt.Errorf("register: %w", err)
	}
	localFP := identityFingerprint(idPub)
	if got := resp.Msg.GetIdentityFingerprint(); got != localFP {
		return fmt.Errorf("server fingerprint %q does not match local %q", got, localFP)
	}

	state := &State{
		ServerURL:                 e.ServerURL,
		AllowInsecureTransport:    e.AllowInsecureTransport,
		AgentID:                   resp.Msg.GetAgentId(),
		IdentityFingerprint:       localFP,
		IdentityPrivateKeyHex:     hex.EncodeToString(idPriv),
		IdentityPublicKeyHex:      hex.EncodeToString(idPub),
		MinerSigningPrivateKeyHex: hex.EncodeToString(mPriv),
		MinerSigningPublicKeyHex:  hex.EncodeToString(mPub),
	}

	// Persist before the api_key prompt so a Ctrl-C cannot orphan the
	// server-side agent row; the operator can complete the enrollment by
	// running `fleet-agent refresh` and entering the api_key when prompted.
	if err := saveState(path, state); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stderr, "Agent registered (agent_id=%d, name=%q).\n", state.AgentID, name)
	_, _ = fmt.Fprintf(stderr, "Identity fingerprint: %s\n", localFP)
	_, _ = fmt.Fprintf(stderr, "Compare this fingerprint against the value shown in the operator UI.\n")

	apiKey, err := secrets.read("Once you confirm enrollment, the UI will display an api_key. Paste it here:\n> ")
	if err != nil {
		return fmt.Errorf("read api_key: %w", err)
	}
	if apiKey == "" {
		return errors.New("empty api_key")
	}
	state.APIKey = apiKey
	if err := runHandshake(context.Background(), client, state); err != nil {
		return err
	}
	if err := saveState(path, state); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "enrolled agent_id=%d fingerprint=%s state=%s\n", state.AgentID, localFP, path)
	return nil
}

func validateServerURL(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse server-url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("server-url scheme must be http or https; got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("server-url has no host")
	}
	if u.Scheme == "https" {
		return nil
	}
	if isLoopbackHost(u.Hostname()) {
		return nil
	}
	if allowInsecure {
		return nil
	}
	return fmt.Errorf("server-url must use https for non-loopback hosts; pass --allow-insecure-transport to override (testing only)")
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}

// secretReader serializes one or more secret prompts off a single stdin.
// On a TTY the prompt is printed and term.ReadPassword reads without echo.
// For piped/scripted input, a shared bufio.Scanner reads one line per
// prompt, so callers can feed multiple secrets via one stdin stream
// (e.g. `printf '%s\n%s\n' "$CODE" "$KEY" | fleet-agent enroll ...`).
type secretReader struct {
	stdin   io.Reader
	stderr  io.Writer
	scanner *bufio.Scanner
	tty     *os.File
}

func newSecretReader(stdin io.Reader, stderr io.Writer) *secretReader {
	sr := &secretReader{stdin: stdin, stderr: stderr}
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		sr.tty = f
	}
	return sr
}

func (sr *secretReader) read(prompt string) (string, error) {
	_, _ = fmt.Fprint(sr.stderr, prompt)
	if sr.tty != nil {
		b, err := term.ReadPassword(int(sr.tty.Fd()))
		_, _ = fmt.Fprintln(sr.stderr)
		if err != nil {
			return "", fmt.Errorf("read from terminal: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	if sr.scanner == nil {
		sr.scanner = bufio.NewScanner(sr.stdin)
		sr.scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	}
	if !sr.scanner.Scan() {
		if err := sr.scanner.Err(); err != nil {
			return "", fmt.Errorf("scan stdin: %w", err)
		}
		return "", errors.New("no input on stdin")
	}
	return strings.TrimSpace(sr.scanner.Text()), nil
}
