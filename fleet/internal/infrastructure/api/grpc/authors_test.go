package grpc_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/alecthomas/assert/v2"

	authorsv1 "github.com/btc-mining/miner-firmware/fleet/generated/grpc/authors/v1"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/authors/v1/authorsv1connect"
	"github.com/btc-mining/miner-firmware/fleet/internal/application"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/api/grpc"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/db/dbtest"
)

func TestAuthorsServer_Add(t *testing.T) {
	t.Run("successfully adds an author", func(t *testing.T) {
		// Setup dependencies
		conn := dbtest.GetTestDB(t)
		authorUseCases := application.NewAuthorUseCases(conn)

		// Setup test server
		mux := http.NewServeMux()
		server := grpc.NewAuthorsServer(authorUseCases)
		path, handler := authorsv1connect.NewAuthorsServiceHandler(server)
		mux.Handle(path, handler)
		testServer := httptest.NewServer(mux)
		defer testServer.Close()

		// Create client
		client := authorsv1connect.NewAuthorsServiceClient(
			http.DefaultClient,
			testServer.URL,
		)

		// Make request
		req := connect.NewRequest(&authorsv1.AddRequest{
			Name: "Test Author",
			Bio:  "Test Bio",
		})

		resp, err := client.Add(t.Context(), req)
		assert.NoError(t, err)

		// Verify response
		assert.NotEqual(t, nil, resp.Msg.Author, "expected author in response, got nil")
		assert.Equal(t, "Test Author", resp.Msg.Author.Name)
		assert.Equal(t, "Test Bio", resp.Msg.Author.Bio)
	})

}
