package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoadLEDAuditRequiresAuthentication(t *testing.T) {
	state, err := newMinerState("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	state.LoadMode = true
	response := httptest.NewRecorder()
	createHTTPHandler(state).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/fake-api/v1/load/led", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("load audit returned %d, want 401", response.Code)
	}
	state.LoadMode = false
	response = httptest.NewRecorder()
	createHTTPHandler(state).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/fake-api/v1/load/led", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("single fixture exposes load audit: %d", response.Code)
	}
}

func TestLoadLEDAuditReadsAppliedBlink(t *testing.T) {
	state, err := newMinerState("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, active := range []bool{true, false} {
		value := "false"
		if active {
			value = "true"
		}
		response := httptest.NewRecorder()
		createBlinkHandler(state)(response, httptest.NewRequest(http.MethodPost, "/cgi-bin/blink.cgi", strings.NewReader(`{"blink":"`+value+`"}`)))
		if response.Code != http.StatusOK {
			t.Fatal(response.Code)
		}
		response = httptest.NewRecorder()
		loadLEDAudit(state).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/fake-api/v1/load/led", nil))
		var result struct {
			Active bool `json:"active"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Active != active {
			t.Fatalf("readback: %s %v", response.Body.String(), err)
		}
	}
	response := httptest.NewRecorder()
	loadLEDAudit(state).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/fake-api/v1/load/led", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatal(response.Code)
	}
}
