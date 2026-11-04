package main

import (
	"encoding/json"
	"net/http"
)

// The hardware API has no LED readback; this load-only audit verifies commands
// that arrived through Fleet and the real Node without manufacturing success.
func loadLEDAudit(state *MinerState) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		state.mu.RLock()
		active := state.IsBlinking
		state.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"active": active})
	})
}
