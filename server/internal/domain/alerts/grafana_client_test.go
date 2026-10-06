package alerts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// grafanaFolderFake serves the two folder endpoints EnsureFolder touches.
type grafanaFolderFake struct {
	getStatus    int
	postStatus   int
	createdUID   string
	createdTitle string
	createdCalls int
}

func (f *grafanaFolderFake) client(t *testing.T) *Grafana {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/folders/{uid}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.getStatus)
		switch f.getStatus {
		case http.StatusOK:
			_, _ = w.Write([]byte(`{"uid":"u","title":"t"}`))
		case http.StatusForbidden:
			// Grafana 13's fail-closed body for non-admins when the folder does not exist.
			_, _ = w.Write([]byte(`{"accessErrorId":"ACE123","message":"You'll need additional permissions to perform this action. Permissions needed: folders:read","title":"Access denied"}`))
		case http.StatusNotFound:
			_, _ = w.Write([]byte(`{"message":"folder not found"}`))
		default:
			_, _ = w.Write([]byte(`{"message":"boom"}`))
		}
	})
	mux.HandleFunc("POST /api/folders", func(w http.ResponseWriter, r *http.Request) {
		f.createdCalls++
		var folder GrafanaFolder
		require.NoError(t, json.NewDecoder(r.Body).Decode(&folder))
		f.createdUID = folder.UID
		f.createdTitle = folder.Title
		w.Header().Set("Content-Type", "application/json")
		// Cases that must not create leave postStatus zero; answer 500 so createdCalls fails the test cleanly instead of WriteHeader(0) panicking.
		if f.postStatus == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"unexpected folder create"}`))
			return
		}
		w.WriteHeader(f.postStatus)
		if f.postStatus == http.StatusOK {
			// The real create echoes the folder back.
			_ = json.NewEncoder(w).Encode(folder)
			return
		}
		_, _ = w.Write([]byte(`{"message":"x"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewGrafana(GrafanaConfig{URL: srv.URL})
}

func TestEnsureFolder(t *testing.T) {
	cases := []struct {
		name       string
		getStatus  int
		postStatus int
		wantErr    string
		wantCreate bool
	}{
		{"already exists", http.StatusOK, 0, "", false},
		{"missing then created", http.StatusNotFound, http.StatusOK, "", true},
		// Grafana 13 returns 403 folders:read (not 404) to non-admins for a missing folder.
		{"forbidden probe then created", http.StatusForbidden, http.StatusOK, "", true},
		{"forbidden probe, concurrent create conflict", http.StatusForbidden, http.StatusConflict, "", true},
		{"forbidden probe, create 412 tolerated as conflict", http.StatusForbidden, http.StatusPreconditionFailed, "", true},
		{"forbidden probe, create also forbidden", http.StatusForbidden, http.StatusForbidden, "create folder", true},
		// 401 means bad credentials, not fail-closed absence: it must stay on the loud probe error and never attempt a create.
		{"unauthorized probe", http.StatusUnauthorized, 0, "get folder", false},
		{"probe server error", http.StatusInternalServerError, 0, "get folder", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &grafanaFolderFake{getStatus: tc.getStatus, postStatus: tc.postStatus}
			err := fake.client(t).EnsureFolder(context.Background(), "proto-fleet-user-7", "Proto Fleet User Rules (org 7)")
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tc.wantCreate {
				assert.Equal(t, 1, fake.createdCalls)
				assert.Equal(t, "proto-fleet-user-7", fake.createdUID)
				assert.Equal(t, "Proto Fleet User Rules (org 7)", fake.createdTitle)
			} else {
				assert.Zero(t, fake.createdCalls)
			}
		})
	}
}
