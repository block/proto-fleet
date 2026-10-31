package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadLEDAuditRequiresAuthentication(t *testing.T) {
	handler, err := newMultiMinerHandler(addressFile(t, "127.0.0.1\n"))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/fake-api/v1/load/led", nil)
	request = request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("load audit returned %d, want 401", response.Code)
	}
	response = httptest.NewRecorder()
	newRESTAPIHandler(NewMinerState("single", "02:00:00:00:00:01")).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("single fixture exposes load audit: %d", response.Code)
	}
}

func TestLoadLEDAuditReadsAppliedLocate(t *testing.T) {
	state := NewMinerState("load", "02:00:00:00:00:01")
	api := NewRESTApiHandler(state)
	for _, active := range []bool{true, false} {
		value := "false"
		if active {
			value = "true"
		}
		response := httptest.NewRecorder()
		api.handleLocate(response, httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?enable="+value, nil))
		if response.Code != http.StatusOK && response.Code != http.StatusAccepted {
			t.Fatal(response.Code)
		}
		response = httptest.NewRecorder()
		loadLEDAudit(state)(response, httptest.NewRequest(http.MethodGet, "/fake-api/v1/load/led", nil))
		var result struct {
			Active bool `json:"active"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Active != active {
			t.Fatalf("readback: %s %v", response.Body.String(), err)
		}
	}
	response := httptest.NewRecorder()
	loadLEDAudit(state)(response, httptest.NewRequest(http.MethodPost, "/fake-api/v1/load/led", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatal(response.Code)
	}
}
