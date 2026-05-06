package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1"
)

type EnrollCmd struct {
	ServerURL string `required:"" help:"base URL of the fleet server, e.g. https://fleet.example.com"`
	Code      string `required:"" help:"one-time enrollment code from the operator UI"`
	Name      string `help:"agent name; defaults to os.Hostname() when empty"`
	Force     bool   `help:"overwrite an existing populated state file"`
}

func (e *EnrollCmd) Run(c *Context) error {
	return e.run(c, os.Stdin, os.Stdout, os.Stderr)
}

func (e *EnrollCmd) run(c *Context, stdin io.Reader, stdout, stderr io.Writer) error {
	path := statePath(c.StateDir)
	existing, exists, err := loadState(path)
	if err != nil {
		return err
	}
	if exists && existing.AgentID != 0 && !e.Force {
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

	client := newGatewayClient(e.ServerURL)
	resp, err := client.Register(context.Background(), connect.NewRequest(&pb.RegisterRequest{
		EnrollmentToken:    e.Code,
		Name:               name,
		IdentityPubkey:     idPub,
		MinerSigningPubkey: mPub,
	}))
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	localFP := identityFingerprint(idPub)
	if got := resp.Msg.GetIdentityFingerprint(); got != localFP {
		return fmt.Errorf("server fingerprint %q does not match local %q", got, localFP)
	}

	state := &State{
		ServerURL:                 e.ServerURL,
		AgentID:                   resp.Msg.GetAgentId(),
		IdentityFingerprint:       localFP,
		IdentityPrivateKeyHex:     hex.EncodeToString(idPriv),
		IdentityPublicKeyHex:      hex.EncodeToString(idPub),
		MinerSigningPrivateKeyHex: hex.EncodeToString(mPriv),
		MinerSigningPublicKeyHex:  hex.EncodeToString(mPub),
	}

	_, _ = fmt.Fprintf(stderr, "Agent registered (agent_id=%d, name=%q).\n", state.AgentID, name)
	_, _ = fmt.Fprintf(stderr, "Identity fingerprint: %s\n", localFP)
	_, _ = fmt.Fprintf(stderr, "Compare this fingerprint against the value shown in the operator UI.\n")
	_, _ = fmt.Fprintf(stderr, "Once you confirm enrollment, the UI will display an api_key. Paste it here:\n> ")

	apiKey, err := readAPIKey(stdin)
	if err != nil {
		return err
	}
	if apiKey == "" {
		return errors.New("empty api key")
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

func readAPIKey(r io.Reader) (string, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 1024), 1024*1024)
	if !s.Scan() {
		if err := s.Err(); err != nil {
			return "", fmt.Errorf("scan stdin: %w", err)
		}
		return "", errors.New("no input on stdin")
	}
	return strings.TrimSpace(s.Text()), nil
}
