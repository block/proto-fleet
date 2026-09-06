package inventory

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	pb "github.com/block/proto-fleet/server/generated/grpc/inventory/v1"
	"github.com/block/proto-fleet/server/generated/grpc/inventory/v1/inventoryv1connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVReadLimitAndDecodedSizeValidation(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.Handle(inventoryv1connect.NewInventoryServiceHandler(
		// Unimplemented means decoding and validation reached the handler,
		// without importing data or requiring a database.
		inventoryv1connect.UnimplementedInventoryServiceHandler{},
		RequestReadLimitOption(),
		connect.WithInterceptors(validate.NewInterceptor()),
	))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	for _, transport := range []struct {
		name    string
		options []connect.ClientOption
	}{
		{"binary", nil},
		{"json", []connect.ClientOption{connect.WithProtoJSON()}},
	} {
		t.Run(transport.name, func(t *testing.T) {
			t.Parallel()
			client := inventoryv1connect.NewInventoryServiceClient(http.DefaultClient, server.URL, transport.options...)
			for _, payload := range []struct {
				name string
				size int
				want connect.Code
			}{
				{"maximum CSV", 10 * 1024 * 1024, connect.CodeUnimplemented},
				{"above decoded limit", 10*1024*1024 + 1, connect.CodeInvalidArgument},
			} {
				t.Run(payload.name, func(t *testing.T) {
					t.Parallel()
					data := make([]byte, payload.size)
					_, err := client.ImportInventoryCsv(t.Context(), connect.NewRequest(&pb.ImportInventoryCsvRequest{CsvData: data}))
					require.Error(t, err)
					assert.Equal(t, payload.want, connect.CodeOf(err), "preview: %v", err)
					_, err = client.ConfirmInventoryImport(t.Context(), connect.NewRequest(&pb.ConfirmInventoryImportRequest{CsvData: data}))
					require.Error(t, err)
					assert.Equal(t, payload.want, connect.CodeOf(err), "confirmation: %v", err)
				})
			}
		})
	}
}
