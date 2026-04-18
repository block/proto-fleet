package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDefaultPasswordContract pins the firmware #3269 default-password
// response contract. If firmware changes either the error code or the
// human-readable message prefix, this test fails here FIRST rather than
// silently skipping remediation in downstream layers (client, device,
// plugin_miner, telemetry).
//
// If firmware intentionally changes its contract, update BOTH the markers
// below AND the firmware reference in NewErrorDefaultPasswordActive's comment.
func TestDefaultPasswordContract(t *testing.T) {
	t.Run("code marker matches firmware ErrorCode", func(t *testing.T) {
		assert.Equal(t, "DEFAULT_PASSWORD_ACTIVE", string(ErrCodeDefaultPasswordActive),
			"firmware #3269 returns this exact code in the 403 body; changing it silently breaks detection")
		assert.True(t, IsDefaultPasswordCode(string(ErrCodeDefaultPasswordActive)))
		assert.True(t, IsDefaultPasswordCode("default_password_active"), "case-insensitive")
		assert.False(t, IsDefaultPasswordCode("ACCESS_DENIED"))
		assert.False(t, IsDefaultPasswordCode(""))
	})

	t.Run("message marker matches firmware prose", func(t *testing.T) {
		assert.Equal(t, "default password must be changed", DefaultPasswordMessageMarker,
			"this substring is what firmware returns in free-text 403 bodies — changing it silently breaks detection")
	})

	t.Run("generated SDKError message contains the marker", func(t *testing.T) {
		err := NewErrorDefaultPasswordActive("device-xyz")
		assert.Contains(t, err.Message, DefaultPasswordMessageMarker,
			"NewErrorDefaultPasswordActive must emit the canonical marker so downstream detectors match it")
		assert.True(t, IsDefaultPasswordMessage(err.Error()))
	})

	t.Run("IsDefaultPasswordMessage matches known firmware shapes", func(t *testing.T) {
		cases := []struct {
			name     string
			msg      string
			expected bool
		}{
			{"firmware prose lowercase", "default password must be changed", true},
			{"firmware prose mixed case", "Default Password Must Be Changed", true},
			{"wrapped with prefix", "forbidden: default password must be changed", true},
			{"code as message body", "DEFAULT_PASSWORD_ACTIVE", true},
			{"code lowercase", "default_password_active", true},
			{"gRPC wrapped error", "rpc error: code = PermissionDenied desc = default password must be changed", true},
			{"generic forbidden", "forbidden: access denied", false},
			{"auth error", "unauthenticated: missing or invalid credentials", false},
			{"empty", "", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, IsDefaultPasswordMessage(tc.msg))
			})
		}
	})
}
