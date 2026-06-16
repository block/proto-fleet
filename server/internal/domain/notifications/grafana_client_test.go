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

func TestRedactSecretsScrubsSecretsInStringValues(t *testing.T) {
	in := []byte(`{"message": "failed to POST to https://hooks.slack.com/services/T1/B2/SECRET: 403"}`)
	out := redactSecrets(in)
	assert.NotContains(t, out, "SECRET")
	assert.NotContains(t, out, "hooks.slack.com")
	assert.Contains(t, out, "[REDACTED-URL]")

	bearer := redactSecrets([]byte(`{"error": "upstream rejected Authorization: Bearer sk-abc123def"}`))
	assert.NotContains(t, bearer, "sk-abc123def")
	assert.Contains(t, bearer, "[REDACTED]")
}

func TestRedactSecretsScrubsPunctuationBearingBearerTokens(t *testing.T) {
	cases := []string{
		"Bearer aGVsbG8+d29ybGQ/Zm9v==",
		"Bearer abc.def~ghi:jkl",
		"Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.s3cr3t-Sig=",
	}
	for _, raw := range cases {
		secret := raw[len("Bearer "):]
		out := redactSecrets([]byte(`{"error": "rejected Authorization: ` + raw + `"}`))
		assert.NotContainsf(t, out, secret, "full token leaked for %q", raw)
		assert.NotContainsf(t, out, secret[len(secret)/2:], "token suffix leaked for %q", raw)
		assert.Contains(t, out, "Bearer [REDACTED]")
	}
}

func TestRedactSecretsNonJSONIsNotPassedThrough(t *testing.T) {
	out := redactSecrets([]byte("Bad Gateway: upstream sent authorization_credentials=sk-secret"))
	assert.NotContains(t, out, "sk-secret")
	assert.Contains(t, out, "non-JSON response body omitted")
	assert.Equal(t, "", redactSecrets(nil))
}
