package main

import (
	"net/http"
	"strconv"
	"strings"
)

// Only the MDKv2 surface consumed by durable pool clearing is modeled here.
// JSON follows protobuf JSON (not MDKv1): int64 identities are strings and pool
// passwords are write-only. State changes finish before the success response.
// The fixture has no on-disk persistence and cannot prove firmware durability.
type v2Pool struct {
	ID       int64  `json:"id,string"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

type v2PoolDiagnostic struct {
	PoolID   int64  `json:"poolId,string"`
	PoolName string `json:"poolName"`
}

func (h *RESTApiHandler) handleV2Pools(w http.ResponseWriter, r *http.Request) {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/pools":
		pools := make([]v2Pool, 0, len(h.state.Pools))
		for _, pool := range h.state.Pools {
			pools = append(pools, v2Pool{ID: pool.StableID, Name: h.state.PoolNames[pool.Idx], URL: pool.Url, Username: pool.Username, Priority: pool.Priority, Enabled: true})
		}
		h.writeJSON(w, http.StatusOK, struct {
			Pools []v2Pool `json:"pools"`
		}{pools})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/pools/diagnostics":
		pools := make([]v2PoolDiagnostic, 0, len(h.state.Pools))
		for _, pool := range h.state.Pools {
			pools = append(pools, v2PoolDiagnostic{PoolID: pool.StableID, PoolName: h.state.PoolNames[pool.Idx]})
		}
		h.writeJSON(w, http.StatusOK, struct {
			Pools []v2PoolDiagnostic `json:"pools"`
		}{pools})
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v2/pools/"):
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v2/pools/"), 10, 64)
		if err != nil || id <= 0 {
			h.writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid pool ID")
			return
		}
		for i, pool := range h.state.Pools {
			if pool.StableID == id {
				delete(h.state.PoolNames, pool.Idx)
				h.state.Pools = append(h.state.Pools[:i], h.state.Pools[i+1:]...)
				h.writeJSON(w, http.StatusOK, struct{}{})
				return
			}
		}
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "Pool not found")
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}
