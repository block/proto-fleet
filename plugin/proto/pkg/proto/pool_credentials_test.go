package proto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdatePoolsPreservesEveryPasswordField(t *testing.T) {
	var received []map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/pools", r.URL.Path)
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	pools := []Pool{
		{Priority: 0, URL: "stratum+tcp://primary.example:3333", WorkerName: "customer.primary", Password: "primary-secret"},
		{Priority: 1, URL: "stratum+tcp://backup.example:3333", WorkerName: "customer.backup", Password: "backup-secret"},
		{Priority: 2, URL: "stratum+tcp://last.example:3333", WorkerName: "customer.last", Password: ""},
	}
	require.NoError(t, client.UpdatePools(t.Context(), pools))
	require.Len(t, received, len(pools))
	for i, pool := range pools {
		var password string
		raw, present := received[i]["password"]
		require.True(t, present, "password must be present even when explicitly empty")
		require.NoError(t, json.Unmarshal(raw, &password))
		assert.Equal(t, pool.Password, password)
		assert.JSONEq(t, fmt.Sprintf(`{"url":%q,"username":%q,"password":%q,"priority":%d}`, pool.URL, pool.WorkerName, pool.Password, pool.Priority), string(mustPoolJSON(t, received[i])))
	}
}

func mustPoolJSON(t *testing.T, pool map[string]json.RawMessage) []byte {
	t.Helper()
	raw, err := json.Marshal(pool)
	require.NoError(t, err)
	return raw
}

func TestUpdatePoolsRedactsFirmwareEcho(t *testing.T) {
	const secret = "pool-private-password"
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"plain forbidden", http.StatusForbidden, "rejected password=" + secret, "forbidden: access denied"},
		{"structured forbidden", http.StatusForbidden, `{"error":{"code":"BAD_POOL","message":"rejected password=` + secret + `"}}`, "forbidden: access denied"},
		{"default password", http.StatusForbidden, `{"error":{"code":"DEFAULT_PASSWORD_ACTIVE","message":"default password must be changed; password=` + secret + `"}}`, "forbidden: default password must be changed"},
		{"default code only", http.StatusForbidden, `{"error":{"code":"default_password_active","message":"echo=` + secret + `"}}`, "forbidden: default password must be changed"},
		{"default raw code", http.StatusForbidden, "  DEFAULT_PASSWORD_ACTIVE echo=" + secret, "forbidden: default password must be changed"},
		{"default raw message", http.StatusForbidden, "Default Password Must Be Changed echo=" + secret, "forbidden: default password must be changed"},
		{"marker outside structured error", http.StatusForbidden, `{"error":{"code":"BAD_POOL","message":"denied"},"detail":"DEFAULT_PASSWORD_ACTIVE ` + secret + `"}`, "forbidden: access denied"},
		{"empty forbidden", http.StatusForbidden, "", "forbidden: access denied"},
		{"invalid request", http.StatusBadRequest, secret, "update pools failed with status 400"},
		{"conflict", http.StatusConflict, secret, "update pools failed with status 409"},
		{"server failure", http.StatusInternalServerError, secret, "update pools failed with status 500"},
		{"unauthorized", http.StatusUnauthorized, secret, "unauthenticated: missing or invalid credentials"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := newTestClient(t, server)
			err := client.UpdatePools(t.Context(), []Pool{{URL: "stratum+tcp://pool.example:3333", WorkerName: "worker"}})
			require.Error(t, err)
			assert.EqualError(t, err, tc.want)
			assert.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), secret)
		})
	}
}

func TestUpdatePoolsPreservesTransportCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	client := newTestClient(t, server)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := client.UpdatePools(ctx, []Pool{{URL: "stratum+tcp://pool.example:3333", Password: "pool-private-password"}})
	require.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), "pool-private-password")
}

func TestPoolPrivateCredentialRedaction(t *testing.T) {
	const secret = "private-proto-pool-credential"
	pool := Pool{Priority: 0, URL: "stratum+tcp://pool.example:3333", WorkerName: "customer.worker", Password: secret}
	for _, rendered := range []string{fmt.Sprintf("%v", pool), fmt.Sprintf("%+v", pool), fmt.Sprintf("%#v", pool), fmt.Sprintf("%v", &pool)} {
		assert.NotContains(t, rendered, secret)
	}
	encoded, err := json.Marshal(pool)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), secret)
}
