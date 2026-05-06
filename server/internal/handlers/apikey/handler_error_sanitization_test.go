package apikey_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	apikeyv1 "github.com/block/proto-fleet/server/generated/grpc/apikey/v1"
	"github.com/block/proto-fleet/server/generated/grpc/apikey/v1/apikeyv1connect"
	domainApiKey "github.com/block/proto-fleet/server/internal/domain/apikey"
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	apikeyHandler "github.com/block/proto-fleet/server/internal/handlers/apikey"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
)

type apiKeyStoreStub struct {
	createFn         func(context.Context, *interfaces.ApiKey) error
	getByHashFn      func(context.Context, string) (*interfaces.ApiKey, error)
	listFn           func(context.Context, int64) ([]interfaces.ApiKey, error)
	revokeFn         func(context.Context, string, int64, time.Time) (int64, error)
	updateLastUsedFn func(context.Context, int64, time.Time) error
}

func (s apiKeyStoreStub) CreateApiKey(ctx context.Context, key *interfaces.ApiKey) error {
	if s.createFn != nil {
		return s.createFn(ctx, key)
	}
	return nil
}

func (s apiKeyStoreStub) CreateAgentApiKey(ctx context.Context, key *interfaces.ApiKey) error {
	if s.createFn != nil {
		return s.createFn(ctx, key)
	}
	return nil
}

func (s apiKeyStoreStub) RevokeApiKeysByAgentID(context.Context, int64, int64, time.Time) (int64, error) {
	return 0, nil
}

func (s apiKeyStoreStub) GetApiKeyByHash(ctx context.Context, keyHash string) (*interfaces.ApiKey, error) {
	if s.getByHashFn != nil {
		return s.getByHashFn(ctx, keyHash)
	}
	return nil, nil
}

func (s apiKeyStoreStub) ListApiKeysByOrganization(ctx context.Context, orgID int64) ([]interfaces.ApiKey, error) {
	if s.listFn != nil {
		return s.listFn(ctx, orgID)
	}
	return nil, nil
}

func (s apiKeyStoreStub) RevokeApiKey(ctx context.Context, keyID string, orgID int64, revokedAt time.Time) (int64, error) {
	if s.revokeFn != nil {
		return s.revokeFn(ctx, keyID, orgID, revokedAt)
	}
	return 0, nil
}

func (s apiKeyStoreStub) UpdateApiKeyLastUsed(ctx context.Context, id int64, lastUsedAt time.Time) error {
	if s.updateLastUsedFn != nil {
		return s.updateLastUsedFn(ctx, id, lastUsedAt)
	}
	return nil
}

type adminAuthInjector struct{}

func (adminAuthInjector) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		info := &session.Info{
			AuthMethod:     session.AuthMethodSession,
			SessionID:      "session-1",
			UserID:         1,
			OrganizationID: 1,
			ExternalUserID: "user-1",
			Username:       "admin",
			Role:           domainAuth.AdminRoleName,
		}
		return next(authn.SetInfo(ctx, info), req)
	}
}

func (adminAuthInjector) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (adminAuthInjector) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func newAPIKeyTestClient(t *testing.T, store interfaces.ApiKeyStore) apikeyv1connect.ApiKeyServiceClient {
	t.Helper()

	service := domainApiKey.NewService(store, nil)
	opts := connect.WithInterceptors(
		interceptors.NewErrorMappingInterceptor(),
		adminAuthInjector{},
	)

	mux := http.NewServeMux()
	mux.Handle(apikeyv1connect.NewApiKeyServiceHandler(apikeyHandler.NewHandler(service), opts))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return apikeyv1connect.NewApiKeyServiceClient(http.DefaultClient, server.URL)
}

func TestAPIKeyHandler_SanitizesInternalErrors(t *testing.T) {
	rawErr := `pq: relation "api_key" does not exist`

	tests := []struct {
		name        string
		store       interfaces.ApiKeyStore
		call        func(t *testing.T, client apikeyv1connect.ApiKeyServiceClient) error
		wantMessage string
	}{
		{
			name: "create api key",
			store: apiKeyStoreStub{
				createFn: func(context.Context, *interfaces.ApiKey) error {
					return errors.New(rawErr)
				},
			},
			call: func(t *testing.T, client apikeyv1connect.ApiKeyServiceClient) error {
				_, err := client.CreateApiKey(t.Context(), connect.NewRequest(&apikeyv1.CreateApiKeyRequest{Name: "test-key"}))
				return err
			},
			wantMessage: "failed to create API key",
		},
		{
			name: "list api keys",
			store: apiKeyStoreStub{
				listFn: func(context.Context, int64) ([]interfaces.ApiKey, error) {
					return nil, errors.New(rawErr)
				},
			},
			call: func(t *testing.T, client apikeyv1connect.ApiKeyServiceClient) error {
				_, err := client.ListApiKeys(t.Context(), connect.NewRequest(&apikeyv1.ListApiKeysRequest{}))
				return err
			},
			wantMessage: "failed to list API keys",
		},
		{
			name: "revoke api key",
			store: apiKeyStoreStub{
				revokeFn: func(context.Context, string, int64, time.Time) (int64, error) {
					return 0, errors.New(rawErr)
				},
			},
			call: func(t *testing.T, client apikeyv1connect.ApiKeyServiceClient) error {
				_, err := client.RevokeApiKey(t.Context(), connect.NewRequest(&apikeyv1.RevokeApiKeyRequest{KeyId: "key-1"}))
				return err
			},
			wantMessage: "failed to revoke API key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newAPIKeyTestClient(t, tt.store)

			err := tt.call(t, client)
			require.Error(t, err)
			require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
			require.Contains(t, err.Error(), tt.wantMessage)
			require.False(t, strings.Contains(err.Error(), rawErr), "raw backend error should not be exposed")
		})
	}
}
