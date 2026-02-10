package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api"
)

func TestHandleTestPoolConnection_InvalidURL_Returns400(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools/test-connection", strings.NewReader(`{"url":"aaa"}`))
	h.handleTestPoolConnection(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	var resp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v; body=%s", err, rr.Body.String())
	}
	if resp.Error.Message != "Invalid pool URL" {
		t.Fatalf("expected error message %q, got %q", "Invalid pool URL", resp.Error.Message)
	}
}

func TestHandleTestPoolConnection_ValidURL_Returns200(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools/test-connection", strings.NewReader(`{"url":"stratum+tcp://mine.ocean.xyz:3334"}`))
	h.handleTestPoolConnection(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestCreatePools_InvalidURL_DoesNotClearExistingPools(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55") // seed with an existing pool
	state.AddPool(&miner_data_api.Pool{Idx: 0, Url: "stratum+tcp://mine.ocean.xyz:3334", Username: "u"})

	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", strings.NewReader(`[{"url":"aaa","username":"u"}]`))
	h.createPools(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	pools := state.GetPools()
	if len(pools) != 1 {
		t.Fatalf("expected existing pools to remain, got %d", len(pools))
	}
	if pools[0].Url != "stratum+tcp://mine.ocean.xyz:3334" {
		t.Fatalf("expected original pool url to remain, got %q", pools[0].Url)
	}
}
