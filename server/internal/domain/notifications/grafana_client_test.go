package notifications

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactSecrets(t *testing.T) {
	in := []byte(`{
		"name": "org-7-pager",
		"type": "webhook",
		"settings": {
			"url": "https://hooks.example.com/x",
			"authorization_scheme": "Bearer",
			"authorization_credentials": "super-secret-token",
			"smtpPassword": "hunter2",
			"empty": ""
		}
	}`)
	out := redactSecrets(in)

	assert.NotContains(t, out, "super-secret-token")
	assert.NotContains(t, out, "hunter2")

	var v struct {
		Name     string         `json:"name"`
		Settings map[string]any `json:"settings"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &v))
	assert.Equal(t, "org-7-pager", v.Name)
	assert.Equal(t, "[REDACTED]", v.Settings["authorization_credentials"])
	assert.Equal(t, "[REDACTED]", v.Settings["smtpPassword"])
	assert.Equal(t, "https://hooks.example.com/x", v.Settings["url"])
}

func TestRedactSecretsKeepsEmptyValues(t *testing.T) {
	out := redactSecrets([]byte(`{"authorization_credentials": ""}`))
	assert.JSONEq(t, `{"authorization_credentials": ""}`, out)
}

func TestRedactSecretsArrays(t *testing.T) {
	out := redactSecrets([]byte(`[{"password": "p1"}, {"password": "p2"}]`))
	assert.NotContains(t, out, "p1")
	assert.NotContains(t, out, "p2")
}

func TestRedactSecretsNonJSONPassthrough(t *testing.T) {
	assert.Equal(t, "plain text error", redactSecrets([]byte("plain text error")))
	assert.Equal(t, "", redactSecrets(nil))
}
