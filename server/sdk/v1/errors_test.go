package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDefaultPasswordContract pins the SDK-owned parts of the default-password
// contract: the error-code value and the case-insensitive code check. Driver-
// specific response parsing (e.g. Proto firmware's message text) belongs in
// the driver package — see plugin/proto/pkg/proto for Proto's contract tests.
func TestDefaultPasswordContract(t *testing.T) {
	t.Run("code value is stable", func(t *testing.T) {
		assert.Equal(t, "DEFAULT_PASSWORD_ACTIVE", string(ErrCodeDefaultPasswordActive),
			"drivers and the fleet server key on this code over gRPC; changing it breaks detection everywhere")
	})

	t.Run("IsDefaultPasswordCode matches case-insensitively", func(t *testing.T) {
		assert.True(t, IsDefaultPasswordCode(string(ErrCodeDefaultPasswordActive)))
		assert.True(t, IsDefaultPasswordCode("default_password_active"))
		assert.True(t, IsDefaultPasswordCode("Default_Password_Active"))
		assert.False(t, IsDefaultPasswordCode("ACCESS_DENIED"))
		assert.False(t, IsDefaultPasswordCode(""))
	})

	t.Run("constructor sets code and non-empty message", func(t *testing.T) {
		err := NewErrorDefaultPasswordActive("device-xyz")
		assert.Equal(t, ErrCodeDefaultPasswordActive, err.Code)
		assert.Contains(t, err.Message, "device-xyz",
			"the message should identify which device needs remediation")
	})
}
