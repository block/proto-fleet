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
	// Webhook URLs routinely embed capability tokens in the path, so
	// the whole value is redacted.
	assert.NotContains(t, out, "hooks.example.com")

	var v struct {
		Name     string         `json:"name"`
		Settings map[string]any `json:"settings"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &v))
	assert.Equal(t, "org-7-pager", v.Name)
	assert.Equal(t, "[REDACTED]", v.Settings["authorization_credentials"])
	assert.Equal(t, "[REDACTED]", v.Settings["smtpPassword"])
	assert.Equal(t, "[REDACTED]", v.Settings["url"])
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

func TestRedactSecretsNonJSONIsNotPassedThrough(t *testing.T) {
	// A non-JSON body can't be key-redacted and may echo the request
	// payload, so it's replaced with a length marker — never the raw
	// content.
	out := redactSecrets([]byte("Bad Gateway: upstream sent authorization_credentials=sk-secret"))
	assert.NotContains(t, out, "sk-secret")
	assert.Contains(t, out, "non-JSON response body omitted")
	assert.Equal(t, "", redactSecrets(nil))
}
