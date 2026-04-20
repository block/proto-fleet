package interceptors_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	domainApiKey "github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/handlers/ping"

	pingv1 "github.com/block/proto-fleet/server/generated/grpc/ping/v1"
	"github.com/block/proto-fleet/server/generated/grpc/ping/v1/pingv1connect"
)

type interceptorAPIKeyStoreStub struct {
	getByHashFn func(context.Context, string) (*interfaces.ApiKey, error)
}

func (s interceptorAPIKeyStoreStub) CreateApiKey(context.Context, *interfaces.ApiKey) error {
	return nil
}

func (s interceptorAPIKeyStoreStub) GetApiKeyByHash(ctx context.Context, keyHash string) (*interfaces.ApiKey, error) {
	if s.getByHashFn != nil {
		return s.getByHashFn(ctx, keyHash)
	}
	return nil, nil
}

func (s interceptorAPIKeyStoreStub) ListApiKeysByOrganization(context.Context, int64) ([]interfaces.ApiKey, error) {
	return nil, nil
}

func (s interceptorAPIKeyStoreStub) RevokeApiKey(context.Context, string, int64, time.Time) (int64, error) {
	return 0, nil
}

func (s interceptorAPIKeyStoreStub) UpdateApiKeyLastUsed(context.Context, int64, time.Time) error {
	return nil
}

func TestAuthInterceptor_SanitizesAPIKeyValidationErrors(t *testing.T) {
	rawErr := `pq: relation "api_key" does not exist`
	apiKeyService := domainApiKey.NewService(interceptorAPIKeyStoreStub{
		getByHashFn: func(context.Context, string) (*interfaces.ApiKey, error) {
			return nil, errors.New(rawErr)
		},
	}, nil)

	opts := connect.WithInterceptors(
		interceptors.NewErrorMappingInterceptor(),
		interceptors.NewAuthInterceptor(nil, nil, nil, apiKeyService, nil, nil),
	)

	mux := http.NewServeMux()
	mux.Handle(pingv1connect.NewPingServiceHandler(ping.Handler{}, opts))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := pingv1connect.NewPingServiceClient(http.DefaultClient, server.URL)
	req := connect.NewRequest(&pingv1.PingRequest{Text: "hello"})
	req.Header().Set("Authorization", "Bearer fleet_deadbeef_secret")

	_, err := client.Ping(t.Context(), req)
	require.Error(t, err)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	require.Contains(t, err.Error(), "API key service unavailable")
	require.False(t, strings.Contains(err.Error(), rawErr), "raw backend error should not be exposed")
}
