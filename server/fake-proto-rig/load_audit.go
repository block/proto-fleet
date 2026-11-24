package main

import (
	"encoding/json"
	"net/http"
)

// The native locate API has no readback. Only the multi-address load handler
// registers this audit, with the same bearer authentication as native writes.
func loadLEDAudit(state *MinerState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"active": state.IsLocateActive()})
	}
}
