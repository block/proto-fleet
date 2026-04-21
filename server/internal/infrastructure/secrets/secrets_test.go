package secrets

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestText(t *testing.T) {
	testSecrets := []string{
		"",                               // empty string
		"abcde",                          // length 5
		"1234567",                        // length 7
		"masterKey12",                    // length 11
		"X5yR8eC1z3NwD6kS",               // length 16
		"f6KcQ3vTD8rS2gW7j4ZyE0hP",       // length 24
		"l5TnV8bQR6pX3uF2",               // length 16
		"secret123456789012345",          // length 21
		"UltraSecretValueHere",           // length 20
		"A1b2C3d4E5f",                    // length 11
		"1234567890ABCDEFGHIJKLMNOPQRST", // length 30
	}
	t.Run("get value is correct", func(t *testing.T) {
		for _, secret := range testSecrets {
			text := NewText(secret)
			assert.Equal(t, secret, text.Value())
		}
	})

	t.Run("string representation is default text", func(t *testing.T) {
		for _, secret := range testSecrets {
			text := NewText(secret)
			assert.NotEqual(t, secret, text.String())
			assert.Equal(t, defaultRedacted, text.String())
		}
	})

	t.Run("fmt string representation is not value", func(t *testing.T) {
		for _, secret := range testSecrets {
			text := NewText(secret)
			assert.NotEqual(t, secret, fmt.Sprint(text))
			assert.NotEqual(t, secret, fmt.Sprintf("%s", text))
		}
	})

	t.Run("marshal json returns default text", func(t *testing.T) {
		for _, secret := range testSecrets {
			text := NewText(secret)
			data, err := text.MarshalJSON()
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf(`"%s"`, defaultRedacted), string(data))
		}
	})

	t.Run("text handler slogs do not leak secrets", func(t *testing.T) {
		for _, secret := range testSecrets {
			if secret == "" {
				continue // Skip empty secret for logging test
			}
			logBuffer := bytes.Buffer{}
			logger := slog.New(slog.NewTextHandler(&logBuffer, nil))

			text := NewText(secret)
			logger.Info("Testing secret", "secret", text)
			logOutput := logBuffer.String()
			assert.NotContains(t, logOutput, secret, "Secret should not be logged")
			assert.Contains(t, logOutput, defaultRedacted, "Default text should be logged")
		}
	})

	t.Run("text handler slogs do not leak with pointer", func(t *testing.T) {
		for _, secret := range testSecrets {
			if secret == "" {
				continue // Skip empty secret for logging test
			}
			data := struct {
				Secret *Text
			}{
				Secret: NewText(secret),
			}
			logBuffer := bytes.Buffer{}
			logger := slog.New(slog.NewTextHandler(&logBuffer, nil))

			logger.Info("Testing secret", "secret", data)
			logOutput := logBuffer.String()
			assert.NotContains(t, logOutput, secret, "Secret should not be logged")
			assert.Contains(t, logOutput, defaultRedacted, "Default text should be logged")
		}
	})

	t.Run("text handler slogs do not leak with value", func(t *testing.T) {
		for _, secret := range testSecrets {
			if secret == "" {
				continue // Skip empty secret for logging test
			}
			data := struct {
				Secret Text
			}{
				Secret: *NewText(secret),
			}
			logBuffer := bytes.Buffer{}
			logger := slog.New(slog.NewTextHandler(&logBuffer, nil))

			logger.Info("Testing secret", "secret", data)
			logOutput := logBuffer.String()
			assert.NotContains(t, logOutput, secret, "Secret should not be logged")
			assert.Contains(t, logOutput, defaultRedacted, "Default text should be logged")
		}
	})

	t.Run("json handler slogs do not leak secrets", func(t *testing.T) {
		for _, secret := range testSecrets {
			if secret == "" {
				continue // Skip empty secret for logging test
			}
			logBuffer := bytes.Buffer{}
			logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))

			text := NewText(secret)
			logger.Info("Testing secret", "secret", text)
			logOutput := logBuffer.String()
			assert.NotContains(t, logOutput, secret, "Secret should not be logged")
			assert.Contains(t, logOutput, defaultRedacted, "Default text should be logged")
		}
	})

	t.Run("unmarshal json works but doesn't get value", func(t *testing.T) {
		for _, secret := range testSecrets {
			if secret == "" {
				continue // Skip empty secret for unmarshalling test
			}
			data := []byte(fmt.Sprintf(`"%s"`, secret))
			text := &Text{}
			err := text.UnmarshalJSON(data)
			require.NoError(t, err)
			assert.NotEqual(t, secret, text.Value())
		}
	})
}
