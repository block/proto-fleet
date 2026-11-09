package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestV2PoolClearStableIdentityAndAuthentication(t *testing.T) {
	state := NewMinerState("test-rig", "00:11:22:33:44:55")
	state.SetAccessToken("test-token")
	handler := NewRESTApiHandler(state)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	request := func(method, path, body string, authenticate bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if authenticate {
			r.Header.Set("Authorization", "Bearer test-token")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/v2/pools", "/api/v2/pools/diagnostics", "/api/v2/pools/1"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "/1") {
			method = http.MethodDelete
		}
		if got := request(method, path, "", false).Code; got != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s: %d", path, got)
		}
	}
	create := func() int64 {
		t.Helper()
		w := request(http.MethodPost, "/api/v1/pools", `[{"url":"stratum+tcp://pool.example:3333","username":"worker","password":"private-password"}]`, true)
		if w.Code != http.StatusOK {
			t.Fatalf("create: %d %s", w.Code, w.Body.String())
		}
		w = request(http.MethodGet, "/api/v2/pools", "", true)
		if strings.Contains(w.Body.String(), "private-password") {
			t.Fatal("readback exposed a password")
		}
		var response struct {
			Pools []v2Pool `json:"pools"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Pools) != 1 || response.Pools[0].ID <= 0 {
			t.Fatalf("invalid pool readback: %s", w.Body.String())
		}
		return response.Pools[0].ID
	}
	oldID := create()
	newID := create()
	if oldID == newID {
		t.Fatal("replacement reused a stable ID")
	}
	if got := request(http.MethodDelete, fmt.Sprintf("/api/v2/pools/%d", oldID), "", true).Code; got != http.StatusNotFound {
		t.Fatalf("stale delete: %d", got)
	}
	if len(state.GetPools()) != 1 {
		t.Fatal("stale delete changed replacement")
	}
	if got := request(http.MethodDelete, fmt.Sprintf("/api/v2/pools/%d", newID), "", true).Code; got != http.StatusOK {
		t.Fatalf("last-pool delete: %d", got)
	}
	for _, path := range []string{"/api/v2/pools", "/api/v2/pools/diagnostics", "/api/v1/pools"} {
		w := request(http.MethodGet, path, "", true)
		var response struct {
			Pools []json.RawMessage `json:"pools"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || len(response.Pools) != 0 {
			t.Fatalf("pools remain at %s: %s", path, w.Body.String())
		}
	}
	if state.GetMiningState() != MiningStateNoPools {
		t.Fatal("cleared rig should report no pools")
	}
}
