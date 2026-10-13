package minerproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/require"
)

func TestProxyControlCompletion(t *testing.T) {
	for _, operation := range []struct {
		method, version, rest string
		complete              bool
	}{
		{http.MethodPost, "v2", "mining/start", true},
		{http.MethodPost, "v2", "mining/stop", true},
		{http.MethodPost, "v2", "mining/scoped/start", true},
		{http.MethodPost, "v2", "mining/scoped/stop", true},
		{http.MethodPut, "v2", "mining/target-power", true},
		{http.MethodPost, "v2", "power/container/controls/coolant_pump", true},
		{http.MethodPost, "v2", "power/container/controls/dry_cooler", true},
		{http.MethodPost, "v2", "power/container/controls/dry_cooler_auto", true},
		{http.MethodPost, "v2", "power/container/controls/tank_led_light", true},
		{http.MethodPost, "v2", "power/container/controls/air_conditioner_1", true},
		{http.MethodPost, "v2", "power/container/controls/air_conditioner_2", true},
		{http.MethodPost, "v2", "power/container/setpoints/coolant_pump_speed_hz", true},
		{http.MethodPost, "v2", "power/container/setpoints/dry_cooler_auto_temp_c", true},
		{http.MethodPost, "v2", "power/container/setpoints/dry_cooler_manual_speed_percent", true},
		{http.MethodPost, "v2", "power/container/setpoints/air_conditioner_1_temp_c", true},
		{http.MethodPost, "v2", "power/container/setpoints/air_conditioner_2_temp_c", true},
		{http.MethodPost, "v2", "modules/T1-M01/run-in", false},
		{http.MethodPost, "v2", "hashboards/identify-led", false},
		{http.MethodPost, "v2", "mining/isolation", false},
		{http.MethodPost, "v1", "new-setting", false},
	} {
		t.Run(operation.version+"/"+operation.rest, func(t *testing.T) {
			database, org, device, h, target := proxyFenceFixture(t)
			transport := &proxyResponseTransport{status: http.StatusOK, body: io.NopCloser(strings.NewReader("{}"))}
			h.httpClient = &http.Client{Transport: transport}
			h.storeToken(target.cacheKey(), "fixture-token")
			request := authenticatedProxyRequest(t, database, h, target)
			request.Method = operation.method
			request.SetPathValue("apiVersion", operation.version)
			request.SetPathValue("rest", operation.rest)
			response := newDeadlineProxyRecorder()
			h.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, int64(1), transport.calls.Load())
			if operation.complete {
				require.Equal(t, "complete", proxyAttemptStatus(t, database, device))
				epoch, err := h.writeService.Reserve(t.Context(), org, device)
				require.NoError(t, err)
				drained, err := h.writeService.Drained(t.Context(), org, device, epoch)
				require.NoError(t, err)
				require.True(t, drained, "returned synchronous controls must allow ownership handoff")
			} else {
				require.Equal(t, "unknown", proxyAttemptStatus(t, database, device))
				_, err := h.writeService.Reserve(t.Context(), org, device)
				require.True(t, fleeterror.IsFailedPreconditionError(err))
			}
		})
	}
}

func TestProxyAcceptedControlRetainsUncertainty(t *testing.T) {
	database, org, device, h, target := proxyFenceFixture(t)
	transport := &proxyResponseTransport{status: http.StatusAccepted, body: io.NopCloser(strings.NewReader("{}"))}
	h.httpClient = &http.Client{Transport: transport}
	request := authenticatedProxyRequest(t, database, h, target)
	request.Method = http.MethodPost
	request.SetPathValue("rest", "mining/start")
	response := newDeadlineProxyRecorder()
	h.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code)
	require.Equal(t, "unknown", proxyAttemptStatus(t, database, device), "an explicitly asynchronous response cannot qualify a synchronous endpoint")
	inspection, err := h.writeService.InspectRecovery(t.Context(), org, device)
	require.NoError(t, err)
	require.Len(t, inspection, 1)
	require.Equal(t, "host", inspection[0].FenceState, "accepted async work must preserve ordinary host operation")
	transport.status = http.StatusOK
	transport.body = io.NopCloser(strings.NewReader("{}"))
	next := newDeadlineProxyRecorder()
	h.ServeHTTP(next, request)
	require.Equal(t, http.StatusOK, next.Code, "a later host control must still be admitted")
	inspection, err = h.writeService.InspectRecovery(t.Context(), org, device)
	require.NoError(t, err)
	require.Len(t, inspection, 1, "the later completed control cannot clear the earlier pending operation")
	_, err = h.writeService.Reserve(t.Context(), org, device)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
}

func TestProxyReturnedControlFailuresFollowCompletionPolicy(t *testing.T) {
	for _, responseStatus := range []int{http.StatusOK, http.StatusAccepted} {
		for _, failure := range []string{"network", "read", "close", "copy", "flush", "HTTP"} {
			if responseStatus == http.StatusAccepted && (failure == "network" || failure == "HTTP") {
				continue // These failures do not receive an accepted response.
			}
			t.Run(fmt.Sprintf("%d/%s", responseStatus, failure), func(t *testing.T) {
				database, org, device, h, target := proxyFenceFixture(t)
				body := &proxyBodyBarrier{reader: strings.NewReader("{}")}
				if failure == "read" {
					body.reader = proxyReaderFunc(func([]byte) (int, error) { return 0, errors.New("fixture-read-secret") })
				}
				if failure == "close" {
					body.closeErr = errors.New("fixture-close-secret")
				}
				status := responseStatus
				if failure == "HTTP" {
					status = http.StatusInternalServerError
				}
				h.httpClient = &http.Client{Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
					if failure == "network" {
						return nil, errors.New("fixture-network-secret")
					}
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: body}, nil
				})}
				recorder := newDeadlineProxyRecorder()
				var response http.ResponseWriter = recorder
				if failure == "copy" {
					response = proxyFailingWriter{recorder}
				}
				if failure == "flush" {
					release := make(chan struct{})
					close(release)
					response = &proxyFlushBoundaryWriter{deadlineProxyRecorder: recorder, entered: make(chan struct{}), release: release, err: errors.New("fixture-flush-secret")}
				}
				request := authenticatedProxyRequest(t, database, h, target)
				request.Method = http.MethodPost
				request.SetPathValue("rest", "mining/start")
				h.ServeHTTP(response, request)
				require.NotContains(t, recorder.Body.String(), "fixture-")
				if responseStatus == http.StatusAccepted {
					require.Equal(t, "unknown", proxyAttemptStatus(t, database, device), "a pending operation remains uncertain when response consumption fails")
					_, err := h.writeService.Reserve(t.Context(), org, device)
					require.True(t, fleeterror.IsFailedPreconditionError(err))
					return
				}
				require.Equal(t, "complete", proxyAttemptStatus(t, database, device), "returned ordered calls retire even when delivery or response fails")
				epoch, err := h.writeService.Reserve(t.Context(), org, device)
				require.NoError(t, err)
				drained, err := h.writeService.Drained(t.Context(), org, device, epoch)
				require.NoError(t, err)
				require.True(t, drained)
			})
		}
	}
}

func TestProxyUncertainControlDeliveryBlocksHandoff(t *testing.T) {
	database, org, device, h, target := proxyFenceFixture(t)
	// The shared completion policy must preserve an intermediary's explicit
	// uncertainty even if the proxy sanitizes the transport error.
	h.httpClient = &http.Client{Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, devicewrite.UncertainDelivery(fmt.Errorf("fixture intermediary may still dispatch"))
	})}
	request := authenticatedProxyRequest(t, database, h, target)
	request.Method = http.MethodPost
	request.SetPathValue("rest", "mining/start")
	response := newDeadlineProxyRecorder()
	h.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadGateway, response.Code)
	require.NotContains(t, response.Body.String(), "fixture")
	require.Equal(t, "unknown", proxyAttemptStatus(t, database, device))
	_, err := h.writeService.Reserve(t.Context(), org, device)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
}

func TestProxyCompletedControlPreservesEarlierUnresolvedWrite(t *testing.T) {
	database, org, device, h, target := proxyFenceFixture(t)
	require.NoError(t, h.writeService.RunHost(t.Context(), org, device, target.binding.WriteEpoch, func(context.Context) error { return nil }))
	h.httpClient = &http.Client{Transport: &proxyResponseTransport{status: http.StatusOK, body: io.NopCloser(strings.NewReader("{}"))}}
	request := authenticatedProxyRequest(t, database, h, target)
	request.Method = http.MethodPost
	request.SetPathValue("rest", "mining/start")
	response := newDeadlineProxyRecorder()
	h.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "complete", proxyAttemptStatus(t, database, device))
	inspection, err := h.writeService.InspectRecovery(t.Context(), org, device)
	require.NoError(t, err)
	require.Len(t, inspection, 1, "earlier unresolved attempts remain recoverable")
	_, err = h.writeService.Reserve(t.Context(), org, device)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
}
