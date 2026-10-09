package minerproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/require"
)

func TestProxyAcceptedMutationRequiresCompletionEvidence(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusAccepted} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			database, org, device, h, target := proxyFenceFixture(t)
			transport := &proxyResponseTransport{status: status, body: io.NopCloser(strings.NewReader("{}"))}
			h.httpClient = &http.Client{Transport: transport}
			h.storeToken(target.cacheKey(), "fixture-token")
			response := newDeadlineProxyRecorder()
			started := false
			err := h.writeService.WithHostAttempt(t.Context(), org, device, target.binding.WriteEpoch, func(ctx context.Context) error {
				return h.forwardAndCopy(ctx, response, httptest.NewRequest(http.MethodPost, "http://fleet.test/", nil), target, "/api/v2/mining/start", nil, &started)
			})
			require.NoError(t, err)
			require.Equal(t, status, response.Code)
			require.Equal(t, int64(1), transport.calls.Load())
			require.Equal(t, "unknown", proxyAttemptStatus(t, database, device), "HTTP acceptance and body closure do not establish device completion")
			inspection, err := h.writeService.InspectRecovery(t.Context(), org, device)
			require.NoError(t, err)
			require.Len(t, inspection, 1)
			require.Equal(t, "host", inspection[0].FenceState)
			_, err = h.writeService.Reserve(t.Context(), org, device)
			require.True(t, fleeterror.IsFailedPreconditionError(err))
		})
	}
}
