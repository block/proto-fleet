package main

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/fleetnodebootstrap"
)

func TestEnrollCmd_HappyPath(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	expiresAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	fake := &fakeFleetNodeGateway{
		expectedCode:     "enroll-code-xyz",
		expectedAPIKey:   "fleet_aabbccdd_zzz",
		fleetNodeID:      77,
		challenge:        bytes.Repeat([]byte{0x42}, 32),
		sessionToken:     "session-after-enroll",
		sessionExpiresAt: expiresAt,
	}
	srv := newFakeServer(t, fake)
	cmd := &EnrollCmd{
		ServerURL:              srv.URL,
		Name:                   "test-node",
		AllowInsecureTransport: true,
	}
	stdin := strings.NewReader(fake.expectedCode + "\n" + fake.expectedAPIKey + "\n")
	var stdout, stderr bytes.Buffer

	// Act
	err := cmd.run(&Context{StateDir: dir}, stdin, &stdout, &stderr)

	// Assert
	require.NoError(t, err)
	loaded, exists, err := fleetnodebootstrap.LoadState(fleetnodebootstrap.StatePath(dir))
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, int64(77), loaded.FleetNodeID)
	assert.Equal(t, fake.expectedAPIKey, loaded.APIKey)
	assert.Equal(t, "session-after-enroll", loaded.SessionToken)
	assert.WithinDuration(t, expiresAt, loaded.SessionExpiresAt, time.Second)
	assert.Len(t, loaded.IdentityFingerprint, 16)
	assert.Equal(t, ed25519.PublicKeySize*2, len(loaded.IdentityPublicKeyHex))
	assert.Equal(t, ed25519.PrivateKeySize*2, len(loaded.IdentityPrivateKeyHex))
	assert.True(t, loaded.AllowInsecureTransport)
	assert.True(t, fake.registered)
	assert.True(t, fake.signatureVerified)
	assert.Contains(t, stdout.String(), "enrolled fleet_node_id=77")
}

func TestEnrollCmd_PersistsStateImmediatelyAfterRegister(t *testing.T) {
	t.Parallel()

	// Arrange: stdin only feeds the enrollment code, then EOFs. The api_key
	// prompt must fail, but state.yaml must already hold the keys +
	// fleet_node_id so the operator can recover via `fleetnode refresh`.
	dir := t.TempDir()
	fake := &fakeFleetNodeGateway{
		expectedCode: "code",
		fleetNodeID:  55,
		challenge:    bytes.Repeat([]byte{0x02}, 32),
	}
	srv := newFakeServer(t, fake)
	cmd := &EnrollCmd{
		ServerURL:              srv.URL,
		Name:                   "node-55",
		AllowInsecureTransport: true,
	}

	// Act
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader("code\n"), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err, "second prompt has no input; enroll should fail at the api_key read")
	loaded, exists, err := fleetnodebootstrap.LoadState(fleetnodebootstrap.StatePath(dir))
	require.NoError(t, err)
	require.True(t, exists, "state must persist immediately after Register so a Ctrl-C during paste does not orphan the fleet node")
	assert.Equal(t, int64(55), loaded.FleetNodeID)
	assert.Empty(t, loaded.APIKey)
	assert.Empty(t, loaded.SessionToken)
	assert.Equal(t, ed25519.PrivateKeySize*2, len(loaded.IdentityPrivateKeyHex))
	assert.True(t, loaded.AllowInsecureTransport)
}

func TestEnrollCmd_PreservesPartialStateOnBeginAuthRejection(t *testing.T) {
	t.Parallel()

	// Arrange: register succeeds, but BeginAuth rejects. Local state must
	// hold keys + fleet_node_id; api_key must not be persisted so the
	// operator can retry `fleetnode refresh` with a different key.
	dir := t.TempDir()
	fake := &fakeFleetNodeGateway{
		expectedCode:   "code",
		expectedAPIKey: "the-real-key",
		fleetNodeID:    99,
		challenge:      bytes.Repeat([]byte{0x05}, 32),
	}
	srv := newFakeServer(t, fake)
	cmd := &EnrollCmd{
		ServerURL:              srv.URL,
		Name:                   "node-99",
		AllowInsecureTransport: true,
	}
	stdin := strings.NewReader("code\nwrong-key\n")
	var stderr bytes.Buffer

	// Act
	err := cmd.run(&Context{StateDir: dir}, stdin, &bytes.Buffer{}, &stderr)

	// Assert
	require.Error(t, err)
	assert.ErrorIs(t, err, fleetnodebootstrap.ErrBeginAuthRejected)
	assert.Contains(t, err.Error(), "revoked api_key, identity_pubkey mismatch")
	assert.NotContains(t, err.Error(), "invalid api_key", "the server-side message must not leak through; the CLI gives generic guidance for all Unauthenticated causes")
	loaded, exists, _ := fleetnodebootstrap.LoadState(fleetnodebootstrap.StatePath(dir))
	require.True(t, exists)
	assert.Equal(t, int64(99), loaded.FleetNodeID)
	assert.Empty(t, loaded.APIKey, "api_key must not persist when CompleteEnrollment failed")
	assert.Empty(t, loaded.SessionToken)
}

func TestEnrollCmd_RejectsEmptyEnrollmentCode(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	srv := newFakeServer(t, &fakeFleetNodeGateway{})
	cmd := &EnrollCmd{
		ServerURL:              srv.URL,
		Name:                   "node-empty-code",
		AllowInsecureTransport: true,
	}

	// Act
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader("\n"), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty enrollment code")
	_, exists, _ := fleetnodebootstrap.LoadState(fleetnodebootstrap.StatePath(dir))
	assert.False(t, exists, "state must not be created when the enrollment code is empty")
}

func TestEnrollCmd_TranslatesRegisterErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		registerErr error
		wantSub     string
		wantNotSub  string
	}{
		{
			name:        "already_exists -> recovery hint",
			registerErr: connect.NewError(connect.CodeAlreadyExists, errors.New("name in use")),
			wantSub:     "revoke the prior fleet node",
		},
		{
			name:        "failed_precondition -> recovery hint",
			registerErr: connect.NewError(connect.CodeFailedPrecondition, errors.New("fleet node identity or name already in use")),
			wantSub:     "revoke the prior fleet node",
		},
		{
			name:        "unauthenticated -> typoed code hint via same wrapper",
			registerErr: connect.NewError(connect.CodeUnauthenticated, errors.New("invalid enrollment code")),
			wantSub:     "fresh one from the operator UI",
		},
		{
			name:        "other code -> generic register: prefix",
			registerErr: connect.NewError(connect.CodeInternal, errors.New("boom")),
			wantSub:     "register:",
			wantNotSub:  "revoke the prior fleet node",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			dir := t.TempDir()
			fake := &fakeFleetNodeGateway{registerError: tc.registerErr}
			srv := newFakeServer(t, fake)
			cmd := &EnrollCmd{
				ServerURL:              srv.URL,
				Name:                   "node-x",
				AllowInsecureTransport: true,
			}

			// Act
			err := cmd.run(&Context{StateDir: dir}, strings.NewReader("any-code\n"), &bytes.Buffer{}, &bytes.Buffer{})

			// Assert
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantSub)
			if tc.wantNotSub != "" {
				assert.NotContains(t, err.Error(), tc.wantNotSub)
			}
		})
	}
}

func TestEnrollCmd_RejectsExistingStateWithoutForce(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	require.NoError(t, fleetnodebootstrap.SaveState(fleetnodebootstrap.StatePath(dir), &fleetnodebootstrap.State{FleetNodeID: 42}))
	cmd := &EnrollCmd{
		ServerURL:              "http://127.0.0.1:1",
		Name:                   "node-x",
		AllowInsecureTransport: true,
	}

	// Act
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state already populated")
	assert.Contains(t, err.Error(), "--force")
}

func TestEnrollCmd_PrintsForceWarningWhenStateIsPopulated(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	require.NoError(t, fleetnodebootstrap.SaveState(fleetnodebootstrap.StatePath(dir), &fleetnodebootstrap.State{FleetNodeID: 42}))
	fake := &fakeFleetNodeGateway{
		registerError: connect.NewError(connect.CodeFailedPrecondition, errors.New("name in use")),
	}
	srv := newFakeServer(t, fake)
	cmd := &EnrollCmd{
		ServerURL:              srv.URL,
		Name:                   "the-node",
		Force:                  true,
		AllowInsecureTransport: true,
	}
	var stderr bytes.Buffer

	// Act (Register fails by design; the warning must have fired before that)
	err := cmd.run(&Context{StateDir: dir}, strings.NewReader("any-code\n"), &bytes.Buffer{}, &stderr)

	// Assert
	require.Error(t, err)
	assert.Contains(t, stderr.String(), "warning: --force")
	assert.Contains(t, stderr.String(), "fleet_node_id=42")
	assert.Contains(t, stderr.String(), `"the-node"`)
}
