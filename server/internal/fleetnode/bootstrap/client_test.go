package bootstrap

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/testutil"
)

func newHTTP2TestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// Regression coverage for the prior DialTLSContext shim that quietly
// bypassed TLS for https:// URLs.
func TestGatewayHTTPClient_HTTPS_ValidatesCertificate(t *testing.T) {
	t.Parallel()

	// Arrange
	srv := newHTTP2TestServer(t)
	trusted, err := newGatewayHTTPClient(srv.URL)
	require.NoError(t, err)
	tlsTransport, ok := trusted.Transport.(*http.Transport)
	require.True(t, ok, "https client must use http.Transport")
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tlsTransport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}

	// Act
	resp, err := trusted.Get(srv.URL)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestGatewayHTTPClient_HTTPS_RejectsUntrustedCertificate(t *testing.T) {
	t.Parallel()

	// Arrange
	srv := newHTTP2TestServer(t)
	client, err := newGatewayHTTPClient(srv.URL)
	require.NoError(t, err)

	// Act
	resp, err := client.Get(srv.URL) //nolint:bodyclose // request fails; resp is nil

	// Assert
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "x509", "expected x509 cert validation error, got %v", err)
}

func TestNewGatewayClient_RejectsNonHTTPScheme(t *testing.T) {
	t.Parallel()

	cases := []string{"ftp://example.com", "://no-scheme", "ws://example.com"}
	for _, target := range cases {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			// Act
			_, err := NewGatewayClient(target)

			// Assert
			require.Error(t, err)
		})
	}
}

func TestGatewayHTTPClient_RejectsRedirect(t *testing.T) {
	t.Parallel()

	cases := []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	}
	for _, code := range cases {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()

			// Arrange
			srv := testutil.NewH2CServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", "http://attacker.example.com/")
				w.WriteHeader(code)
			}))
			client, err := newGatewayHTTPClient(srv.URL)
			require.NoError(t, err)

			// Act
			resp, err := client.Post(srv.URL, "application/proto", strings.NewReader(""))

			// Assert
			require.Error(t, err)
			assert.Contains(t, err.Error(), "redirects are not allowed")
			if resp != nil {
				_ = resp.Body.Close()
			}
		})
	}
}
