package foreman

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListMiners_Paginated(t *testing.T) {
	// Arrange
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		resp := PaginatedMinersResponse{
			Limit:  2,
			Offset: 0,
			Total:  3,
		}
		if n == 1 {
			resp.Results = []Miner{
				{ID: 1, Name: "miner1", IP: "10.0.0.1"},
				{ID: 2, Name: "miner2", IP: "10.0.0.2"},
			}
		} else {
			resp.Offset = 2
			resp.Results = []Miner{
				{ID: 3, Name: "miner3", IP: "10.0.0.3"},
			}
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("key", "456")
	client.baseURL = server.URL

	// Act
	miners, err := client.ListMiners(context.Background())

	// Assert
	require.NoError(t, err)
	assert.Len(t, miners, 3)
	assert.Equal(t, int32(2), callCount.Load())
	assert.Equal(t, "miner3", miners[2].Name)
}

func TestListSiteMapGroups(t *testing.T) {
	// Arrange
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		groups := []SiteMapGroup{
			{ID: 1, Name: "G1"},
			{ID: 2, Name: "G2"},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(groups)
	}))
	defer server.Close()

	client := NewClient("key", "789")
	client.baseURL = server.URL

	// Act
	groups, err := client.ListSiteMapGroups(context.Background())

	// Assert
	require.NoError(t, err)
	assert.Len(t, groups, 2)
	assert.Equal(t, "G1", groups[0].Name)
}

func TestGet_RetriesOn429(t *testing.T) {
	// Arrange
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"detail":"rate limited"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient("test-key", "123")
	client.baseURL = server.URL

	// Act
	_, err := client.ListSiteMapGroups(context.Background())

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, int32(2), callCount.Load())
}

func TestGet_ExhaustsRetriesOn429(t *testing.T) {
	// Arrange
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"rate limited"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "123")
	client.baseURL = server.URL

	// Act
	_, err := client.ListSiteMapGroups(context.Background())

	// Assert
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.True(t, apiErr.IsRateLimited())
	assert.Equal(t, int32(maxRetries+1), callCount.Load())
}

func TestGet_RetriesOn429_RespectsContextCancellation(t *testing.T) {
	// Arrange
	var callCount atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		cancel() // cancel after first request
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"rate limited"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "123")
	client.baseURL = server.URL

	// Act
	_, err := client.ListSiteMapGroups(ctx)

	// Assert
	require.Error(t, err)
	assert.Equal(t, int32(1), callCount.Load())
}

func TestNewClient_SanitizesClientID(t *testing.T) {
	tests := []struct {
		name       string
		clientID   string
		expectedID string
	}{
		{
			name:       "valid numeric",
			clientID:   "32888",
			expectedID: "32888",
		},
		{
			name:       "path traversal attempt",
			clientID:   "../../../etc/passwd",
			expectedID: "0",
		},
		{
			name:       "empty string",
			clientID:   "",
			expectedID: "0",
		},
		{
			name:       "alphanumeric",
			clientID:   "123abc",
			expectedID: "0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			client := NewClient("key", tc.clientID)

			// Assert
			assert.Equal(t, tc.expectedID, client.clientID)
		})
	}
}
