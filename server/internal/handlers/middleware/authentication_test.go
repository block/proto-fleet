package middleware_test

import (
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/middleware"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/ping"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"

	pingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/ping/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/ping/v1/pingv1connect"
)

func TestAuthMiddleware(t *testing.T) {

	tokenSvc, _ := token.NewService(token.Config{
		SecretKey:        "000000000000000000000000000000000000",
		ExpirationPeriod: time.Hour * 24,
	})

	allowList := []string{
		pingv1connect.PingServiceEchoProcedure,
	}

	t.Run("should respect allow list", func(t *testing.T) {

		// Setup test server
		mux := http.NewServeMux()
		path, handler := pingv1connect.NewPingServiceHandler(ping.Handler{})
		mux.Handle(path, handler)

		middleware := middleware.NewAuthMiddleware(tokenSvc, allowList)
		testServer := httptest.NewServer(middleware.Wrap(mux))
		defer testServer.Close()

		// Create client
		client := pingv1connect.NewPingServiceClient(
			http.DefaultClient,
			testServer.URL,
		)

		// Make request
		req := connect.NewRequest(&pingv1.EchoRequest{
			Text: "Hello",
		})

		resp, err := client.Echo(t.Context(), req)
		assert.NoError(t, err)

		// Verify response
		assert.Equal(t, "Hello", resp.Msg.Text)
	})

	t.Run("should fail auth when procedure not in allow list", func(t *testing.T) {

		// Setup test server
		mux := http.NewServeMux()
		path, handler := pingv1connect.NewPingServiceHandler(ping.Handler{})
		mux.Handle(path, handler)

		middleware := middleware.NewAuthMiddleware(tokenSvc, []string{})
		testServer := httptest.NewServer(middleware.Wrap(mux))
		defer testServer.Close()

		// Create client
		client := pingv1connect.NewPingServiceClient(
			http.DefaultClient,
			testServer.URL,
		)

		// Make request
		req := connect.NewRequest(&pingv1.EchoRequest{
			Text: "Hello",
		})

		_, err := client.Echo(t.Context(), req)
		assert.Error(t, err)
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))

	})

	t.Run("should pass auth check when token is valid", func(t *testing.T) {

		// Setup test server
		mux := http.NewServeMux()
		path, handler := pingv1connect.NewPingServiceHandler(ping.Handler{})
		mux.Handle(path, handler)

		middleware := middleware.NewAuthMiddleware(tokenSvc, allowList)
		testServer := httptest.NewServer(middleware.Wrap(mux))
		defer testServer.Close()

		// Create client
		client := pingv1connect.NewPingServiceClient(
			http.DefaultClient,
			testServer.URL,
		)

		// Make request
		req := connect.NewRequest(&pingv1.PingRequest{
			Text: "Hello",
		})

		token, err := tokenSvc.GenerateJWT("user_123")
		assert.NoError(t, err)

		req.Header().Set(
			"Authorization",
			"Bearer "+token,
		)

		resp, err := client.Ping(t.Context(), req)
		assert.NoError(t, err)

		// Verify response
		assert.Equal(t, "Hello", resp.Msg.Text)
	})

	t.Run("should fail auth check when token is invalid", func(t *testing.T) {

		// Setup test server
		mux := http.NewServeMux()
		path, handler := pingv1connect.NewPingServiceHandler(ping.Handler{})
		mux.Handle(path, handler)

		middleware := middleware.NewAuthMiddleware(tokenSvc, allowList)
		testServer := httptest.NewServer(middleware.Wrap(mux))
		defer testServer.Close()

		// Create client
		client := pingv1connect.NewPingServiceClient(
			http.DefaultClient,
			testServer.URL,
		)

		// Make request
		req := connect.NewRequest(&pingv1.PingRequest{
			Text: "Hello",
		})

		req.Header().Set(
			"Authorization",
			"Bearer hvhjvghjvjvgvcghjvjvgj",
		)

		_, err := client.Ping(t.Context(), req)

		// Verify response
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

}
