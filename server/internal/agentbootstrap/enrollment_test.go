package agentbootstrap

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister_HappyPath(t *testing.T) {
	t.Parallel()

	// Arrange
	fake := &fakeAgentGateway{
		expectedCode: "enroll-code-xyz",
		agentID:      77,
	}
	srv := newFakeServer(t, fake)

	// Act
	result, err := Register(t.Context(), RegisterParams{
		ServerURL:              srv.URL,
		Name:                   "test-agent",
		Code:                   fake.expectedCode,
		AllowInsecureTransport: true,
	})

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int64(77), result.State.AgentID)
	assert.Len(t, result.State.IdentityFingerprint, 16)
	assert.Equal(t, ed25519.PublicKeySize*2, len(result.State.IdentityPublicKeyHex))
	assert.Equal(t, ed25519.PrivateKeySize*2, len(result.State.IdentityPrivateKeyHex))
	assert.Empty(t, result.State.APIKey, "Register returns partial state without api_key")
	assert.Empty(t, result.State.SessionToken, "Register returns partial state without session_token")
	assert.True(t, result.State.AllowInsecureTransport)
	assert.True(t, fake.registered)
}

func TestRegister_TranslatesErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		registerErr error
		wantTarget  error
		wantSub     string
	}{
		{
			name:        "already_exists wraps ErrRegisterRejected",
			registerErr: connect.NewError(connect.CodeAlreadyExists, errors.New("name in use")),
			wantTarget:  ErrRegisterRejected,
			wantSub:     "server rejected register",
		},
		{
			name:        "failed_precondition wraps ErrRegisterRejected",
			registerErr: connect.NewError(connect.CodeFailedPrecondition, errors.New("agent identity or name already in use")),
			wantTarget:  ErrRegisterRejected,
			wantSub:     "server rejected register",
		},
		{
			name:        "unauthenticated (typoed/expired enrollment code) wraps ErrRegisterRejected",
			registerErr: connect.NewError(connect.CodeUnauthenticated, errors.New("invalid enrollment code")),
			wantTarget:  ErrRegisterRejected,
			wantSub:     "server rejected register",
		},
		{
			name:        "other code wraps with generic register: prefix",
			registerErr: connect.NewError(connect.CodeInternal, errors.New("boom")),
			wantSub:     "register:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			fake := &fakeAgentGateway{registerError: tc.registerErr}
			srv := newFakeServer(t, fake)

			// Act
			_, err := Register(t.Context(), RegisterParams{
				ServerURL:              srv.URL,
				Name:                   "agent-x",
				Code:                   "any",
				AllowInsecureTransport: true,
			})

			// Assert
			require.Error(t, err)
			if tc.wantTarget != nil {
				assert.ErrorIs(t, err, tc.wantTarget)
			}
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

func TestRegister_ValidatesInputs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		params  RegisterParams
		wantSub string
	}{
		{
			name:    "rejects plain http for remote host",
			params:  RegisterParams{ServerURL: "http://fleet.example.com", Name: "n", Code: "c"},
			wantSub: "https",
		},
		{
			name:    "requires name",
			params:  RegisterParams{ServerURL: "https://fleet.example.com", Code: "c"},
			wantSub: "Name",
		},
		{
			name:    "requires code",
			params:  RegisterParams{ServerURL: "https://fleet.example.com", Name: "n"},
			wantSub: "Code",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act
			_, err := Register(t.Context(), tc.params)

			// Assert
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

func TestCompleteEnrollment_HappyPath(t *testing.T) {
	t.Parallel()

	// Arrange: simulate the post-Register state by running Register against
	// the fake, then calling CompleteEnrollment with the matching api_key.
	expiresAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	fake := &fakeAgentGateway{
		expectedCode:     "code",
		expectedAPIKey:   "fleet_post_register_key",
		agentID:          88,
		challenge:        bytes.Repeat([]byte{0x42}, 32),
		sessionToken:     "session-after-complete",
		sessionExpiresAt: expiresAt,
	}
	srv := newFakeServer(t, fake)
	result, err := Register(t.Context(), RegisterParams{
		ServerURL:              srv.URL,
		Name:                   "agent-88",
		Code:                   "code",
		AllowInsecureTransport: true,
	})
	require.NoError(t, err)

	// Act
	err = CompleteEnrollment(t.Context(), result.State, fake.expectedAPIKey)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "fleet_post_register_key", result.State.APIKey)
	assert.Equal(t, "session-after-complete", result.State.SessionToken)
	assert.WithinDuration(t, expiresAt, result.State.SessionExpiresAt, time.Second)
	assert.True(t, fake.signatureVerified)
}

func TestCompleteEnrollment_RejectsNonHTTPSWhenAllowInsecureUnset(t *testing.T) {
	t.Parallel()

	// Arrange: a tampered or stale state file where ServerURL was downgraded
	// to plaintext http for a non-loopback host. CompleteEnrollment must
	// refuse before sending the api_key on the wire.
	state := &State{
		ServerURL:              "http://fleet.example.com",
		AllowInsecureTransport: false,
		IdentityPrivateKeyHex:  hex.EncodeToString(make([]byte, ed25519.PrivateKeySize)),
		IdentityPublicKeyHex:   hex.EncodeToString(make([]byte, ed25519.PublicKeySize)),
	}

	// Act
	err := CompleteEnrollment(t.Context(), state, "fleet_some_key")

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
	assert.Empty(t, state.APIKey, "rejected URL must not leave api_key written into state")
}

func TestCompleteEnrollment_PreservesStateOnHandshakeFailure(t *testing.T) {
	t.Parallel()

	// Arrange: valid URL, but the supplied api_key does not match what the
	// fake expects, so BeginAuthHandshake will return Unauthenticated.
	pub, priv, err := GenerateKeypair()
	require.NoError(t, err)
	fake := &fakeAgentGateway{
		expectedAPIKey: "right-key",
		identityPub:    pub,
		challenge:      bytes.Repeat([]byte{0x10}, 32),
	}
	srv := newFakeServer(t, fake)
	state := &State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
	}

	// Act
	err = CompleteEnrollment(t.Context(), state, "wrong-key")

	// Assert
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBeginAuthRejected)
	// The rejected key must NOT be persisted into state; on retry, the
	// caller can supply a different api_key without first re-zeroing it.
	assert.Empty(t, state.APIKey)
	assert.Empty(t, state.SessionToken)
	assert.True(t, state.SessionExpiresAt.IsZero())
}

func TestCompleteEnrollment_RejectsEmptyInputs(t *testing.T) {
	t.Parallel()

	// Act
	errNilState := CompleteEnrollment(t.Context(), nil, "k")
	errEmptyKey := CompleteEnrollment(t.Context(), &State{}, "")

	// Assert
	require.Error(t, errNilState)
	require.Error(t, errEmptyKey)
	assert.Contains(t, errNilState.Error(), "state")
	assert.Contains(t, errEmptyKey.Error(), "apiKey")
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
		{name: "userinfo rejected", url: "https://fleet.example.com@attacker.example", allowInsecure: false, wantErr: "userinfo"},
		{name: "userinfo with password rejected", url: "https://user:pass@attacker.example", allowInsecure: false, wantErr: "userinfo"},
		{name: "query string rejected", url: "https://fleet.example.com?foo=bar", allowInsecure: false, wantErr: "query"},
		{name: "fragment rejected", url: "https://fleet.example.com#frag", allowInsecure: false, wantErr: "fragment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act
			err := ValidateServerURL(tc.url, tc.allowInsecure)

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
