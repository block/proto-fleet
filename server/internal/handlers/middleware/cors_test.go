package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestCORSPreflightPermitsAccountWorkspaceHeaderWhenCrossOriginEnabled(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		label := "default_same_origin"
		if enabled {
			label = "cross_origin_enabled"
		}
		t.Run(label, func(t *testing.T) {
			called := false
			handler := NewCORSMiddleware(enabled).Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			req := httptest.NewRequest(http.MethodOptions, "/hosted.v1.HostedAccountService/ListAccounts", nil)
			req.Header.Set("Origin", "https://fleet-client.example")
			req.Header.Set("Access-Control-Request-Method", http.MethodPost)
			req.Header.Set("Access-Control-Request-Headers", strings.Join([]string{"connect-protocol-version", "content-type", strings.ToLower(hostedaccounts.RequestAccountHeader)}, ", "))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			require.False(t, called, "preflight is answered before hosted RPC execution")
			if enabled {
				require.NotEmpty(t, response.Header().Get("Access-Control-Allow-Origin"))
				require.Equal(t, "true", response.Header().Get("Access-Control-Allow-Credentials"))
				require.Contains(t, strings.ToLower(response.Header().Get("Access-Control-Allow-Headers")), strings.ToLower(hostedaccounts.RequestAccountHeader))
			} else {
				require.Empty(t, response.Header().Get("Access-Control-Allow-Origin"), "default middleware must retain same-origin policy")
			}
		})
	}
}
