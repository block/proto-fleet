package testutil

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// NewH2CServer is HTTP/2-over-cleartext; required for Connect-RPC bidi
// streams, which can't run over HTTP/1.1.
func NewH2CServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h2c.NewHandler(h, &http2.Server{}))
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func NewH2CClient() *http.Client {
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
}
