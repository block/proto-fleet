package interceptors_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/interceptors"
	"github.com/btc-mining/proto-fleet/server/internal/handlers/ping"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"

	pingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/ping/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/ping/v1/pingv1connect"
)

func TestAuthInterceptor(t *testing.T) {
	tokenSvc, _ := token.NewService(token.Config{
		SecretKey:        "000000000000000000000000000000000000",
		ExpirationPeriod: time.Hour * 24,
	})

	allowList := []string{
		pingv1connect.PingServiceEchoProcedure,
	}

	t.Run("should respect allow list", func(t *testing.T) {
		// Setup test server
		authInterceptor := interceptors.NewAuthInterceptor(tokenSvc, allowList)
		errorInterceptor := interceptors.NewErrorMappingInterceptor()
		path, handler := pingv1connect.NewPingServiceHandler(
			ping.Handler{},
			connect.WithInterceptors(errorInterceptor, authInterceptor),
		)
		mux := http.NewServeMux()
		mux.Handle(path, handler)
		testServer := httptest.NewServer(mux)
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
		authInterceptor := interceptors.NewAuthInterceptor(tokenSvc, []string{})
		errorInterceptor := interceptors.NewErrorMappingInterceptor()
		path, handler := pingv1connect.NewPingServiceHandler(
			ping.Handler{},
			connect.WithInterceptors(errorInterceptor, authInterceptor),
		)
		mux := http.NewServeMux()
		mux.Handle(path, handler)
		testServer := httptest.NewServer(mux)
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
		authInterceptor := interceptors.NewAuthInterceptor(tokenSvc, allowList)
		errorInterceptor := interceptors.NewErrorMappingInterceptor()
		path, handler := pingv1connect.NewPingServiceHandler(
			ping.Handler{},
			connect.WithInterceptors(errorInterceptor, authInterceptor),
		)
		mux := http.NewServeMux()
		mux.Handle(path, handler)
		testServer := httptest.NewServer(mux)
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

		jwt, _, err := tokenSvc.GenerateJWT(123, 1)
		assert.NoError(t, err)

		req.Header().Set(
			"Authorization",
			"Bearer "+jwt,
		)

		resp, err := client.Ping(t.Context(), req)
		assert.NoError(t, err)

		// Verify response
		assert.Equal(t, "Hello", resp.Msg.Text)
	})

	t.Run("should fail auth check when token is invalid", func(t *testing.T) {
		// Setup test server
		authInterceptor := interceptors.NewAuthInterceptor(tokenSvc, allowList)
		errorInterceptor := interceptors.NewErrorMappingInterceptor()
		path, handler := pingv1connect.NewPingServiceHandler(
			ping.Handler{},
			connect.WithInterceptors(errorInterceptor, authInterceptor),
		)
		mux := http.NewServeMux()
		mux.Handle(path, handler)
		testServer := httptest.NewServer(mux)
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
