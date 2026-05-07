package main

import (
	"bytes"
	"crypto/ed25519"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnrollCmd_HappyPath(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	expiresAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	fake := &fakeAgentGateway{
		expectedCode:     "enroll-code-xyz",
		expectedAPIKey:   "fleet_aabbccdd_zzz",
		agentID:          77,
		challenge:        bytes.Repeat([]byte{0x42}, 32),
		sessionToken:     "session-after-enroll",
		sessionExpiresAt: expiresAt,
	}
	srv := newFakeServer(t, fake)
	cmd := &EnrollCmd{
		ServerURL:              srv.URL,
		Name:                   "test-agent",
		AllowInsecureTransport: true,
	}
	stdin := strings.NewReader(fake.expectedCode + "\n" + fake.expectedAPIKey + "\n")
	var stdout, stderr bytes.Buffer

	// Act
	err := cmd.run(&Context{StateDir: dir}, stdin, &stdout, &stderr)

	// Assert
	require.NoError(t, err)
	loaded, exists, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, int64(77), loaded.AgentID)
	assert.Contains(t, stderr.String(), `name="test-agent"`)
	assert.Equal(t, fake.expectedAPIKey, loaded.APIKey)
	assert.Equal(t, "session-after-enroll", loaded.SessionToken)
	assert.WithinDuration(t, expiresAt, loaded.SessionExpiresAt, time.Second)
	assert.Len(t, loaded.IdentityFingerprint, 16)
	assert.Equal(t, ed25519.PublicKeySize*2, len(loaded.IdentityPublicKeyHex))
	assert.Equal(t, ed25519.PrivateKeySize*2, len(loaded.IdentityPrivateKeyHex))
	assert.True(t, loaded.AllowInsecureTransport)
	assert.True(t, fake.registered)
	assert.True(t, fake.signatureVerified)
}

func TestEnrollCmd_PersistsStateImmediatelyAfterRegister(t *testing.T) {
	t.Parallel()

	// Arrange: stdin only feeds the enrollment code, then EOFs. The api_key
	// prompt must fail, but state.yaml must already hold the keys + agent_id
	// so the operator can recover via `fleet-agent refresh`.
	dir := t.TempDir()
	fake := &fakeAgentGateway{
		expectedCode: "code",
		agentID:      55,
		challenge:    bytes.Repeat([]byte{0x02}, 32),
	}
	srv := newFakeServer(t, fake)
	cmd := &EnrollCmd{
		ServerURL:              srv.URL,
		Name:                   "agent-55",
		AllowInsecureTransport: true,
	}

	// Act
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader("code\n"), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err, "second prompt has no input; enroll should fail at the api_key read")
	loaded, exists, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	require.True(t, exists, "state must persist immediately after Register so a Ctrl-C during paste does not orphan the agent")
	assert.Equal(t, int64(55), loaded.AgentID)
	assert.Empty(t, loaded.APIKey)
	assert.Empty(t, loaded.SessionToken)
	assert.Equal(t, ed25519.PrivateKeySize*2, len(loaded.IdentityPrivateKeyHex))
	assert.True(t, loaded.AllowInsecureTransport)
}

func TestEnrollCmd_PersistsStateBeforeHandshake(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	const pastedAPIKey = "wrong-key"
	fake := &fakeAgentGateway{
		expectedCode:   "code",
		expectedAPIKey: "right-key",
		agentID:        99,
		challenge:      bytes.Repeat([]byte{0x01}, 32),
	}
	srv := newFakeServer(t, fake)
	cmd := &EnrollCmd{
		ServerURL:              srv.URL,
		Name:                   "agent-99",
		AllowInsecureTransport: true,
	}

	// Act
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader("code\n"+pastedAPIKey+"\n"), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	loaded, exists, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	require.True(t, exists, "state must persist after Register so the operator can recover via refresh")
	assert.Equal(t, int64(99), loaded.AgentID)
	assert.Equal(t, pastedAPIKey, loaded.APIKey)
	assert.Empty(t, loaded.SessionToken)
}

func TestEnrollCmd_RejectsEmptyEnrollmentCode(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	srv := newFakeServer(t, &fakeAgentGateway{})
	cmd := &EnrollCmd{
		ServerURL:              srv.URL,
		Name:                   "agent-empty-code",
		AllowInsecureTransport: true,
	}

	// Act
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader("\n"), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty enrollment code")
	_, exists, _ := loadState(filepath.Join(dir, "state.yaml"))
	assert.False(t, exists, "state must not be created when the enrollment code is empty")
}

func TestValidateServerURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		url           string
		allowInsecure bool
		wantErr       string
	}{
		{name: "https accepted", url: "https://fleet.example.com", allowInsecure: false, wantErr: ""},
		{name: "loopback http localhost", url: "http://localhost:4000", allowInsecure: false, wantErr: ""},
		{name: "loopback http 127.0.0.1", url: "http://127.0.0.1:4000", allowInsecure: false, wantErr: ""},
		{name: "loopback http 127.x.x.x", url: "http://127.5.6.7:4000", allowInsecure: false, wantErr: ""},
		{name: "loopback http ipv6", url: "http://[::1]:4000", allowInsecure: false, wantErr: ""},
		{name: "remote http rejected", url: "http://fleet.example.com", allowInsecure: false, wantErr: "https"},
		{name: "remote http allowed via flag", url: "http://fleet.example.com", allowInsecure: true, wantErr: ""},
		{name: "unknown scheme rejected", url: "ftp://fleet.example.com", allowInsecure: false, wantErr: "scheme"},
		{name: "missing host rejected", url: "https://", allowInsecure: false, wantErr: "host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act
			err := validateServerURL(tc.url, tc.allowInsecure)

			// Assert
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
