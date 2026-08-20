package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  string
	}{
		{name: "too short", password: "1234567", wantErr: "at least 8 characters"},
		{name: "invalid UTF-8", password: "\xff\xff\xff\xff\xff\xff\xff\xff", wantErr: "valid UTF-8"},
		{name: "minimum length", password: "12345678"},
		{name: "unicode characters", password: "密码密码密码密码"},
		{name: "maximum bytes", password: strings.Repeat("a", 72)},
		{name: "too many bytes", password: strings.Repeat("a", 73), wantErr: "at most 72 bytes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePassword(test.password)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestGeneratedTemporaryPasswordSatisfiesPolicy(t *testing.T) {
	password, err := GenerateTemporaryPassword()

	require.NoError(t, err)
	require.NoError(t, ValidatePassword(password))
}
