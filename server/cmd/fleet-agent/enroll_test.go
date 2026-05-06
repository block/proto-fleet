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

func TestEnrollCmd_HappyPath_ApiKeyFlag(t *testing.T) {
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
		Code:                   fake.expectedCode,
		Name:                   "test-agent",
		APIKey:                 fake.expectedAPIKey,
		AllowInsecureTransport: true,
	}
	var stdout, stderr bytes.Buffer

	// Act
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader(""), &stdout, &stderr)

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
	assert.True(t, fake.registered)
	assert.True(t, fake.signatureVerified)
}

func TestEnrollCmd_PersistsStateBeforeHandshake(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	fake := &fakeAgentGateway{
		expectedCode:   "code",
		expectedAPIKey: "wrong-api-key-so-handshake-fails",
		agentID:        99,
		challenge:      bytes.Repeat([]byte{0x01}, 32),
	}
	srv := newFakeServer(t, fake)
	cmd := &EnrollCmd{
		ServerURL:              srv.URL,
		Code:                   "code",
		Name:                   "agent-99",
		APIKey:                 "intentionally-wrong-key",
		AllowInsecureTransport: true,
	}

	// Act
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	loaded, exists, err := loadState(filepath.Join(dir, "state.yaml"))
	require.NoError(t, err)
	require.True(t, exists, "state must persist after Register so the operator can recover via refresh")
	assert.Equal(t, int64(99), loaded.AgentID)
	assert.Equal(t, "intentionally-wrong-key", loaded.APIKey)
	assert.Empty(t, loaded.SessionToken)
}

func TestEnrollCmd_RefusesPlainHTTPForRemoteHost(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	cmd := &EnrollCmd{
		ServerURL: "http://fleet.example.com",
		Code:      "code",
		APIKey:    "k",
	}

	// Act
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
}

func TestEnrollCmd_AllowsLoopbackHTTP(t *testing.T) {
	t.Parallel()

	cases := []string{
		"http://localhost:4000",
		"http://127.0.0.1:4000",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			// Act
			err := validateServerURL(raw, false)

			// Assert
			require.NoError(t, err)
		})
	}
}

func TestValidateServerURL_AllowInsecureFlag(t *testing.T) {
	t.Parallel()

	// Act
	err := validateServerURL("http://fleet.example.com", true)

	// Assert
	require.NoError(t, err)
}
