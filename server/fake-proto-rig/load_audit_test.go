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
	// Arrange
	handler, err := newMultiMinerHandler(addressFile(t, "127.0.0.1\n"))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/fake-api/v1/load/led", nil)
	request = request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}))
	response := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(response, request)

	// Assert
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("load audit returned %d, want 401", response.Code)
	}
	response = httptest.NewRecorder()

	// Act
	newRESTAPIHandler(NewMinerState("single", "02:00:00:00:00:01")).ServeHTTP(response, request)

	// Assert
	if response.Code != http.StatusNotFound {
		t.Fatalf("single fixture exposes load audit: %d", response.Code)
	}
}

func TestLoadLEDAuditReadsAppliedLocate(t *testing.T) {
	// Arrange
	state := NewMinerState("load", "02:00:00:00:00:01")
	api := NewRESTApiHandler(state)
	for _, active := range []bool{true, false} {
		value := "false"
		if active {
			value = "true"
		}
		response := httptest.NewRecorder()

		// Act
		api.handleLocate(response, httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?enable="+value, nil))

		// Assert
		if response.Code != http.StatusOK && response.Code != http.StatusAccepted {
			t.Fatal(response.Code)
		}
		response = httptest.NewRecorder()

		// Act
		loadLEDAudit(state)(response, httptest.NewRequest(http.MethodGet, "/fake-api/v1/load/led", nil))
		var result struct {
			Active bool `json:"active"`
		}

		// Assert
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Active != active {
			t.Fatalf("readback: %s %v", response.Body.String(), err)
		}
	}
	response := httptest.NewRecorder()

	// Act
	loadLEDAudit(state)(response, httptest.NewRequest(http.MethodPost, "/fake-api/v1/load/led", nil))

	// Assert
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatal(response.Code)
	}
}
