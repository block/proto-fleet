package updaterapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var acknowledgeStartedAt = time.Date(2026, 8, 15, 8, 30, 0, 123_000_000, time.UTC)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func acknowledgeTestClient(t *testing.T, response Operation) *Client {
	t.Helper()
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/v1/acknowledge", request.URL.Path)
		var body AcknowledgeRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		assert.Equal(t, "11111111-1111-4111-8111-111111111111", body.OperationID)
		assert.True(t, body.ExpectedStartedAt.Equal(acknowledgeStartedAt))
		assert.Equal(t, uint64(7), body.ExpectedOutcomeRevision)

		encoded, err := json.Marshal(AcknowledgeResponse{
			Operation:           response,
			AlreadyAcknowledged: true,
		})
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(encoded))),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})
	return NewClientWithHTTP(&http.Client{Transport: transport})
}

func TestClientAcknowledgeCarriesAndValidatesOutcomeIdentity(t *testing.T) {
	t.Parallel()

	const operationID = "11111111-1111-4111-8111-111111111111"
	t.Run("matching revision", func(t *testing.T) {
		t.Parallel()
		client := acknowledgeTestClient(t, Operation{
			ID:              operationID,
			Phase:           PhaseFailed,
			StartedAt:       acknowledgeStartedAt,
			OutcomeRevision: 7,
			Acknowledged:    true,
		})

		operation, alreadyAcknowledged, err := client.Acknowledge(
			context.Background(),
			operationID,
			acknowledgeStartedAt,
			7,
		)
		require.NoError(t, err)
		assert.True(t, alreadyAcknowledged)
		assert.Equal(t, uint64(7), operation.OutcomeRevision)
	})

	t.Run("mismatched revision", func(t *testing.T) {
		t.Parallel()
		client := acknowledgeTestClient(t, Operation{
			ID:              operationID,
			Phase:           PhaseFailed,
			StartedAt:       acknowledgeStartedAt,
			OutcomeRevision: 8,
			Acknowledged:    true,
		})

		_, _, err := client.Acknowledge(context.Background(), operationID, acknowledgeStartedAt, 7)
		var protocolErr *ProtocolError
		require.ErrorAs(t, err, &protocolErr)
	})

	t.Run("mismatched operation incarnation", func(t *testing.T) {
		t.Parallel()
		client := acknowledgeTestClient(t, Operation{
			ID:              operationID,
			Phase:           PhaseFailed,
			StartedAt:       acknowledgeStartedAt.Add(time.Second),
			OutcomeRevision: 7,
			Acknowledged:    true,
		})

		_, _, err := client.Acknowledge(context.Background(), operationID, acknowledgeStartedAt, 7)
		var protocolErr *ProtocolError
		require.ErrorAs(t, err, &protocolErr)
	})
}
