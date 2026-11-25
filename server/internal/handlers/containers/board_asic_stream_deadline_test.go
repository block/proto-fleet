package containers

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/grpc/containers/v1/containersv1connect"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/stretchr/testify/require"
)

// A client that stops reading must not hold the stream's handler past the
// write budget. The handler here writes until a write fails, as a relay with
// a steady controller would.
func TestBoardASICStreamWriteDeadlineEndsAStalledResponse(t *testing.T) {
	const budget = 300 * time.Millisecond
	for name, tc := range map[string]struct {
		path    string
		stalled bool
	}{
		"the board ASIC stream": {path: containersv1connect.ContainerServiceStreamBoardASICReadingsProcedure},
		"another procedure":     {path: containersv1connect.ContainerServiceSetBoardIdentifyProcedure, stalled: true},
	} {
		t.Run(name, func(t *testing.T) {
			returned := make(chan struct{})
			release := make(chan struct{})
			writing := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(returned)
				chunk := make([]byte, 64<<10)
				for {
					select {
					case <-release:
						return
					default:
					}
					if _, err := w.Write(chunk); err != nil {
						return
					}
				}
			})
			// The telemetry middleware wraps the response writer in production.
			handler := middleware.TelemetryMiddleware{}.Wrap(WithBoardASICStreamWriteDeadline(writing, budget))
			srv := httptest.NewServer(handler)
			t.Cleanup(srv.Close)

			conn, err := net.Dial("tcp", srv.Listener.Addr().String())
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			_, err = conn.Write([]byte("POST " + tc.path + " HTTP/1.1\r\nHost: fleet\r\nContent-Length: 0\r\n\r\n"))
			require.NoError(t, err)
			// Read the status line, then stop reading while the connection stays open.
			_, err = bufio.NewReader(conn).ReadString('\n')
			require.NoError(t, err)

			select {
			case <-returned:
				require.False(t, tc.stalled, "only the stream has a write deadline")
			case <-time.After(5 * budget):
				require.True(t, tc.stalled, "the stalled stream response was not ended")
			}
			// Unblock the write so the other procedure's handler can return.
			close(release)
			_ = conn.Close()
			<-returned
		})
	}
}
