package notifications

import (
	"encoding/json"
	"net/http"
)

// EnabledResponse reports whether the notifications stack (the Grafana sidecar
// the ChannelService proxies) is enabled for this deployment.
type EnabledResponse struct {
	Enabled bool `json:"enabled"`
}

// NewEnabledHandler serves a tiny, unauthenticated capability probe the client
// uses to decide whether to surface the Notifications settings nav at runtime.
//
// The released client is a prebuilt bundle, so a build-time flag can't track a
// per-deployment runtime toggle (`run-fleet.sh --enable-beta-notifications`).
// This endpoint lets the bundle learn, at load time, whether the sidecar is on.
func NewEnabledHandler(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(EnabledResponse{Enabled: enabled})
	}
}
